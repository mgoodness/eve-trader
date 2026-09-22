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
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mgoodness/eve-trader/ledger"
)

// targetNetMarginParam is the Portfolio's view-level target net margin
// control: a percentage in the URL, defaulting to 0% net, so Target equals
// Break-even (docs/spec/v2.md §4.7).
const targetNetMarginParam = "targetmargin"

// parseTargetNetMargin reads the target net margin control and returns it
// as a fraction plus the raw percentage string the input renders. An
// absent, blank, non-numeric, or out-of-range value falls back to the 0%
// default rather than erroring.
func parseTargetNetMargin(q url.Values) (margin float64, raw string) {
	if !q.Has(targetNetMarginParam) {
		return 0, "0"
	}
	raw = strings.TrimSpace(q.Get(targetNetMarginParam))
	if raw == "" {
		return 0, "0"
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v >= 100 {
		return 0, "0"
	}
	return v / 100, raw
}

// portfolioGroup is one group of the Portfolio table. A positions group
// carries decision rows; the orders group carries raise-only re-list gains,
// which are one row per order rather than per position. Count is the group's
// full size; the slice is empty when the group has none, so the template can
// render the heading and an empty state.
type portfolioGroup struct {
	Title       string
	Orders      bool
	Count       int
	Positions   []ledger.Position
	RelistGains []ledger.RelistGain
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
	Groups  []portfolioGroup
	Summary portfolioSummary
	// TargetNetMargin is the raw percentage the control renders, so the
	// input round-trips what the user typed (default "0").
	TargetNetMargin string
	Reauth          bool
	FirstBoot       bool
}

// portfolioGroupOrder is the decision order the table renders in
// (docs/spec/v2.md §7), including the raise-only "Orders worth moving"
// group between Below target and Transfers.
var portfolioGroupOrder = []struct {
	title  string
	status ledger.Status
	orders bool
}{
	{"At target now", ledger.StatusAtTarget, false},
	{"Below target", ledger.StatusBelowTarget, false},
	{"Orders worth moving", "", true},
	{"Transfers", ledger.StatusTransfer, false},
	{"No market", ledger.StatusNoMarket, false},
	{"Closed / realized", ledger.StatusClosed, false},
}

// buildPortfolioData groups the report's positions and re-list gains and
// copies the summary figures. Empty groups still render, with a count of
// zero. targetRaw is the raw target-margin percentage the control renders.
func buildPortfolioData(report ledger.Report, targetRaw string) portfolioData {
	byStatus := make(map[ledger.Status][]ledger.Position, len(portfolioGroupOrder))
	for _, p := range report.Positions {
		byStatus[p.Status] = append(byStatus[p.Status], p)
	}

	groups := make([]portfolioGroup, 0, len(portfolioGroupOrder))
	for _, g := range portfolioGroupOrder {
		if g.orders {
			groups = append(groups, portfolioGroup{
				Title:       g.title,
				Orders:      true,
				Count:       len(report.RelistGains),
				RelistGains: report.RelistGains,
			})
			continue
		}
		positions := byStatus[g.status]
		groups = append(groups, portfolioGroup{
			Title:     g.title,
			Count:     len(positions),
			Positions: positions,
		})
	}

	return portfolioData{
		Groups:          groups,
		TargetNetMargin: targetRaw,
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
	margin, targetRaw := parseTargetNetMargin(r.URL.Query())
	report, err := ledger.ComputePnLForTarget(r.Context(), s.db, characterID, margin)
	if err != nil {
		slog.Error("loading portfolio", "err", err)
		http.Error(w, "loading portfolio", http.StatusInternalServerError)
		return
	}

	s.renderPortfolio(w, buildPortfolioData(report, targetRaw))
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
