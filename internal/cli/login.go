package cli

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/config"
	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/spf13/cobra"
)

// newLoginCmd builds the `login` command (spec §12; tickets #28-#30): it
// runs the Authorization Code + PKCE round-trip and stores the resulting
// credentials. --client-id is optional: a previously-stored client id is
// reused, and RunLogin prompts on stdin when neither is available. The
// reuse guard (refuse to overwrite a working stored refresh token unless
// --force is passed) and --force itself live in RunLogin.
func newLoginCmd(cfg Config) *cobra.Command {
	var params LoginParams

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authorize eve-trader against EVE SSO and store credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			runCfg := cfg
			if runCfg.Stdin == nil {
				runCfg.Stdin = cmd.InOrStdin()
			}
			return RunLogin(cmd.Context(), runCfg, params, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&params.ClientID, "client-id", "", "the EVE SSO application client id (remembered after the first successful login; prompted on stdin if neither given nor stored)")
	cmd.Flags().StringVar(&params.RedirectURI, "redirect-uri", DefaultRedirectURI, "the registered loopback redirect URI")
	cmd.Flags().BoolVar(&params.Force, "force", false, "re-authorize even if the stored refresh token still works, replacing credentials.json")

	return cmd
}

// DefaultRedirectURI is the registered loopback callback eve-trader asks
// EVE SSO to redirect back to when no --redirect-uri flag is given (spec
// §12: "Redirect is the loopback http://127.0.0.1:8000/callback (verified
// accepted)").
const DefaultRedirectURI = "http://127.0.0.1:8000/callback"

// LoginParams are the per-invocation inputs to RunLogin that live outside
// Config (ticket #28): the client id and the redirect URI, the only two
// flags this ticket's scope covers.
type LoginParams struct {
	ClientID    string
	RedirectURI string

	// Force skips the reuse guard and re-authorizes even when the stored
	// refresh token still works, atomically replacing credentials.json
	// (ticket #30).
	Force bool
}

