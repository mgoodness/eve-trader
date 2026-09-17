// Package server wires the eve-trader HTTP routes together: the
// ESIGateway seam, the SQLite database, and the opportunity-table route
// (the dense sortable table decided in docs/spec/v1.md §7, backed by the
// real ranking query in package ranking).
package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/tokencrypt"
	"github.com/mgoodness/eve-trader/ranking"
)

// errNoStoredToken reports that no esi_token row exists at all -- the
// first-boot state before the one-time EVE SSO login. It is distinct from a
// refresh failure of a stored token: both leave the app unauthenticated and
// latch the re-authentication banner, but only the former leaves the skill
// poller with nothing to do.
var errNoStoredToken = errors.New("no stored ESI token")

// Server is the eve-trader HTTP handler.
type Server struct {
	gateway esi.ESIGateway
	db      *sql.DB
	mux     *http.ServeMux
	auth    AuthConfig

	mu             sync.RWMutex
	authChecked    bool
	reauthRequired bool
	// reauthFirstBoot records that the latched re-authentication state is
	// first boot (no token has ever been stored) rather than a failed
	// refresh, so the banner can ask for the initial login accurately.
	reauthFirstBoot bool
}

// New builds a Server that serves ESI/database-backed routes using
// gateway and db. auth configures the /auth/login and /auth/callback
// EVE SSO login flow (see auth.go).
func New(gateway esi.ESIGateway, db *sql.DB, auth AuthConfig) *Server {
	s := &Server{gateway: gateway, db: db, auth: auth}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.HandleFunc("GET /opportunities", s.handleOpportunities)
	mux.HandleFunc("GET /auth/login", s.handleAuthLogin)
	mux.HandleFunc("GET /auth/callback", s.handleAuthCallback)
	s.mux = mux

	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// pageData is the data handed to the "page.html" template.
type pageData struct {
	Opportunities []ranking.Opportunity
	Reauth        bool
	FirstBoot     bool
}

// handleIndex renders the opportunity table: one row per item that clears
// the v1 filter thresholds, ranked ISK/day descending by default (see
// docs/spec/v1.md §4/§7). Market data, item names, and character skills
// are read directly from SQLite -- no ESIGateway call is made here (that
// arrives with live polling in a later ticket).
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if failed, firstBoot := s.authenticationFailed(r.Context()); failed {
		s.renderIndex(w, pageData{Reauth: true, FirstBoot: firstBoot})
		return
	}

	opportunities, err := ranking.Load(r.Context(), s.db)
	if err != nil {
		slog.Error("loading opportunities", "err", err)
		http.Error(w, "loading opportunities", http.StatusInternalServerError)
		return
	}

	s.renderIndex(w, pageData{Opportunities: opportunities})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.db.PingContext(r.Context()); err != nil {
		http.Error(w, "unhealthy", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) renderIndex(w http.ResponseWriter, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "page.html", data); err != nil {
		slog.Error("rendering index", "err", err)
	}
}

// authenticationFailed performs the one refresh attempt needed before live
// ESI work. Both a failed refresh and the absence of a stored token count as
// unauthenticated, and both are latched: requests must not create a silent
// retry loop while the user is being asked to authenticate again. firstBoot
// distinguishes "no credential has ever been stored" from a refresh failure
// so callers can word the prompt accurately.
func (s *Server) authenticationFailed(ctx context.Context) (failed, firstBoot bool) {
	s.mu.Lock()
	checked := s.authChecked
	failed = s.reauthRequired
	firstBoot = s.reauthFirstBoot
	s.mu.Unlock()
	if checked {
		return failed, firstBoot
	}
	_, _, err := s.refreshAuthentication(ctx, false)
	if err == nil {
		return false, false
	}
	s.mu.RLock()
	firstBoot = s.reauthFirstBoot
	s.mu.RUnlock()
	return true, firstBoot
}

// refreshAuthentication mints a current access token from the stored refresh
// token. All authenticated callers use this path, so a refresh failure -- and
// the absence of any stored token -- latches the same re-authentication state
// rather than implementing separate handling.
func (s *Server) refreshAuthentication(ctx context.Context, force bool) (esi.Token, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.reauthRequired {
		return esi.Token{}, false, errors.New("reauthentication required")
	}
	if !force && s.authChecked {
		return esi.Token{}, false, nil
	}

	var characterID int
	var ciphertext []byte
	if err := s.db.QueryRowContext(ctx, `SELECT character_id, encrypted_refresh_token FROM esi_token LIMIT 1`).Scan(&characterID, &ciphertext); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.reauthFirstBoot = true
			return s.latchAuthFailure(errNoStoredToken)
		}
		return s.latchAuthFailure(fmt.Errorf("loading stored ESI token: %w", err))
	}
	refreshToken, err := tokencrypt.Decrypt(s.auth.TokenKey, ciphertext)
	if err != nil {
		return s.latchAuthFailure(fmt.Errorf("decrypting stored ESI refresh token: %w", err))
	}
	refreshed, err := s.gateway.RefreshToken(ctx, refreshToken)
	if err != nil {
		return s.latchAuthFailure(fmt.Errorf("refreshing ESI token: %w", err))
	}
	if refreshed.RefreshToken != "" && refreshed.RefreshToken != refreshToken {
		encrypted, err := tokencrypt.Encrypt(s.auth.TokenKey, refreshed.RefreshToken)
		if err != nil {
			return s.latchAuthFailure(fmt.Errorf("encrypting rotated ESI refresh token: %w", err))
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE esi_token SET encrypted_refresh_token = ?, updated_at = ? WHERE character_id = ?`, encrypted, nowUTC(), characterID); err != nil {
			return s.latchAuthFailure(fmt.Errorf("storing rotated ESI refresh token: %w", err))
		}
	}
	s.authChecked = true
	return refreshed, true, nil
}

func (s *Server) latchAuthFailure(err error) (esi.Token, bool, error) {
	s.reauthRequired = true
	s.authChecked = true
	slog.Error("authenticated ESI work stopped until re-authentication", "err", err)
	return esi.Token{}, false, err
}

func (s *Server) resetAuthentication() {
	s.mu.Lock()
	s.authChecked, s.reauthRequired, s.reauthFirstBoot = false, false, false
	s.mu.Unlock()
}

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }

// handleOpportunities serves the htmx sort partial: it re-runs the same
// ranking query, re-sorts by the requested column (?sort=buy|sell|margin|
// iskunit|volday|iskday), and returns just the re-ordered <tr> rows for
// an innerHTML swap into <tbody id="rows">.
func (s *Server) handleOpportunities(w http.ResponseWriter, r *http.Request) {
	opportunities, err := ranking.Load(r.Context(), s.db)
	if err != nil {
		slog.Error("loading opportunities", "err", err)
		http.Error(w, "loading opportunities", http.StatusInternalServerError)
		return
	}
	ranking.Sort(opportunities, r.URL.Query().Get("sort"))

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "rows", opportunities); err != nil {
		slog.Error("rendering opportunities partial", "err", err)
	}
}
