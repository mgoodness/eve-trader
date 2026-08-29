// Package web wires the app's net/http server: routes, and handlers that
// combine the esi.Client seam with storage.Store.
package web

import (
	"embed"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/mgoodness/eve-trader/internal/auth"
	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
	"github.com/mgoodness/eve-trader/internal/notify"
	"github.com/mgoodness/eve-trader/internal/scanner"
	"github.com/mgoodness/eve-trader/internal/storage"
)

//go:embed templates/dashboard.html
var templateFS embed.FS

var dashboardTemplate = template.Must(template.ParseFS(templateFS, "templates/dashboard.html"))

// Server holds the app's dependencies and routes requests to handlers.
type Server struct {
	esi         esi.Client
	store       *storage.Store
	notifier    *notify.Notifier
	clientID    string
	callbackURL string
	mux         *http.ServeMux

	mu           sync.Mutex
	pendingState string
}

// NewServer builds a Server wired to the given ESIClient, Store, and
// Notifier (for Discord Alert delivery -- pass one with an empty
// WebhookURL to disable it). Login uses the EVE SSO application
// identified by clientID, redirecting back to callbackURL on completion
// (see internal/auth).
func NewServer(esiClient esi.Client, store *storage.Store, notifier *notify.Notifier, clientID, callbackURL string) *Server {
	s := &Server{
		esi:         esiClient,
		store:       store,
		notifier:    notifier,
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
	s.mux.HandleFunc("GET /{$}", s.handleDashboard)
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

	http.Redirect(w, r, "/", http.StatusFound)
}

// handleDashboard renders the split-panel dashboard: a fixed Orders
// sidebar with the authenticated character's real open orders and Alert
// badges, a real Alert Feed panel, and a hub-tabbed, filterable
// Opportunity Scanner panel. If nobody's logged in yet, it renders a
// login prompt instead.
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	characterID, err := auth.CurrentCharacterID(ctx, s.store)
	if errors.Is(err, storage.ErrNoToken) {
		s.renderDashboard(w, dashboardView{
			Authenticated: false,
			LoginURL:      "/auth/login",
		})
		return
	}
	if err != nil {
		log.Printf("dashboard: %v", err)
		http.Error(w, "failed to determine authenticated character", http.StatusInternalServerError)
		return
	}

	selectedHub := parseHubParam(r)
	filter := parseFilterParams(r)

	view, err := buildDashboardView(ctx, s.esi, s.store, s.notifier, characterID, selectedHub, filter, time.Now())
	if err != nil {
		log.Printf("dashboard: %v", err)
		http.Error(w, "failed to build dashboard", http.StatusBadGateway)
		return
	}

	s.renderDashboard(w, view)
}

// parseHubParam reads the ?hub= query parameter (default Jita, falling
// back to Jita for an unrecognized value).
func parseHubParam(r *http.Request) hub.Hub {
	if h, ok := hub.ByName(r.URL.Query().Get("hub")); ok {
		return h
	}
	return hub.Jita
}

// parseFilterParams reads the Opportunity panel's configurable
// ?minVolume=/?minMargin= query parameters (default 0: no filtering).
func parseFilterParams(r *http.Request) scanner.Filter {
	return scanner.Filter{
		MinVolume: parseFloatParam(r, "minVolume"),
		MinMargin: parseFloatParam(r, "minMargin"),
	}
}

func parseFloatParam(r *http.Request, name string) float64 {
	v, err := strconv.ParseFloat(r.URL.Query().Get(name), 64)
	if err != nil {
		return 0
	}
	return v
}

func (s *Server) renderDashboard(w http.ResponseWriter, view dashboardView) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := dashboardTemplate.Execute(w, view); err != nil {
		log.Printf("render dashboard: %v", err)
	}
}
