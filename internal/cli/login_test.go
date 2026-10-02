package cli_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/cli"
	"github.com/mgoodness/eve-trader/internal/config"
)

// fakeLoginVerificationServer serves the SSO token endpoint (the
// authorization-code exchange RunLogin performs) plus the ESI
// skills/standings routes its verification step reads, and records every
// grant_type the token endpoint saw -- so a test can prove verification
// reuses the freshly minted access token rather than performing a second
// refresh-token exchange.
type fakeLoginVerificationServer struct {
	*httptest.Server
	grantTypes []string
}

func newFakeLoginVerificationServer(t *testing.T, characterIDClaim string, skills, standings []map[string]any) *fakeLoginVerificationServer {
	t.Helper()
	fake := &fakeLoginVerificationServer{}
	fake.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v2/oauth/token":
			_ = r.ParseForm()
			fake.grantTypes = append(fake.grantTypes, r.PostForm.Get("grant_type"))
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  fakeJWTWithName(characterIDClaim, "Test Pilot"),
				"token_type":    "Bearer",
				"expires_in":    1200,
				"refresh_token": "rotated-refresh-token",
			})
		case strings.HasSuffix(r.URL.Path, "/skills/"):
			json.NewEncoder(w).Encode(map[string]any{"skills": skills})
		case strings.HasSuffix(r.URL.Path, "/standings/"):
			json.NewEncoder(w).Encode(standings)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(fake.Close)
	return fake
}

