// Package server wires the eve-trader HTTP routes together: the
// ESIGateway seam, the SQLite database, and the opportunity-table route
// (the dense sortable table decided in docs/spec/v1.md §7, backed by the
// real ranking query in package ranking).
package server

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/tokencrypt"
	"github.com/mgoodness/eve-trader/ranking"
)

// Server is the eve-trader HTTP handler.
type Server struct {
	gateway esi.ESIGateway
	db      *sql.DB
	mux     *http.ServeMux
	auth    AuthConfig

	mu             sync.RWMutex
	authChecked    bool
	reauthRequired bool
}

// New builds a Server that serves ESI/database-backed routes using
// gateway and db. auth configures the /auth/login and /auth/callback
// EVE SSO login flow (see auth.go).
func New(gateway esi.ESIGateway, db *sql.DB, auth AuthConfig) *Server {
	s := &Server{gateway: gateway, db: db, auth: auth}

	mux := http.NewServeMux()
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
}

// handleIndex renders the opportunity table: one row per item that clears
// the v1 filter thresholds, ranked ISK/day descending by default (see
// docs/spec/v1.md §4/§7). Market data, item names, and character skills
// are read directly from SQLite -- no ESIGateway call is made here (that
// arrives with live polling in a later ticket).
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if s.authenticationFailed(r.Context()) {
		s.renderIndex(w, pageData{Reauth: true})
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

func (s *Server) renderIndex(w http.ResponseWriter, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "page.html", data); err != nil {
		slog.Error("rendering index", "err", err)
	}
}

// authenticationFailed performs the one refresh attempt needed before live
// ESI work. A failed refresh is latched: requests must not create a silent
// retry loop while the user is being asked to authenticate again.
func (s *Server) authenticationFailed(ctx context.Context) bool {
	// Serialize the first refresh. EVE refresh tokens rotate and are
	// single-use, so concurrent home requests must not refresh the same token.
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.authChecked {
		return s.reauthRequired
	}

	var characterID int
	var ciphertext []byte
	err := s.db.QueryRowContext(ctx, `SELECT character_id, encrypted_refresh_token FROM esi_token LIMIT 1`).Scan(&characterID, &ciphertext)
	if err == sql.ErrNoRows {
		s.authChecked = true
		return false
	}
	if err != nil {
		slog.Error("loading stored ESI token", "err", err)
		s.authChecked, s.reauthRequired = true, true
		return true
	}

	refreshToken, err := tokencrypt.Decrypt(s.auth.TokenKey, ciphertext)
	if err != nil {
		slog.Error("decrypting stored ESI refresh token", "err", err)
		s.authChecked, s.reauthRequired = true, true
		return true
	}
	refreshed, err := s.gateway.RefreshToken(ctx, refreshToken)
	if err != nil {
		slog.Error("refreshing ESI token; polling stopped until re-authentication", "err", err)
		s.authChecked, s.reauthRequired = true, true
		return true
	}
	if refreshed.RefreshToken != "" && refreshed.RefreshToken != refreshToken {
		encrypted, encryptErr := tokencrypt.Encrypt(s.auth.TokenKey, refreshed.RefreshToken)
		if encryptErr != nil {
			slog.Error("encrypting rotated ESI refresh token", "err", encryptErr)
			s.authChecked, s.reauthRequired = true, true
			return true
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE esi_token SET encrypted_refresh_token = ?, updated_at = ? WHERE character_id = ?`, encrypted, nowUTC(), characterID); err != nil {
			slog.Error("storing rotated ESI refresh token", "err", err)
			s.authChecked, s.reauthRequired = true, true
			return true
		}
	}
	s.authChecked = true
	return false
}

func (s *Server) setAuthState(checked, failed bool) {
	s.mu.Lock()
	s.authChecked, s.reauthRequired = checked, failed
	s.mu.Unlock()
}

func (s *Server) resetAuthentication() {
	s.mu.Lock()
	s.authChecked, s.reauthRequired = false, false
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
