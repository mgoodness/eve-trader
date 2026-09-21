package server

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"github.com/mgoodness/eve-trader/ranking"
)

// Filter form defaults (v1.1). The minimum-volume and maximum-margin
// defaults are constants; the minimum-margin default is derived per request
// from the character's skills (see minMarginParam and
// ranking.BreakEvenGrossMargin), so it has no constant here.
const (
	defaultMinVolume = 20.0
	defaultMaxMargin = 60.0
)

// minMarginParam names the one control whose default is not a constant: it
// is the character's fee break-even, computed per request.
const minMarginParam = "minmargin"

// filterSpec describes one user-filter control: its URL param, sidebar
// label, hover help, default bound (nil = no default bound; the skills-derived
// minimum-margin default is injected per request in filterSpecsWithDefaults),
// validity range, and the bits the summary copy needs. Keeping the four
// controls in one table means the parser, the rendered form, and the
// round-trip links cannot disagree about them.
type filterSpec struct {
	name         string
	label        string
	summaryLabel string
	unit         string
	placeholder  string
	help         string
	def          *float64
	valid        func(float64) bool
}

var filterSpecs = []filterSpec{
	{
		name:         "minvol",
		label:        "Minimum daily volume",
		summaryLabel: "minimum daily volume",
		help:         fmt.Sprintf("Hide items whose average daily Heimatar volume is below this. Default %s keeps dead markets out; clear it for no lower bound.", formatNumber(defaultMinVolume)),
		def:          float64Ptr(defaultMinVolume),
		valid:        nonNegative,
	},
	{
		name:         minMarginParam,
		label:        "Minimum gross margin %",
		summaryLabel: "minimum gross margin",
		unit:         "%",
		help:         "Hide items whose gross margin is below this. Gross margin is (sell - buy) / sell. The default is your character's fee break-even, so the list starts fee-positive; clear it for no lower bound.",
		def:          nil, // skills-derived; filled in by filterSpecsWithDefaults
		valid:        percentage,
	},
	{
		name:         "maxmargin",
		label:        "Maximum gross margin %",
		summaryLabel: "maximum gross margin",
		unit:         "%",
		help:         fmt.Sprintf("Hide items whose gross margin is above this - usually a stale or manipulated price. Default %s%%; clear it for no upper bound.", formatNumber(defaultMaxMargin)),
		def:          float64Ptr(defaultMaxMargin),
		valid:        percentage,
	},
	{
		name:         "maxsell",
		label:        "Maximum sell price",
		summaryLabel: "maximum sell price",
		unit:         " ISK",
		placeholder:  "no cap",
		help:         "Hide items whose sell price is above this, to cap what you must spend to acquire them. Blank means no cap.",
		def:          nil,
		valid:        nonNegative,
	},
}

// controlView is one control as the sidebar renders it: the label, the
// current value, and the shared hover help.
type controlView struct {
	Name        string
	Label       string
	Help        string
	Value       string
	Placeholder string
}

// filterControl is one parsed control: the concrete bound handed to ranking
// (nil = no bound), the raw string the input renders, and whether the param
// appeared in the request at all. Its spec carries the effective default for
// this request (minmargin's is skills-derived).
type filterControl struct {
	spec    filterSpec
	bound   *float64
	raw     string
	present bool
}

// filterForm is the parsed user-filter state for one request: the concrete
// bounds handed to the ranking query and the per-control raw values the form
// renders. present records which controls appeared in the request URL at
// all, so generated links round-trip exactly the state the user asked for
// without embedding untouched defaults. specs is the control table with the
// skills-derived minimum-margin default already filled in, so the table
// stays the single source of truth for every control.
type filterForm struct {
	Bounds   ranking.Filters
	specs    []filterSpec
	controls map[string]*filterControl
}

// filterSpecsWithDefaults copies the control table with the per-request
// minimum-margin default substituted, so callers can keep reading defaults
// from the spec rather than carrying a parallel copy.
func filterSpecsWithDefaults(minMarginDefault float64) []filterSpec {
	specs := make([]filterSpec, len(filterSpecs))
	copy(specs, filterSpecs)
	for i := range specs {
		if specs[i].name == minMarginParam {
			specs[i].def = float64Ptr(minMarginDefault)
		}
	}
	return specs
}

// parseFilterForm applies the stateless URL contract to q. minMarginDefault
// is the skills-derived default for the minimum-margin control; the other
// controls carry their own constant defaults in filterSpecs.
//
//   - an absent param uses the control's default;
//   - a present-but-empty param means "no bound";
//   - a present value is used when numeric and in range.
//
// A non-numeric or out-of-range value falls back to the control's default,
// and min margin greater than max margin falls both back to defaults. It
// never errors: a typo degrades to a default rather than an error page.
func parseFilterForm(q url.Values, minMarginDefault float64) filterForm {
	specs := filterSpecsWithDefaults(minMarginDefault)
	f := filterForm{
		specs:    specs,
		controls: make(map[string]*filterControl, len(specs)),
	}
	for _, spec := range specs {
		bound, raw := parseBound(q, spec.name, spec.def, spec.valid)
		f.controls[spec.name] = &filterControl{
			spec:    spec,
			bound:   bound,
			raw:     raw,
			present: q.Has(spec.name),
		}
	}

	min, max := f.controls["minmargin"], f.controls["maxmargin"]
	if min.bound != nil && max.bound != nil && *min.bound > *max.bound {
		min.reset()
		max.reset()
	}

	f.Bounds = ranking.Filters{
		MinVolume: f.controls["minvol"].bound,
		MinMargin: min.bound,
		MaxMargin: max.bound,
		MaxSell:   f.controls["maxsell"].bound,
	}
	return f
}

// values returns the query params that preserve this filter state, with the
// active sort key added. Untouched controls (absent in the request) are
// omitted so defaults are not embedded in generated links; blanked controls
// round-trip as present-but-empty so "no bound" stays "no bound".
func (f filterForm) values(sortKey string) url.Values {
	q := url.Values{}
	for _, spec := range f.specs {
		if c := f.controls[spec.name]; c.present {
			q.Set(spec.name, c.raw)
		}
	}
	if sortKey != "" {
		q.Set("sort", sortKey)
	}
	return q
}

// controlsView returns the four controls in form order.
func (f filterForm) controlsView() []controlView {
	views := make([]controlView, len(f.specs))
	for i, spec := range f.specs {
		views[i] = controlView{
			Name:        spec.name,
			Label:       spec.label,
			Help:        spec.help,
			Value:       f.controls[spec.name].raw,
			Placeholder: spec.placeholder,
		}
	}
	return views
}

// activeSummary names the controls that currently impose a bound, for the
// summary line and empty state. A blank (no-bound) control contributes
// nothing, so copy can never hardcode a threshold the user has cleared.
func (f filterForm) activeSummary() string {
	var parts []string
	for _, spec := range f.specs {
		if c := f.controls[spec.name]; c.raw != "" {
			parts = append(parts, spec.summaryLabel+" "+c.raw+spec.unit)
		}
	}
	return strings.Join(parts, "; ")
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
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || !valid(v) {
		return def, formatDefault(def)
	}
	return float64Ptr(v), raw
}

// reset restores a control to its default (or no bound when it has none).
func (c *filterControl) reset() {
	c.bound = c.spec.def
	c.raw = formatDefault(c.spec.def)
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