// fakeSSOServer serves only the SSO token endpoint RunLogin's code
// exchange hits; the authorize endpoint is never actually requested in
// these tests (a real browser or human would GET it; the fake opener
// below plays that role by going straight to the loopback callback, the
// same way a browser redirect would).
func fakeSSOServer(t *testing.T, rotatedRefreshToken string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/oauth/token" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "fake-access-token",
			"token_type":    "Bearer",
			"expires_in":    1200,
			"refresh_token": rotatedRefreshToken,
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// freeLoopbackRedirectURI finds a currently-free 127.0.0.1 port and
// returns a redirect URI on it, without holding the port open: RunLogin
// binds it itself.
func freeLoopbackRedirectURI(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("finding a free port: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return "http://" + addr + "/callback"
}

// simulateConsent fakes the browser's half of the round-trip: instead of
// showing a human a consent page, it reads the state eve-trader generated
// straight off the authorize URL it was asked to open and GETs the
// redirect URI with that state and the given code, exactly as EVE SSO's
// redirect would.
func simulateConsent(code string) func(authorizeURL string) error {
	return simulateConsentWithState(code, "")
}

// simulateConsentWithState is simulateConsent but lets a test force a
// state that doesn't match the one RunLogin generated, to exercise the
// CSRF guard.
func simulateConsentWithState(code, forcedState string) func(authorizeURL string) error {
	return func(authorizeURL string) error {
		u, err := url.Parse(authorizeURL)
		if err != nil {
			return err
		}
		state := forcedState
		if state == "" {
			state = u.Query().Get("state")
		}
		callbackURL := u.Query().Get("redirect_uri") + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
		resp, err := http.Get(callbackURL)
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	}
}

func TestRunLoginPrintsTheConsentURLEvenWhenNoBrowserCanBeOpened(t *testing.T) {
	server := fakeSSOServer(t, "rotated-refresh-token")
	cfg := cli.DefaultConfig()
	cfg.SSOBaseURL = server.URL
	cfg.ConfigDir = t.TempDir()
	cfg.OpenBrowser = func(string) error { return errors.New("no browser available in this environment") }

	// No fake consent ever lands on the callback here, so RunLogin would
	// otherwise block forever; a short deadline lets the test observe the
	// printed URL without caring how RunLogin ultimately resolves.
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()

	var buf strings.Builder
	_ = cli.RunLogin(ctx, cfg, cli.LoginParams{ClientID: "my-client-id", RedirectURI: freeLoopbackRedirectURI(t)}, &buf)

	wantPrefix := server.URL + "/v2/oauth/authorize?"
	if !strings.Contains(buf.String(), wantPrefix) {
		t.Errorf("got output %q, want it to contain the consent URL (prefix %q) even though no browser could be opened", buf.String(), wantPrefix)
	}
}

func TestRunLoginAbortsAndWritesNothingOnAStateMismatch(t *testing.T) {
	server := fakeSSOServer(t, "rotated-refresh-token")
	cfg := cli.DefaultConfig()
	cfg.SSOBaseURL = server.URL
	cfg.ConfigDir = t.TempDir()
	cfg.OpenBrowser = simulateConsentWithState("the-auth-code", "not-the-state-eve-trader-sent")

	var buf strings.Builder
	err := cli.RunLogin(t.Context(), cfg, cli.LoginParams{ClientID: "my-client-id", RedirectURI: freeLoopbackRedirectURI(t)}, &buf)
	if err == nil {
		t.Fatal("got no error for a state mismatch, want one")
	}

	if _, err := os.Stat(filepath.Join(cfg.ConfigDir, "credentials.json")); !os.IsNotExist(err) {
		t.Errorf("got credentials.json present after a state mismatch (stat err: %v), want nothing written", err)
	}
}

func TestRunLoginAcceptsAPastedCallbackURLOnStdinWhenNoCallbackArrives(t *testing.T) {
	server := newFakeLoginVerificationServer(t, "CHARACTER:EVE:932683762", pilotSkills(), []map[string]any{})
	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = server.URL
	cfg.SSOBaseURL = server.URL
	cfg.ConfigDir = t.TempDir()
	cfg.CacheDir = t.TempDir()

	redirectURI := freeLoopbackRedirectURI(t)
	stdinR, stdinW := io.Pipe()
	cfg.Stdin = stdinR
	// No fake browser ever hits the loopback callback here -- this simulates
	// a pilot on an SSH session who opened the printed URL on a different
	// machine and pasted the resulting (unreachable, 127.0.0.1) redirect URL
	// back on stdin instead.
	cfg.OpenBrowser = func(authorizeURL string) error {
		u, err := url.Parse(authorizeURL)
		if err != nil {
			return err
		}
		state := u.Query().Get("state")
		go func() {
			fmt.Fprintf(stdinW, "%s?code=the-auth-code&state=%s\n", redirectURI, state)
		}()
		return nil
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var buf strings.Builder
	err := cli.RunLogin(ctx, cfg, cli.LoginParams{ClientID: "my-client-id", RedirectURI: redirectURI}, &buf)
	if err != nil {
		t.Fatalf("RunLogin: %v\noutput: %s", err, buf.String())
	}

	creds, err := config.LoadCredentials(filepath.Join(cfg.ConfigDir, "credentials.json"))
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if creds.RefreshToken != "rotated-refresh-token" {
		t.Errorf("got refresh token %q, want rotated-refresh-token from the pasted-callback exchange", creds.RefreshToken)
	}
}

func TestRunLoginAcceptsABareAuthorizationCodeOnStdinWhenNoCallbackArrives(t *testing.T) {
	server := newFakeLoginVerificationServer(t, "CHARACTER:EVE:932683762", pilotSkills(), []map[string]any{})
	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = server.URL
	cfg.SSOBaseURL = server.URL
	cfg.ConfigDir = t.TempDir()
	cfg.CacheDir = t.TempDir()

	// A bare code, no URL at all: the pilot copied just the `code` value out
	// of the browser's address bar rather than the whole unreachable
	// redirect URL.
	cfg.Stdin = strings.NewReader("the-auth-code\n")
	cfg.OpenBrowser = func(string) error { return nil }

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var buf strings.Builder
	err := cli.RunLogin(ctx, cfg, cli.LoginParams{ClientID: "my-client-id", RedirectURI: freeLoopbackRedirectURI(t)}, &buf)
	if err != nil {
		t.Fatalf("RunLogin: %v\noutput: %s", err, buf.String())
	}

	creds, err := config.LoadCredentials(filepath.Join(cfg.ConfigDir, "credentials.json"))
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if creds.RefreshToken != "rotated-refresh-token" {
		t.Errorf("got refresh token %q, want rotated-refresh-token from the bare-code exchange", creds.RefreshToken)
	}
}

func TestRunLoginSharesOneStdinReaderBetweenTheClientIDPromptAndTheCallbackFallback(t *testing.T) {
	server := newFakeLoginVerificationServer(t, "CHARACTER:EVE:932683762", pilotSkills(), []map[string]any{})
	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = server.URL
	cfg.SSOBaseURL = server.URL
	cfg.ConfigDir = t.TempDir()
	cfg.CacheDir = t.TempDir()
	// A single piped stdin stream delivering both the client-id prompt's
	// answer and the headless callback fallback's bare code in one
	// underlying read -- the real "login over SSH, first run, no client id
	// yet" scenario, where a stacked second bufio.Reader over the same
	// stream could over-read and lose the second line.
	cfg.Stdin = strings.NewReader("prompted-client-id\nthe-auth-code\n")
	// No browser ever reaches the loopback listener, so only the stdin
	// fallback can produce the callback.
	cfg.OpenBrowser = func(string) error { return nil }

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	var buf strings.Builder
	err := cli.RunLogin(ctx, cfg, cli.LoginParams{RedirectURI: freeLoopbackRedirectURI(t)}, &buf)
	if err != nil {
		t.Fatalf("RunLogin with the client-id prompt and the stdin callback fallback sharing one stdin stream: %v\noutput: %s", err, buf.String())
	}

	creds, err := config.LoadCredentials(filepath.Join(cfg.ConfigDir, "credentials.json"))
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if creds.ClientID != "prompted-client-id" {
		t.Errorf("got client id %q, want the prompted prompted-client-id", creds.ClientID)
	}
	if creds.RefreshToken != "rotated-refresh-token" {
		t.Errorf("got refresh token %q, want rotated-refresh-token from the stdin-fallback exchange", creds.RefreshToken)
	}
}

func TestLoginCommandRedirectURIFlagOverridesTheDefaultAndBindsThatHostPort(t *testing.T) {
	server := newFakeLoginVerificationServer(t, "CHARACTER:EVE:932683762", pilotSkills(), []map[string]any{})
	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = server.URL
	cfg.SSOBaseURL = server.URL
	cfg.ConfigDir = t.TempDir()
	cfg.CacheDir = t.TempDir()
	cfg.OpenBrowser = simulateConsent("the-auth-code")

	customRedirectURI := freeLoopbackRedirectURI(t)
	if strings.Contains(customRedirectURI, "127.0.0.1:8000") {
		t.Fatal("test setup picked the default port by chance; rerun")
	}

	root := cli.NewRootCmd(cfg)
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"login", "--client-id", "my-client-id", "--redirect-uri", customRedirectURI})

	if err := root.Execute(); err != nil {
		t.Fatalf("login: %v\noutput: %s", err, out.String())
	}

	creds, err := config.LoadCredentials(filepath.Join(cfg.ConfigDir, "credentials.json"))
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if creds.RedirectURI != customRedirectURI {
		t.Errorf("got redirect uri %q, want the --redirect-uri override %q", creds.RedirectURI, customRedirectURI)
	}
}

func TestRunLoginCompletesThePKCERoundTripAndWritesCredentials(t *testing.T) {
	server := newFakeLoginVerificationServer(t, "CHARACTER:EVE:932683762", pilotSkills(), []map[string]any{})
	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = server.URL
	cfg.SSOBaseURL = server.URL
	cfg.ConfigDir = t.TempDir()
	cfg.CacheDir = t.TempDir()
	cfg.OpenBrowser = simulateConsent("the-auth-code")

	redirectURI := freeLoopbackRedirectURI(t)

	var buf strings.Builder
	err := cli.RunLogin(t.Context(), cfg, cli.LoginParams{ClientID: "my-client-id", RedirectURI: redirectURI}, &buf)
	if err != nil {
		t.Fatalf("RunLogin: %v", err)
	}

	path := filepath.Join(cfg.ConfigDir, "credentials.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat credentials.json: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("got mode %#o, want 0600", perm)
	}

	creds, err := config.LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if creds.ClientID != "my-client-id" {
		t.Errorf("got client id %q, want my-client-id", creds.ClientID)
	}
	if creds.RedirectURI != redirectURI {
		t.Errorf("got redirect uri %q, want %q", creds.RedirectURI, redirectURI)
	}
	if creds.RefreshToken != "rotated-refresh-token" {
		t.Errorf("got refresh token %q, want rotated-refresh-token", creds.RefreshToken)
	}
}

func TestRunLoginVerifiesScopesAndPrintsThePilotFactsFromTheFreshAccessToken(t *testing.T) {
	server := newFakeLoginVerificationServer(t, "CHARACTER:EVE:932683762", pilotSkills(), []map[string]any{})
	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = server.URL
	cfg.SSOBaseURL = server.URL
	cfg.ConfigDir = t.TempDir()
	cfg.CacheDir = t.TempDir()
	cfg.OpenBrowser = simulateConsent("the-auth-code")

	var buf strings.Builder
	err := cli.RunLogin(t.Context(), cfg, cli.LoginParams{ClientID: "my-client-id", RedirectURI: freeLoopbackRedirectURI(t)}, &buf)
	if err != nil {
		t.Fatalf("RunLogin: %v\noutput: %s", err, buf.String())
	}

	out := buf.String()
	// pilotSkills() is Trade 4 / Broker Relations 4 / Accounting 3 at zero
	// standings (spec §4): broker 1.800%, sales tax 5.025%, order limit 21.
	for _, want := range []string{"Test Pilot", "1.800%", "5.025%", "21"} {
		if !strings.Contains(out, want) {
			t.Errorf("got output %q, want it to contain %q", out, want)
		}
	}

	for _, gt := range server.grantTypes {
		if gt == "refresh_token" {
			t.Fatalf("got a refresh_token grant during login verification (grant types seen: %v), want verification to reuse the freshly minted access token instead of performing a second refresh", server.grantTypes)
		}
	}
}

func TestRunLoginRefusesToOverwriteAWorkingStoredRefreshTokenAndNamesTheCharacter(t *testing.T) {
	server := newFakeLoginVerificationServer(t, "CHARACTER:EVE:932683762", pilotSkills(), []map[string]any{})
	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = server.URL
	cfg.SSOBaseURL = server.URL
	cfg.ConfigDir = t.TempDir()
	cfg.CacheDir = t.TempDir()
	cfg.Credentials = config.Credentials{
		ClientID:     "existing-client-id",
		RedirectURI:  cli.DefaultRedirectURI,
		RefreshToken: "working-refresh-token",
	}

	browserOpened := false
	cfg.OpenBrowser = func(string) error {
		browserOpened = true
		return nil
	}

	var buf strings.Builder
	err := cli.RunLogin(t.Context(), cfg, cli.LoginParams{RedirectURI: freeLoopbackRedirectURI(t)}, &buf)
	if err == nil {
		t.Fatalf("got no error for a working stored refresh token without --force, want a refusal\noutput: %s", buf.String())
	}

	if !strings.Contains(buf.String(), "Test Pilot") {
		t.Errorf("got output %q, want it to name the character by name (Test Pilot)", buf.String())
	}

	if browserOpened {
		t.Error("got the browser opened, want the reuse guard to refuse before ever starting a new authorization round-trip")
	}

	for _, gt := range server.grantTypes {
		if gt == "authorization_code" {
			t.Fatalf("got an authorization_code grant (grant types seen: %v), want only a refresh_token grant while checking reuse", server.grantTypes)
		}
	}
}

func TestRunLoginForceReauthorizesAndReplacesCredentialsEvenThoughTheStoredRefreshTokenStillWorks(t *testing.T) {
	server := newFakeLoginVerificationServer(t, "CHARACTER:EVE:932683762", pilotSkills(), []map[string]any{})
	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = server.URL
	cfg.SSOBaseURL = server.URL
	cfg.ConfigDir = t.TempDir()
	cfg.CacheDir = t.TempDir()
	cfg.Credentials = config.Credentials{
		ClientID:     "existing-client-id",
		RedirectURI:  cli.DefaultRedirectURI,
		RefreshToken: "working-refresh-token",
	}
	cfg.OpenBrowser = simulateConsent("the-auth-code")

	redirectURI := freeLoopbackRedirectURI(t)
	var buf strings.Builder
	err := cli.RunLogin(t.Context(), cfg, cli.LoginParams{ClientID: "existing-client-id", RedirectURI: redirectURI, Force: true}, &buf)
	if err != nil {
		t.Fatalf("RunLogin with --force: %v\noutput: %s", err, buf.String())
	}

	foundAuthCodeGrant := false
	for _, gt := range server.grantTypes {
		if gt == "authorization_code" {
			foundAuthCodeGrant = true
		}
	}
	if !foundAuthCodeGrant {
		t.Fatalf("got grant types %v, want --force to perform a fresh authorization_code exchange even though the stored refresh token still works", server.grantTypes)
	}

	creds, err := config.LoadCredentials(filepath.Join(cfg.ConfigDir, "credentials.json"))
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if creds.RedirectURI != redirectURI {
		t.Errorf("got redirect uri %q, want the freshly re-authorized %q", creds.RedirectURI, redirectURI)
	}
}

func TestRunLoginProceedsWithoutForceWhenTheStoredRefreshTokenNoLongerWorks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v2/oauth/token":
			_ = r.ParseForm()
			if r.PostForm.Get("grant_type") == "refresh_token" {
				http.Error(w, "invalid_grant", http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  fakeJWT("CHARACTER:EVE:932683762"),
				"token_type":    "Bearer",
				"expires_in":    1200,
				"refresh_token": "brand-new-refresh-token",
			})
		case strings.HasSuffix(r.URL.Path, "/skills/"):
			json.NewEncoder(w).Encode(map[string]any{"skills": pilotSkills()})
		case strings.HasSuffix(r.URL.Path, "/standings/"):
			json.NewEncoder(w).Encode([]map[string]any{})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = server.URL
	cfg.SSOBaseURL = server.URL
	cfg.ConfigDir = t.TempDir()
	cfg.CacheDir = t.TempDir()
	cfg.Credentials = config.Credentials{
		ClientID:     "existing-client-id",
		RedirectURI:  cli.DefaultRedirectURI,
		RefreshToken: "stale-refresh-token",
	}
	cfg.OpenBrowser = simulateConsent("the-auth-code")

	var buf strings.Builder
	err := cli.RunLogin(t.Context(), cfg, cli.LoginParams{ClientID: "existing-client-id", RedirectURI: freeLoopbackRedirectURI(t)}, &buf)
	if err != nil {
		t.Fatalf("RunLogin with a stale stored refresh token (no --force): %v\noutput: %s", err, buf.String())
	}

	creds, err := config.LoadCredentials(filepath.Join(cfg.ConfigDir, "credentials.json"))
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if creds.RefreshToken != "brand-new-refresh-token" {
		t.Errorf("got refresh token %q, want the freshly authorized brand-new-refresh-token", creds.RefreshToken)
	}
}

