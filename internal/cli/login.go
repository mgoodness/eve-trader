package cli

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"

	"github.com/mgoodness/eve-trader/internal/config"
	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/spf13/cobra"
)

// newLoginCmd builds the `login` command (spec §12; ticket #28): it runs
// the Authorization Code + PKCE round-trip and stores the resulting
// credentials. --client-id is required here; ticket #30 layers the
// reuse guard, --force, and remembering a previously-used client id on
// top.
func newLoginCmd(cfg Config) *cobra.Command {
	var params LoginParams

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authorize eve-trader against EVE SSO and store credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunLogin(cmd.Context(), cfg, params, cmd.OutOrStdout())
		},
	}

	cmd.Flags().StringVar(&params.ClientID, "client-id", "", "the EVE SSO application client id")
	cmd.Flags().StringVar(&params.RedirectURI, "redirect-uri", DefaultRedirectURI, "the registered loopback redirect URI")
	cmd.MarkFlagRequired("client-id")

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
// Scope verification, the reuse guard/--force, and the headless stdin
// fallback are out of this ticket's scope and deliberately not called
// here: RunLogin returns plainly on success, leaving its caller free to
// run a verification step (or a reuse check before ever calling RunLogin)
// without fighting an early os.Exit baked into this function.
func RunLogin(ctx context.Context, cfg Config, params LoginParams, w io.Writer) error {
	redirectURI := params.RedirectURI
	if redirectURI == "" {
		redirectURI = DefaultRedirectURI
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

	authorizeURL := esi.AuthorizeURL(cfg.SSOBaseURL, params.ClientID, redirectURI, state, challenge)
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

		client := esi.NewClient(esi.ClientOptions{SSOBaseURL: cfg.SSOBaseURL, UserAgent: cfg.UserAgent})
		token, err := client.ExchangeAuthorizationCode(ctx, params.ClientID, redirectURI, result.code, verifier)
		if err != nil {
			return fmt.Errorf("exchanging authorization code: %w", err)
		}

		creds := config.Credentials{
			ClientID:     params.ClientID,
			RedirectURI:  redirectURI,
			RefreshToken: token.RefreshToken,
		}
		path := filepath.Join(cfg.ConfigDir, "credentials.json")
		if err := config.SaveCredentials(path, creds); err != nil {
			return fmt.Errorf("saving credentials: %w", err)
		}

		fmt.Fprintf(w, "Logged in; credentials saved to %s\n", path)
		return nil
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
