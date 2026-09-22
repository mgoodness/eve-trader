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
	"net/url"
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
	mux.HandleFunc("GET /portfolio", s.handlePortfolio)
	mux.HandleFunc("GET /auth/login", s.handleAuthLogin)
	mux.HandleFunc("GET /auth/callback", s.handleAuthCallback)
	s.mux = mux

	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// realismRules are the always-on filters the sidebar names, built from
// ranking's thresholds so the displayed copy cannot drift from the code.
var realismRules = []string{
	fmt.Sprintf("At least %d days of recent trade history", ranking.MinTradeDays),
	fmt.Sprintf("No manipulated history (fewer than %d trade-days with a greater-than-%gx high/low swing)", ranking.ManipulatedTradeDays, ranking.ManipulatedSwingRatio),
	fmt.Sprintf("No single-order spreads (one or fewer orders within %g%% of best on either side)", ranking.NearBookBand*100),
	"Complete price history (items not yet re-fetched are hidden)",
}

// pageData is the data handed to the "page.html" template and to the
// htmx-sorted "layout" partial. Shown/Total and the two hidden counts feed
// the sidebar and empty-state copy; FilterForm and Columns carry the
// stateless URL state.
type pageData struct {
	Opportunities   []ranking.Opportunity
	HiddenByRealism int
	HiddenByFilters int
	Total           int
	Shown           int
	RealismRules    []string

	FilterForm          filterForm
	FilterControls      []controlView
	Sort                string
	Columns             []sortColumn
	ActiveFilterSummary string

	Reauth    bool
	FirstBoot bool
}

// sortColumn is one sortable table header. Href is the htmx partial request
// (which carries the current filters); Push is the canonical full-page URL
// the browser address bar keeps so a refresh or bookmark reproduces the
// exact filtered, sorted view.
type sortColumn struct {
	Key    string
	Label  string
	Href   string
	Push   string
	Active bool
}

// sortableColumns are the table columns the user can sort by, in display
// order. "margin" sorts by the displayed net margin.
var sortableColumns = []struct {
	key   string
	label string
}{
	{"buy", "Buy"},
	{"sell", "Sell"},
	{"margin", "Net margin"},
	{"iskunit", "ISK/unit"},
	{"volday", "Vol/day"},
	{"iskday", "ISK/day"},
}

// parseSort resolves the ?sort param to a known column key. An absent or
// unrecognised value falls back to the default rank, ISK/day descending.
func parseSort(q url.Values) string {
	for _, c := range sortableColumns {
		if q.Get("sort") == c.key {
			return c.key
		}
	}
	return "iskday"
}

// buildColumns renders each sortable header's links with the current filter
// state preserved, so sorting never drops the filters.
func buildColumns(form filterForm, sortKey string) []sortColumn {
	cols := make([]sortColumn, len(sortableColumns))
	for i, c := range sortableColumns {
		encoded := form.values(c.key).Encode()
		cols[i] = sortColumn{
			Key:    c.key,
			Label:  c.label,
			Href:   "/opportunities?" + encoded,
			Push:   "/?" + encoded,
			Active: sortKey == c.key,
		}
	}
	return cols
}

// buildPageData is the one place filters, sort, and the ranking pass compose:
// it parses the URL contract, loads the realism survivors, applies the user
// filters, sorts the survivors, and assembles the view model both the index
// page and the htmx partial render.
func (s *Server) buildPageData(r *http.Request) (pageData, error) {
	query := r.URL.Query()

	// The minimum-margin default is the character's fee break-even, so the
	// default list is fee-positive at any skill level (and, after the
	// standings fast-follow, at any standing). Load the skills-and-standings
	// rates once per request for it; ranking.Load re-reads the same rows.
	rates, err := ranking.LoadFeeRates(r.Context(), s.db)
	if err != nil {
		return pageData{}, err
	}
	form := parseFilterForm(query, ranking.BreakEvenGrossMargin(rates))
	sortKey := parseSort(query)

	result, err := ranking.Load(r.Context(), s.db, form.Bounds)
	if err != nil {
		return pageData{}, err
	}
	ranking.Sort(result.Opportunities, sortKey)

	return pageData{
		Opportunities:       result.Opportunities,
		HiddenByRealism:     result.HiddenByRealism,
		HiddenByFilters:     result.HiddenByFilters,
		Total:               result.Total(),
		Shown:               len(result.Opportunities),
		RealismRules:        realismRules,
		FilterForm:          form,
		FilterControls:      form.controlsView(),
		Sort:                sortKey,
		Columns:             buildColumns(form, sortKey),
		ActiveFilterSummary: form.activeSummary(),
	}, nil
}

// handleIndex renders the opportunity table: one row per item that clears
// the always-on realism filters and the user's filters, ranked ISK/day
// descending by default (see docs/spec/v1.md §4/§7). Market data, item
// names, and character skills are read directly from SQLite -- no
// ESIGateway call is made here (that arrives with live polling in a later
// ticket).
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if failed, firstBoot := s.authenticationFailed(r.Context()); failed {
		s.renderIndex(w, pageData{Reauth: true, FirstBoot: firstBoot})
		return
	}

	data, err := s.buildPageData(r)
	if err != nil {
		slog.Error("loading opportunities", "err", err)
		http.Error(w, "loading opportunities", http.StatusInternalServerError)
		return
	}

	s.renderIndex(w, data)
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

// latchInsufficientScope latches the same re-authentication state as a
// refresh failure, for the distinct case where the stored refresh token
// still refreshes successfully but was granted under a narrower OAuth
// scope than an authenticated call now needs (e.g. a v1 token after the
// v2 scope expansion, docs/spec/v2.md §6: "the tool requires one
// re-consent"). The "Re-authenticate with EVE" banner's link re-requests
// ssoScope's current scope set, so following it once re-consents and
// replaces the under-scoped token.
func (s *Server) latchInsufficientScope(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reauthRequired = true
	s.authChecked = true
	slog.Error("authenticated ESI work stopped: token lacks a newly required scope, re-authentication required", "err", err)
}

// latchIfInsufficientScope recognizes ESI's 403 for a token that refreshes
// fine but was never granted the scope a call needs -- the state a pre-v2
// refresh token is in after the ssoScope expansion (docs/spec/v2.md §6).
// It latches the same "Re-authenticate with EVE" banner a refresh failure
// does, whose link requests the current (superset) ssoScope, so following
// it once re-consents and replaces the under-scoped token.
func (s *Server) latchIfInsufficientScope(err error) {
	var httpErr *esi.HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusForbidden {
		s.latchInsufficientScope(err)
	}
}

func (s *Server) resetAuthentication() {
	s.mu.Lock()
	s.authChecked, s.reauthRequired, s.reauthFirstBoot = false, false, false
	s.mu.Unlock()
}

func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }

// handleOpportunities serves the htmx sort partial: it re-runs the same
// filter+ranking pass, re-sorts by the requested column (?sort=buy|sell|
// margin|iskunit|volday|iskday), and returns the re-rendered layout so the
// sidebar's hidden sort and the summary stay in step. htmx pushes the
// canonical full-page URL so the address bar stays bookmarkable.
func (s *Server) handleOpportunities(w http.ResponseWriter, r *http.Request) {
	data, err := s.buildPageData(r)
	if err != nil {
		slog.Error("loading opportunities", "err", err)
		http.Error(w, "loading opportunities", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		slog.Error("rendering opportunities partial", "err", err)
	}
}
