// Package web wires the app's net/http server: routes, and handlers that
// combine the esi.Client seam with storage.Store.
package web

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/storage"
)

// Server holds the app's dependencies and routes requests to handlers.
type Server struct {
	esi   esi.Client
	store *storage.Store
	mux   *http.ServeMux
}

// NewServer builds a Server wired to the given ESIClient and Store.
func NewServer(esiClient esi.Client, store *storage.Store) *Server {
	s := &Server{esi: esiClient, store: store, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
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