func TestRunLoginPromptsForTheClientIDWhenNeitherFlagNorStoredCredentialsGiveOne(t *testing.T) {
	server := newFakeLoginVerificationServer(t, "CHARACTER:EVE:932683762", pilotSkills(), []map[string]any{})
	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = server.URL
	cfg.SSOBaseURL = server.URL
	cfg.ConfigDir = t.TempDir()
	cfg.CacheDir = t.TempDir()
	cfg.OpenBrowser = simulateConsent("the-auth-code")
	cfg.Stdin = strings.NewReader("prompted-client-id\n")

	var buf strings.Builder
	err := cli.RunLogin(t.Context(), cfg, cli.LoginParams{RedirectURI: freeLoopbackRedirectURI(t)}, &buf)
	if err != nil {
		t.Fatalf("RunLogin with no --client-id and no stored credentials: %v\noutput: %s", err, buf.String())
	}

	if !strings.Contains(buf.String(), "client id") {
		t.Errorf("got output %q, want a prompt mentioning the client id", buf.String())
	}

	creds, err := config.LoadCredentials(filepath.Join(cfg.ConfigDir, "credentials.json"))
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if creds.ClientID != "prompted-client-id" {
		t.Errorf("got client id %q, want the prompted prompted-client-id", creds.ClientID)
	}
}

