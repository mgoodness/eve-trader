// Portfolio route: the dense per-position table over the P/L engine,
// grouped by the decision each position demands (docs/spec/v2.md §7). The
// P/L engine owns every figure; this file only groups rows and shapes the
// view model.
package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/mgoodness/eve-trader/ledger"
)

// portfolioGroup is one decision group of the Portfolio table. Count is the
// group's full size; Positions is empty when the group has none, so the
// template can render the heading and an empty state.
type portfolioGroup struct {
	Title     string
	Count     int
	Positions []ledger.Position
}

// portfolioSummary is the four-cell Realised / Unrealised / Fees (est) /
// Unattributed strip.
type portfolioSummary struct {
	Realized         float64
	Unrealized       float64
	EstimatedFees    float64
	UnattributedFees float64
}

// portfolioData is the view model for "portfolio.html". The Reauth and
// FirstBoot fields let the same template serve the re-authentication
// banner when no valid credential exists.
type portfolioData struct {
	Groups    []portfolioGroup
	Summary   portfolioSummary
	Reauth    bool
	FirstBoot bool
}

// portfolioGroupOrder is the decision order the table renders in
// (docs/spec/v2.md §7). "Orders worth moving" slots in beside these when
// the re-list-gain ticket lands.
var portfolioGroupOrder = []struct {
	status ledger.Status
	title  string
}{
	{ledger.StatusAtTarget, "At target now"},
	{ledger.StatusBelowTarget, "Below target"},
	{ledger.StatusTransfer, "Transfers"},
	{ledger.StatusNoMarket, "No market"},
	{ledger.StatusClosed, "Closed / realized"},
}

// buildPortfolioData groups the report's positions and copies the summary
// figures. Empty groups still render, with a count of zero.
func buildPortfolioData(report ledger.Report) portfolioData {
	byStatus := make(map[ledger.Status][]ledger.Position, len(portfolioGroupOrder))
	for _, p := range report.Positions {
		byStatus[p.Status] = append(byStatus[p.Status], p)
	}

	groups := make([]portfolioGroup, 0, len(portfolioGroupOrder))
	for _, g := range portfolioGroupOrder {
		positions := byStatus[g.status]
		groups = append(groups, portfolioGroup{Title: g.title, Count: len(positions), Positions: positions})
	}

	return portfolioData{
		Groups: groups,
		Summary: portfolioSummary{
			Realized:         report.Realized,
			Unrealized:       report.Unrealized,
			EstimatedFees:    report.EstimatedFees,
			UnattributedFees: report.UnattributedFees,
		},
	}
}

// handlePortfolio renders the Portfolio view. Like the opportunity table it
// reads the local ledger directly -- no ESIGateway call -- but it derives
// the report from the authenticated character's records.
func (s *Server) handlePortfolio(w http.ResponseWriter, r *http.Request) {
	if failed, firstBoot := s.authenticationFailed(r.Context()); failed {
		s.renderPortfolio(w, portfolioData{Reauth: true, FirstBoot: firstBoot})
		return
	}

	characterID, err := s.characterID(r.Context())
	if err != nil {
		slog.Error("loading portfolio character", "err", err)
		http.Error(w, "loading portfolio", http.StatusInternalServerError)
		return
	}
	report, err := ledger.ComputePnL(r.Context(), s.db, characterID)
	if err != nil {
		slog.Error("loading portfolio", "err", err)
		http.Error(w, "loading portfolio", http.StatusInternalServerError)
		return
	}

	s.renderPortfolio(w, buildPortfolioData(report))
}

// characterID reads the tracked character from the single esi_token row.
// authenticationFailed has already confirmed a token exists on this path.
func (s *Server) characterID(ctx context.Context) (int, error) {
	var id int
	if err := s.db.QueryRowContext(ctx, `SELECT character_id FROM esi_token LIMIT 1`).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errNoStoredToken
		}
		return 0, fmt.Errorf("loading tracked character id: %w", err)
	}
	return id, nil
}

func (s *Server) renderPortfolio(w http.ResponseWriter, data portfolioData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "portfolio.html", data); err != nil {
		slog.Error("rendering portfolio", "err", err)
	}
}
