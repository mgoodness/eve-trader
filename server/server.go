// Package server wires the eve-trader HTTP routes together: the
// ESIGateway seam, the SQLite database, and the opportunity-table route
// (the dense sortable table decided in docs/spec/v1.md §7, backed by the
// real ranking query in package ranking).
package server

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/ranking"
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
	mux.HandleFunc("GET /opportunities", s.handleOpportunities)
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
}

// handleIndex renders the opportunity table: one row per item that clears
// the v1 filter thresholds, ranked ISK/day descending by default (see
// docs/spec/v1.md §4/§7). Market data, item names, and character skills
// are read directly from SQLite -- no ESIGateway call is made here (that
// arrives with live polling in a later ticket).
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	opportunities, err := ranking.Load(r.Context(), s.db)
	if err != nil {
		slog.Error("loading opportunities", "err", err)
		http.Error(w, "loading opportunities", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "page.html", pageData{Opportunities: opportunities}); err != nil {
		slog.Error("rendering index", "err", err)
	}
}

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
