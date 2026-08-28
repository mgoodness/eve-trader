// Package web wires the app's net/http server: routes, and handlers that
// combine the esi.Client seam with storage.Store.
package web

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"sync"

	"github.com/mgoodness/eve-trader/internal/auth"
	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/storage"
)

// Server holds the app's dependencies and routes requests to handlers.
type Server struct {
	esi         esi.Client
	store       *storage.Store
	clientID    string
	callbackURL string
	mux         *http.ServeMux

	mu           sync.Mutex
	pendingState string
}

// NewServer builds a Server wired to the given ESIClient and Store. Login
// uses the EVE SSO application identified by clientID, redirecting back to
// callbackURL on completion (see internal/auth).
func NewServer(esiClient esi.Client, store *storage.Store, clientID, callbackURL string) *Server {
	s := &Server{
		esi:         esiClient,
		store:       store,
		clientID:    clientID,
		callbackURL: callbackURL,
		mux:         http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("GET /auth/login", s.handleLogin)
	s.mux.HandleFunc("GET /auth/callback", s.handleAuthCallback)
	s.mux.HandleFunc("GET /api/characters/{id}/orders", s.handleCharacterOrders)
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// handleLogin redirects to EVE SSO's authorize URL, stashing a CSRF state
// value in memory to be checked by handleAuthCallback. A single-character
// tool needs no broader session/user concept -- one pending login at a
// time is all there is.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	state, err := auth.RandomState()
	if err != nil {
		http.Error(w, "failed to start login", http.StatusInternalServerError)
		return
	}

	s.mu.Lock()
	s.pendingState = state
	s.mu.Unlock()

	http.Redirect(w, r, auth.LoginURL(s.clientID, s.callbackURL, state), http.StatusFound)
}

// handleAuthCallback completes the OAuth authorization-code exchange and
// persists the resulting token.
func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	wantState := s.pendingState
	s.mu.Unlock()

	if wantState == "" || r.URL.Query().Get("state") != wantState {
		http.Error(w, "invalid or expired login state", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	if _, err := auth.Exchange(r.Context(), s.esi, s.store, code); err != nil {
		log.Printf("auth callback: %v", err)
		http.Error(w, "authentication failed", http.StatusBadGateway)
		return
	}

	// Compare-and-clear: only clear the state this callback consumed, not
	// whatever's pending now -- a second /auth/login could otherwise start
	// while this exchange was in flight, and this clear would wipe out
	// that still-pending login's state instead of its own.
	s.mu.Lock()
	if s.pendingState == wantState {
		s.pendingState = ""
	}
	s.mu.Unlock()

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("Logged in.")); err != nil {
		log.Printf("write auth callback response: %v", err)
	}
}

// handleCharacterOrders exercises the ESIClient seam end-to-end: it fetches
// a character's open orders via esi.Client and renders them as JSON.
func (s *Server) handleCharacterOrders(w http.ResponseWriter, r *http.Request) {
	characterID, err := strconv.ParseInt(r.PathValue("id"), 10, 32)
	if err != nil {
		http.Error(w, "invalid character id", http.StatusBadRequest)
		return
	}

	orders, err := s.esi.CharacterOrders(r.Context(), int32(characterID))
	if err != nil {
		http.Error(w, "failed to fetch orders", http.StatusBadGateway)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(orders); err != nil {
		log.Printf("encode orders response: %v", err)
	}
}