// RunLogin drives the OAuth 2.0 Authorization Code + PKCE round-trip
// against EVE SSO end to end (spec §12; ticket #28): it builds the
// authorize URL with a freshly generated PKCE S256 challenge and a random
// state, prints the URL, best-effort opens it in a browser, binds a
// loopback listener on the redirect URI's host:port, waits for the
// callback, rejects a mismatched state before anything is written,
// exchanges the authorization code for tokens with no client secret, and
// writes credentials.json at mode 600 with the client id, redirect URI,
// and the (possibly rotated) refresh token.
//
// Once the token exchange has succeeded and credentials are written, it
// verifies the granted scopes by reading the character's skills and
// standings through the same access-token path pilotFacts uses for a
// normal run (ticket #29), with the access token the exchange just
// minted -- never a second refresh-token exchange -- and prints the
// character id, broker fee, sales tax, and order limit it derived. A
// verification failure is returned as a plain error, so a wrong scope or
// the wrong character fails at login rather than part-way through a
// market scan.
//
// Before any of that, RunLogin runs the reuse guard (ticket #30): unless
// params.Force is set, a stored refresh token (cfg.Credentials) that
// still refreshes means eve-trader is already logged in, so RunLogin
// prints the authenticated character (reusing pilotFacts -- the same
// refresh+verify path a normal run uses, which also persists any token
// rotation the check itself triggered) and returns a plain error without
// ever starting a new authorization round-trip. A stored refresh token
// that no longer refreshes is not a reuse-guard failure -- it falls
// through to a fresh login below, which is what makes `login` "safe to
// re-run" even after the stored session has gone stale.
//
// RunLogin also resolves the client id (ticket #30): params.ClientID
// (the --client-id flag) wins, then cfg.Credentials.ClientID (remembered
// from a previous successful login), and finally a prompt on cfg.Stdin
// (os.Stdin if unset). Whichever id is resolved is the one written to
// credentials.json on success, so it never needs to be passed or typed
// again.
//
// The headless stdin fallback for the callback itself is out of this
// ticket's scope.
func RunLogin(ctx context.Context, cfg Config, params LoginParams, w io.Writer) error {
	redirectURI := params.RedirectURI
	if redirectURI == "" {
		redirectURI = DefaultRedirectURI
	}

	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		return fmt.Errorf("opening cache: %w", err)
	}

	if !params.Force && cfg.Credentials.RefreshToken != "" {
		if err := refuseIfStoredCredentialsStillWork(ctx, cfg, store, w); err != nil {
			return err
		}
	}

	clientID, err := resolveClientID(params.ClientID, cfg, w)
	if err != nil {
		return err
	}

	listener, callbackPath, err := newLoopbackListener(redirectURI)
	if err != nil {
		return fmt.Errorf("binding loopback listener for %s: %w", redirectURI, err)
	}
	defer listener.Close()

	verifier, challenge, err := esi.GeneratePKCE()
	if err != nil {
		return err
	}
	state, err := randomState()
	if err != nil {
		return err
	}

	authorizeURL := esi.AuthorizeURL(cfg.SSOBaseURL, clientID, redirectURI, state, challenge)
	fmt.Fprintf(w, "Open this URL to authorize eve-trader:\n%s\n", authorizeURL)

	// The listener must already be serving before the browser (real or
	// faked) can be opened: a real browser's request would otherwise just
	// queue; a synchronous fake opener (as tests use, to drive the callback
	// itself) would deadlock waiting for a response nothing is serving yet.
	results, errs := serveOneCallback(ctx, listener, callbackPath)

	open := cfg.OpenBrowser
	if open == nil {
		open = defaultOpenBrowser
	}
	// Best-effort only: the consent URL above works regardless of whether a
	// browser could be opened (ticket #28 acceptance criterion).
	_ = open(authorizeURL)

	select {
	case result := <-results:
		if result.state != state {
			return fmt.Errorf("callback state %q does not match the state eve-trader sent; aborting (possible CSRF)", result.state)
		}

		client := esi.NewClient(esi.ClientOptions{
			BaseURL:    cfg.ESIBaseURL,
			SSOBaseURL: cfg.SSOBaseURL,
			UserAgent:  cfg.UserAgent,
			CompatDate: cfg.CompatDate,
		})
		token, err := client.ExchangeAuthorizationCode(ctx, clientID, redirectURI, result.code, verifier)
		if err != nil {
			return fmt.Errorf("exchanging authorization code: %w", err)
		}

		creds := config.Credentials{
			ClientID:     clientID,
			RedirectURI:  redirectURI,
			RefreshToken: token.RefreshToken,
		}
		path := filepath.Join(cfg.ConfigDir, "credentials.json")
		if err := config.SaveCredentials(path, creds); err != nil {
			return fmt.Errorf("saving credentials: %w", err)
		}

		fmt.Fprintf(w, "Logged in; credentials saved to %s\n", path)

		// Verification lives after credentials are written (RunLogin's doc
		// comment above): a wrong scope or character still means eve-trader
		// authorized against EVE SSO successfully, but RunLogin fails loudly
		// rather than silently handing back a success a market scan would
		// later fail on.
		return verifyScopes(ctx, cfg, client, token.AccessToken, w)
	case err := <-errs:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// callbackResult is the authorization code and state the loopback
// listener captured off the callback request's query string.
type callbackResult struct {
	code  string
	state string
}

// newLoopbackListener parses redirectURI and binds a TCP listener on its
// host:port (ticket #28 acceptance criterion: "--redirect-uri overrides
// the default and the listener binds its host:port").
func newLoopbackListener(redirectURI string) (net.Listener, string, error) {
	u, err := url.Parse(redirectURI)
	if err != nil {
		return nil, "", fmt.Errorf("parsing redirect URI %q: %w", redirectURI, err)
	}
	listener, err := net.Listen("tcp", u.Host)
	if err != nil {
		return nil, "", err
	}
	return listener, u.Path, nil
}

// serveOneCallback serves exactly one request on path over listener,
// captures its code/state (or its error parameter) onto the returned
// channels, and shuts the server down once that request lands or ctx is
// cancelled.
func serveOneCallback(ctx context.Context, listener net.Listener, path string) (<-chan callbackResult, <-chan error) {
	results := make(chan callbackResult, 1)
	errs := make(chan error, 1)

	mux := http.NewServeMux()
	server := &http.Server{Handler: mux}

	mux.HandleFunc(path, func(rw http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if authErr := q.Get("error"); authErr != "" {
			http.Error(rw, "authorization failed", http.StatusBadRequest)
			select {
			case errs <- fmt.Errorf("EVE SSO returned an authorization error: %s", authErr):
			default:
			}
			go server.Shutdown(context.Background())
			return
		}

		fmt.Fprintln(rw, "You may close this window and return to eve-trader.")
		select {
		case results <- callbackResult{code: q.Get("code"), state: q.Get("state")}:
		default:
		}
		go server.Shutdown(context.Background())
	})

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			select {
			case errs <- err:
			default:
			}
		}
	}()

	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	return results, errs
}

