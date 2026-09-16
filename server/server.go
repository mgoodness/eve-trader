// Package server wires the eve-trader HTTP routes together: the
// ESIGateway seam, the SQLite database, and the (as-yet placeholder)
// opportunity-table route later tickets will fill in.
package server

import (
	"database/sql"
	"fmt"
	"net/http"

	"github.com/mgoodness/eve-trader/esi"
)

// Server is the eve-trader HTTP handler.
type Server struct {
	gateway esi.ESIGateway
	db      *sql.DB
	mux     *http.ServeMux
}

// New builds a Server that serves ESI/database-backed routes using
// gateway and db.
func New(gateway esi.ESIGateway, db *sql.DB) *Server {
	s := &Server{gateway: gateway, db: db}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	s.mux = mux

	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// handleIndex is a placeholder for the opportunity table (see
// eve-trader#13); it exists so the app has a running main route from the
// project's skeleton onward.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintln(w, "eve-trader: opportunity table not yet implemented")
}
