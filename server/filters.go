package server

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/mgoodness/eve-trader/ranking"
)

// Filter form defaults (v1.1). The minimum gross margin default is a static
// placeholder for this ticket; the next ticket derives it from the
// character's fee break-even.
const (
	defaultMinVolume = 20.0
	defaultMinMargin = 7.0
	defaultMaxMargin = 60.0
)

// maxSellDefault is deliberately "no default": the max-sell control starts
// blank, meaning no cap.
var maxSellDefault *float64

// filterForm is the parsed user-filter state for one request: the concrete
// bounds handed to the ranking query and the raw strings the form inputs
// render. present records which controls appeared in the request URL at
// all, so links can round-trip exactly the state the user asked for without
// embedding untouched defaults.
type filterForm struct {
	Bounds ranking.Filters

	MinVolume string
	MinMargin string
	MaxMargin string
	MaxSell   string

	present map[string]bool
}

// filteredParamNames are the four user-filter controls, in form order.
var filteredParamNames = []string{"minvol", "minmargin", "maxmargin", "maxsell"}

// parseFilterForm applies the stateless URL contract to q:
//
//   - an absent param uses the control's default;
//   - a present-but-empty param means "no bound";
//   - a present value is used when numeric and in range.
//
// A non-numeric or out-of-range value falls back to the control's default,
// and min margin greater than max margin falls both back to defaults. It
// never errors: a typo degrades to a default rather than an error page.
func parseFilterForm(q url.Values) filterForm {
	f := filterForm{present: map[string]bool{}}
	for _, name := range filteredParamNames {
		f.present[name] = q.Has(name)
	}

	f.Bounds.MinVolume, f.MinVolume = parseBound(q, "minvol", float64Ptr(defaultMinVolume), nonNegative)
	f.Bounds.MinMargin, f.MinMargin = parseBound(q, "minmargin", float64Ptr(defaultMinMargin), percentage)
	f.Bounds.MaxMargin, f.MaxMargin = parseBound(q, "maxmargin", float64Ptr(defaultMaxMargin), percentage)
	f.Bounds.MaxSell, f.MaxSell = parseBound(q, "maxsell", maxSellDefault, nonNegative)

	if f.Bounds.MinMargin != nil && f.Bounds.MaxMargin != nil && *f.Bounds.MinMargin > *f.Bounds.MaxMargin {
		f.Bounds.MinMargin, f.MinMargin = float64Ptr(defaultMinMargin), formatNumber(defaultMinMargin)
		f.Bounds.MaxMargin, f.MaxMargin = float64Ptr(defaultMaxMargin), formatNumber(defaultMaxMargin)
	}
	return f
}

// values returns the query params that preserve this filter state, with the
// active sort key added. Untouched controls (absent in the request) are
// omitted so defaults are not embedded in generated links; blanked controls
// round-trip as present-but-empty so "no bound" stays "no bound".
func (f filterForm) values(sortKey string) url.Values {
	q := url.Values{}
	for _, name := range filteredParamNames {
		if f.present[name] {
			q.Set(name, f.raw(name))
		}
	}
	if sortKey != "" {
		q.Set("sort", sortKey)
	}
	return q
}

func (f filterForm) raw(name string) string {
	switch name {
	case "minvol":
		return f.MinVolume
	case "minmargin":
		return f.MinMargin
	case "maxmargin":
		return f.MaxMargin
	case "maxsell":
		return f.MaxSell
	}
	return ""
}

// parseBound reads one control. def is the value used when the param is
// absent or invalid; a nil def means the control's default is "no bound".
// It returns the concrete bound (nil = no bound) and the raw string the
// input should render.
func parseBound(q url.Values, name string, def *float64, valid func(float64) bool) (*float64, string) {
	if !q.Has(name) {
		return def, formatDefault(def)
	}
	raw := strings.TrimSpace(q.Get(name))
	if raw == "" {
		return nil, ""
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || !valid(v) {
		return def, formatDefault(def)
	}
	return float64Ptr(v), raw
}

func float64Ptr(v float64) *float64 { return &v }

func formatDefault(def *float64) string {
	if def == nil {
		return ""
	}
	return formatNumber(*def)
}

func formatNumber(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func nonNegative(v float64) bool { return v >= 0 }

func percentage(v float64) bool { return v >= 0 && v <= 100 }

// activeSummary names the controls that currently impose a bound, for the
// empty state. A blank (no-bound) control contributes nothing, so copy can
// never hardcode a threshold that the user has since cleared.
func (f filterForm) activeSummary() string {
	var parts []string
	if f.MinVolume != "" {
		parts = append(parts, "minimum daily volume "+f.MinVolume)
	}
	if f.MinMargin != "" {
		parts = append(parts, "minimum gross margin "+f.MinMargin+"%")
	}
	if f.MaxMargin != "" {
		parts = append(parts, "maximum gross margin "+f.MaxMargin+"%")
	}
	if f.MaxSell != "" {
		parts = append(parts, "maximum sell price "+f.MaxSell+" ISK")
	}
	return strings.Join(parts, "; ")
}