// randomState generates a random, URL-safe CSRF state value for the
// authorization request (spec §12: "state ... required by EVE SSO, must
// be verified on return").
func randomState() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generating state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// verifyScopes proves the scopes EVE SSO just granted are the ones
// eve-trader needs by reading skills and standings through
// pilotFactsForAccessToken with accessToken -- the one the authorization-code
// exchange just minted -- rather than performing a second, redundant
// refresh-token exchange (ticket #29: "reuses the existing pilot-facts path
// for verification, so there is a single access-token/refresh code path").
// On success it prints the character id and the derived fee rates/order
// limit; any failure (a missing scope, the wrong character, a transient ESI
// error) is returned as a plain error so RunLogin fails loudly rather than
// reporting a success a later market scan would fail on.
func verifyScopes(ctx context.Context, cfg Config, client *esi.Client, accessToken string, w io.Writer) error {
	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		return fmt.Errorf("opening cache for scope verification: %w", err)
	}

	characterID, err := esi.CharacterIDFromAccessToken(accessToken)
	if err != nil {
		return fmt.Errorf("decoding character id from access token: %w", err)
	}

	facts, err := pilotFactsForAccessToken(ctx, cfg, store, client, accessToken)
	if err != nil {
		return fmt.Errorf("verifying granted scopes (reading skills and standings): %w", err)
	}

	fmt.Fprintf(w, "Verified scopes for character %d: broker fee %.3f%%, sales tax %.3f%%, order limit %d\n",
		characterID, facts.Fees.Broker*100, facts.Fees.SalesTax*100, facts.OrderLimit)
	return nil
}

// refuseIfStoredCredentialsStillWork is `login`'s reuse guard (ticket #30,
// parent #26: "If a stored refresh token still refreshes, login prints the
// authenticated character and exits non-zero without --force"). It reuses
// pilotFacts -- the same refresh+verify path (and rotated-refresh-token
// persistence) a normal run uses -- against cfg.Credentials, so there is a
// single access-token/refresh code path whether `login` or `recommend` is
// checking it.
//
// A stored refresh token that still refreshes is reported as a non-nil
// error naming the character, which the caller returns straight to the
// pilot without ever starting a fresh authorization round-trip. A stored
// refresh token that fails to refresh (expired, revoked, wrong client)
// is not treated as a guard failure: it returns nil so RunLogin falls
// through to a fresh login, which is what keeps `login` safe to re-run
// even once the stored session has gone stale.
func refuseIfStoredCredentialsStillWork(ctx context.Context, cfg Config, store *cache.Store, w io.Writer) error {
	facts, err := pilotFacts(ctx, cfg, store)
	if err != nil {
		// The stored refresh token no longer works (or some other transient
		// failure occurred) -- either way, there is nothing working to
		// refuse overwriting, so fall through to a fresh login.
		return nil
	}

	fmt.Fprintf(w, "Stored credentials already work for character %d: broker fee %.3f%%, sales tax %.3f%%, order limit %d\n",
		facts.CharacterID, facts.Fees.Broker*100, facts.Fees.SalesTax*100, facts.OrderLimit)
	return fmt.Errorf("refusing to overwrite working credentials for character %d; rerun with --force to re-authorize", facts.CharacterID)
}

// resolveClientID picks the client id a login run authorizes with (ticket
// #30): the --client-id flag (flagClientID) wins, then a client id
// remembered from a previous successful login (cfg.Credentials.ClientID),
// and finally a prompt on cfg.Stdin (os.Stdin if unset) -- so a pilot
// types or passes the client id exactly once, ever.
func resolveClientID(flagClientID string, cfg Config, w io.Writer) (string, error) {
	if flagClientID != "" {
		return flagClientID, nil
	}
	if cfg.Credentials.ClientID != "" {
		return cfg.Credentials.ClientID, nil
	}
	return promptForClientID(cfg.Stdin, w)
}

// promptForClientID prompts on w and reads a single line off in (os.Stdin
// if in is nil), trimming surrounding whitespace. It is the fallback
// resolveClientID uses when neither --client-id nor a stored
// credentials.json gives a client id (ticket #30 acceptance criterion).
func promptForClientID(in io.Reader, w io.Writer) (string, error) {
	if in == nil {
		in = os.Stdin
	}

	fmt.Fprint(w, "Enter your EVE SSO application client id: ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("reading client id from stdin: %w", err)
	}

	clientID := strings.TrimSpace(line)
	if clientID == "" {
		return "", fmt.Errorf("no client id given; pass --client-id or let a previous login remember one")
	}
	return clientID, nil
}