func TestLoginCommandForceFlagSkipsTheReuseGuard(t *testing.T) {
	server := newFakeLoginVerificationServer(t, "CHARACTER:EVE:932683762", pilotSkills(), []map[string]any{})
	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = server.URL
	cfg.SSOBaseURL = server.URL
	cfg.ConfigDir = t.TempDir()
	cfg.CacheDir = t.TempDir()
	cfg.Credentials = config.Credentials{
		ClientID:     "existing-client-id",
		RedirectURI:  cli.DefaultRedirectURI,
		RefreshToken: "working-refresh-token",
	}
	cfg.OpenBrowser = simulateConsent("the-auth-code")

	root := cli.NewRootCmd(cfg)
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"login", "--client-id", "existing-client-id", "--redirect-uri", freeLoopbackRedirectURI(t), "--force"})

	if err := root.Execute(); err != nil {
		t.Fatalf("login --force: %v\noutput: %s", err, out.String())
	}

	found := false
	for _, gt := range server.grantTypes {
		if gt == "authorization_code" {
			found = true
		}
	}
	if !found {
		t.Fatalf("got grant types %v, want --force wired through the command to perform a fresh authorization_code exchange", server.grantTypes)
	}
}

func TestLoginCommandDoesNotRequireTheClientIDFlagWhenOneIsAlreadyStored(t *testing.T) {
	server := newFakeLoginVerificationServer(t, "CHARACTER:EVE:932683762", pilotSkills(), []map[string]any{})
	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = server.URL
	cfg.SSOBaseURL = server.URL
	cfg.ConfigDir = t.TempDir()
	cfg.CacheDir = t.TempDir()
	cfg.Credentials = config.Credentials{ClientID: "stored-client-id"}
	cfg.OpenBrowser = simulateConsent("the-auth-code")

	root := cli.NewRootCmd(cfg)
	var out strings.Builder
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"login", "--redirect-uri", freeLoopbackRedirectURI(t)})

	if err := root.Execute(); err != nil {
		t.Fatalf("login with no --client-id flag but a stored client id: %v\noutput: %s", err, out.String())
	}

	creds, err := config.LoadCredentials(filepath.Join(cfg.ConfigDir, "credentials.json"))
	if err != nil {
		t.Fatalf("LoadCredentials: %v", err)
	}
	if creds.ClientID != "stored-client-id" {
		t.Errorf("got client id %q, want the remembered stored-client-id", creds.ClientID)
	}
}

func TestRunLoginFailsWithANonZeroErrorWhenScopeVerificationFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v2/oauth/token":
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  fakeJWT("CHARACTER:EVE:932683762"),
				"token_type":    "Bearer",
				"expires_in":    1200,
				"refresh_token": "rotated-refresh-token",
			})
		default:
			// No scope granted for skills/standings: ESI would 403 here for a
			// token missing the required scopes.
			http.Error(w, "missing scope", http.StatusForbidden)
		}
	}))
	t.Cleanup(server.Close)

	cfg := cli.DefaultConfig()
	cfg.ESIBaseURL = server.URL
	cfg.SSOBaseURL = server.URL
	cfg.ConfigDir = t.TempDir()
	cfg.CacheDir = t.TempDir()
	cfg.OpenBrowser = simulateConsent("the-auth-code")

	var buf strings.Builder
	err := cli.RunLogin(t.Context(), cfg, cli.LoginParams{ClientID: "my-client-id", RedirectURI: freeLoopbackRedirectURI(t)}, &buf)
	if err == nil {
		t.Fatalf("got no error for a scope verification failure, want a non-zero error\noutput: %s", buf.String())
	}
}
