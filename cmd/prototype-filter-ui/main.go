// Command prototype-filter-ui is a THROWAWAY prototype answering:
// "What should the v1.1 filter form look like, how should excluded items
// read to the user, and how does filter state coexist with the sortable
// column headers?"
// (wayfinder ticket: Prototype: filter-form UI + excluded-item presentation,
// eve-trader#62)
//
// Three structurally different variants of the same page, switchable via
// ?variant=A|B|C. Run with: go run ./cmd/prototype-filter-ui
// then open http://localhost:8090/?variant=A
//
// Filtering and sorting are real (server-side over fake data), so the
// stateless query contract decided in eve-trader#60 is exercised:
//   - absent param  -> that control's default (minvol=20, minmargin=7, maxmargin=60)
//   - present-empty -> no bound (e.g. ...&minvol=)
//   - sort + filters compose in one query string (hidden `sort` input, and
//     column headers `hx-include` the filter form)
//
// This code is not meant to be promoted as-is — see the "prototype" skill for
// the capture/cleanup convention. The winning variant gets rewritten properly
// against the real ranking query and schema.
package main

import (
	"bytes"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// ---- prototype data model ----

// Item is one catalog row with the realism status the always-on filters
// (eve-trader#59) would have computed for it. RealismFail is "" for items
// that pass realism and a reason key otherwise.
type Item struct {
	TypeID     int
	Name       string
	Buy        float64 // P_b
	Sell       float64 // P_s
	VolPerDay  float64 // V_d (Heimatar-region-wide)
	RealismFail string // "", "history", "manipulated", "thinbook"
	TradeDays  int
	Swing      float64 // max(highest)/min(lowest)
}

// Row is the displayed shape: an Item plus its computed figures.
type Row struct {
	TypeID int
	Name   string
	Buy    float64
	Sell   float64
	Gross  float64 // gross margin %   M  = (P_s-P_b)/P_s*100
	Net    float64 // net margin %      π/P_s*100
	Profit float64 // π, net profit per unit after fees
	Vol    float64 // V_d
	ISKDay float64 // EDP = π × V_d × CaptureRate
}

// CaptureRate is the fixed ~20% capture assumption (eve-trader#61).
const CaptureRate = 0.20

// Displayed skill levels, maxed like the prior-art screenshots.
const (
	brokerRelationsLevel = 5
	accountingLevel      = 5
)

func brokerFeeRate(lvl int) float64 { return 0.03 - 0.003*float64(lvl) }
func salesTaxRate(lvl int) float64  { return 0.075 * (1 - 0.11*float64(lvl)) }

func newRow(it Item) Row {
	rb := brokerFeeRate(brokerRelationsLevel)
	rt := salesTaxRate(accountingLevel)
	profit := it.Sell - it.Buy - it.Buy*rb - it.Sell*rb - it.Sell*rt
	return Row{
		TypeID: it.TypeID,
		Name:   it.Name,
		Buy:    it.Buy,
		Sell:   it.Sell,
		Gross:  (it.Sell - it.Buy) / it.Sell * 100,
		Net:    profit / it.Sell * 100,
		Profit: profit,
		Vol:    it.VolPerDay,
		ISKDay: profit * it.VolPerDay * CaptureRate,
	}
}

// catalog is the fake item set. Mix of realistic rows, rows the user filters
// exclude at defaults, and rows the always-on realism filters hide.
var catalog = []Item{
	{34, "Tritanium", 4.85, 5.35, 850000, "", 30, 0},
	{35, "Pyerite", 11.20, 12.80, 210000, "", 30, 0},
	{44992, "PLEX", 4850000, 5120000, 1.2, "", 28, 0},
	{22, "Mexallon", 62.50, 71.00, 34000, "", 30, 0},
	{36, "Isogen", 105.00, 118.50, 18500, "", 27, 0},
	{16273, "Nitrogen Isotopes", 92.10, 99.75, 32000, "", 26, 0},
	{2718, "Antimatter Charge L", 210.00, 245.00, 5200, "", 25, 0},
	{11399, "Morphite", 8850, 9640, 210, "", 24, 0},
	{9832, "Republic Fleet EM Shield Amplifier", 1450000, 1680000, 0.7, "", 21, 0},
	{40, "Nocxium", 895.00, 985.00, 1400, "", 22, 0},

	// Realism passes, but excluded by the user-filter defaults.
	{178, "Low-Volume Ore", 100, 110, 12, "", 30, 0},
	{179, "Thin Spread Ore", 100, 104, 5000, "", 30, 0},
	{180, "Scam-Priced Widget", 100, 300, 800, "", 29, 0},
	{181, "Capital Hull", 900000000, 1050000000, 0.5, "", 30, 0},

	// Hidden by the always-on realism filters (eve-trader#59).
	{182, "Dead Market Ore", 100, 110, 5000, "history", 3, 1.0},
	{183, "Manipulated Widget", 100, 112, 700, "manipulated", 10, 40.0},
	{184, "Single-Order Spread", 100, 130, 3000, "thinbook", 20, 2.0},
}

var realismRules = []string{
	"at least 7 days of recent trade history",
	"no manipulated history (fewer than 14 trade-days with a >20× high/low swing)",
	"no single-order spreads (≤1 order within 5% of best, either side)",
}

// ---- the stateless filter contract (eve-trader#60) ----

type bounds struct {
	minVol    *float64
	minMargin *float64
	maxMargin *float64
	maxSell   *float64
}

func f64(v float64) *float64 { return &v }

func parseControl(q url.Values, name string, def *float64, defStr string, valid func(float64) bool) (*float64, string) {
	if !q.Has(name) {
		return def, defStr
	}
	raw := strings.TrimSpace(q.Get(name))
	if raw == "" {
		return nil, "" // present-but-empty => no bound
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || !valid(v) {
		return def, defStr // invalid => fall back to default
	}
	return f64(v), raw
}

func parseBounds(q url.Values) (bounds, map[string]string) {
	minVol, minVolRaw := parseControl(q, "minvol", f64(20), "20", func(v float64) bool { return v >= 0 })
	minMargin, minMarginRaw := parseControl(q, "minmargin", f64(7), "7", func(v float64) bool { return v >= 0 && v <= 100 })
	maxMargin, maxMarginRaw := parseControl(q, "maxmargin", f64(60), "60", func(v float64) bool { return v >= 0 && v <= 100 })
	maxSell, maxSellRaw := parseControl(q, "maxsell", nil, "", func(v float64) bool { return v >= 0 })

	// min margin > max margin => both fall back to defaults.
	if minMargin != nil && maxMargin != nil && *minMargin > *maxMargin {
		minMargin, maxMargin = f64(7), f64(60)
		minMarginRaw, maxMarginRaw = "7", "60"
	}

	return bounds{minVol, minMargin, maxMargin, maxSell}, map[string]string{
		"minvol": minVolRaw, "minmargin": minMarginRaw,
		"maxmargin": maxMarginRaw, "maxsell": maxSellRaw,
	}
}

func passesRealism(it Item) bool { return it.RealismFail == "" }

func passesUser(r Row, b bounds) bool {
	if b.minVol != nil && r.Vol < *b.minVol {
		return false
	}
	if b.minMargin != nil && r.Gross < *b.minMargin {
		return false
	}
	if b.maxMargin != nil && r.Gross > *b.maxMargin {
		return false
	}
	if b.maxSell != nil && r.Sell > *b.maxSell {
		return false
	}
	return true
}

func sortRows(rows []Row, by string) {
	switch by {
	case "buy":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Buy < rows[j].Buy })
	case "sell":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Sell < rows[j].Sell })
	case "margin":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Net > rows[j].Net })
	case "iskunit":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Profit > rows[j].Profit })
	case "volday":
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Vol > rows[j].Vol })
	default:
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].ISKDay > rows[j].ISKDay })
	}
}

// ---- view model ----

type pageModel struct {
	Variant     string
	VariantName string
	PrevHref    string
	NextHref    string

	Query     url.Values // the current query (variant + filters + sort)
	Sort      string
	Inputs    map[string]string
	ActiveMin bool // whether each control currently bounds

	Rows          []Row
	Total         int
	RealismHidden int
	UserHidden    int
	RealismRules  []string

	// Chips are the active-filter chips for variant C: label + href that
	// clears that one control.
	Chips []chip
}

type chip struct {
	Label  string
	Href   string
	IsSet  bool
}

var variantOrder = []string{"A", "B", "C"}
var variantNames = map[string]string{
	"A": "Toolbar strip",
	"B": "Sidebar panel",
	"C": "Chips + disclosure band",
}

func neighbor(cur string, delta int) string {
	for i, v := range variantOrder {
		if v == cur {
			return variantOrder[(i+delta+len(variantOrder))%len(variantOrder)]
		}
	}
	return variantOrder[0]
}

func cloneValues(q url.Values) url.Values {
	out := url.Values{}
	for k, vs := range q {
		for _, v := range vs {
			out.Add(k, v)
		}
	}
	return out
}

func withVariant(q url.Values, variant string) string {
	c := cloneValues(q)
	c.Set("variant", variant)
	return "/?" + c.Encode()
}

func buildModel(q url.Values) pageModel {
	variant := q.Get("variant")
	if variant != "A" && variant != "B" && variant != "C" {
		variant = "A"
	}
	sortKey := q.Get("sort")
	b, inputs := parseBounds(q)

	// Split the catalog into realism survivors and realism-excluded.
	var survivors []Item
	realismHidden := 0
	for _, it := range catalog {
		if passesRealism(it) {
			survivors = append(survivors, it)
		} else {
			realismHidden++
		}
	}

	shown := make([]Row, 0, len(survivors))
	userHidden := 0
	for _, it := range survivors {
		r := newRow(it)
		if !passesUser(r, b) {
			userHidden++
			continue
		}
		shown = append(shown, r)
	}
	sortRows(shown, sortKey)

	chips := make([]chip, 0, 4)
	addChip := func(label, key string) {
		c := cloneValues(q)
		c.Set(key, "") // clearing a field submits empty => no bound
		chips = append(chips, chip{Label: label, Href: "/?" + c.Encode(), IsSet: inputs[key] != ""})
	}
	addChip("Min vol "+orDash(inputs["minvol"]), "minvol")
	addChip("Min margin "+orDash(inputs["minmargin"])+"%", "minmargin")
	addChip("Max margin "+orDash(inputs["maxmargin"])+"%", "maxmargin")
	addChip("Max sell "+orDash(inputs["maxsell"]), "maxsell")

	return pageModel{
		Variant:       variant,
		VariantName:   variantNames[variant],
		PrevHref:      withVariant(q, neighbor(variant, -1)),
		NextHref:      withVariant(q, neighbor(variant, 1)),
		Query:         cloneValues(q),
		Sort:          sortKey,
		Inputs:        inputs,
		Rows:          shown,
		Total:         len(catalog),
		RealismHidden: realismHidden,
		UserHidden:    userHidden,
		RealismRules:  realismRules,
		Chips:         chips,
	}
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// ---- template helpers ----

var tplFuncs = template.FuncMap{
	"fmtISK": func(v float64) string {
		neg := v < 0
		if neg {
			v = -v
		}
		s := formatThousands(int64(v))
		if neg {
			return "-" + s
		}
		return s
	},
	"fmtPct": func(v float64) string { return trimFloat(v) + "%" },
	"fmtNum": func(v float64) string { return formatThousands(int64(v)) },
	"add":    func(a, b int) int { return a + b },
	"help":   helpTip,
}

// helpTip renders a small hover tooltip icon. Prototype-only.
func helpTip(text string) template.HTML {
	return template.HTML(`<span class="group relative inline-block align-middle ml-1 cursor-help text-slate-500">&#9432;<span class="pointer-events-none absolute z-20 hidden group-hover:block w-56 bottom-full left-1/2 -translate-x-1/2 mb-1 rounded-md bg-slate-700 text-slate-100 text-xs normal-case leading-snug p-2 shadow-lg">` + template.HTMLEscapeString(text) + `</span></span>`)
}

func mustExec(tpl *template.Template, data any) template.HTML {
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		log.Printf("template error in %s: %v", tpl.Name(), err)
		return template.HTML("<div class='text-red-400'>template error — see server log</div>")
	}
	return template.HTML(buf.String())
}

// ---- shared: sortable header cell, `hx-include`-ing the filter form ----

// headerCell renders a clickable column header. hx-include pulls the current
// filter form values into the request, so sorting preserves the filters.
func headerCell(label, key, currentSort, indicator string) template.HTML {
	cls := "py-2 pr-4 cursor-pointer hover:text-slate-200"
	if currentSort == key || (currentSort == "" && key == "iskday") {
		cls += " text-slate-100"
	} else {
		cls += " text-slate-400"
	}
	h := `<th class="` + cls + `" hx-get="/?sort=` + key + `" hx-include="#filterform" hx-target="#results" hx-swap="innerHTML" hx-push-url="true">` +
		template.HTMLEscapeString(label + indicator) + `</th>`
	return template.HTML(h)
}

// ---- footnote + summary copy ----

const footnoteHTML = `Profit figures are <strong>after broker fee and sales tax</strong> and assume you
	capture <strong>~20% of daily volume</strong>. Volume is <strong>Heimatar-region-wide</strong>, not
	Rens-station-specific, so treat Vol/day and ISK/day as an approximation. Items with thin or
	manipulated history are hidden automatically.`

func summaryText(m pageModel) template.HTML {
	return template.HTML("Showing <strong>" + itoa(len(m.Rows)) + "</strong> of " + itoa(m.Total) +
		" items &middot; <strong>" + itoa(m.RealismHidden) + "</strong> hidden by realism filters" +
		" &middot; <strong>" + itoa(m.UserHidden) + "</strong> outside your filters.")
}

// ---- variant A: toolbar strip ----

var tableRowsTpl = template.Must(template.New("tableRows").Funcs(tplFuncs).Parse(`
{{if not .}}<tr><td colspan="7" class="py-6 text-center text-slate-500">No opportunities clear the current filters. Widen or clear a filter above.</td></tr>{{end}}
{{range .}}
<tr class="border-b border-slate-800 hover:bg-slate-900">
  <td class="py-2 pr-4">{{.Name}}</td>
  <td class="py-2 pr-4 text-slate-300">{{fmtISK .Buy}}</td>
  <td class="py-2 pr-4 text-slate-300">{{fmtISK .Sell}}</td>
  <td class="py-2 pr-4">{{fmtPct .Net}}</td>
  <td class="py-2 pr-4">{{fmtISK .Profit}}</td>
  <td class="py-2 pr-4 text-slate-400">{{fmtNum .Vol}}</td>
  <td class="py-2 pr-4 font-semibold text-emerald-400">{{fmtISK .ISKDay}}</td>
</tr>
{{end}}
`))

var variantATpl = template.Must(template.New("a").Funcs(tplFuncs).Parse(`
<div id="results">
  <form id="filterform" hx-get="/" hx-target="#results" hx-swap="innerHTML" hx-push-url="true"
        class="flex flex-wrap items-end gap-3 rounded-lg bg-slate-900 border border-slate-800 px-4 py-3">
    <input type="hidden" name="sort" value="{{.Sort}}">
    <label class="text-xs text-slate-400">Min vol/day{{help "Hide items whose average daily Heimatar volume is below this. Default 20 keeps dead markets out; clear it to show everything."}}
      <input name="minvol" value="{{index .Inputs "minvol"}}" size="7"
        class="block mt-1 bg-slate-950 border border-slate-700 rounded px-2 py-1 text-sm text-slate-100"></label>
    <label class="text-xs text-slate-400">Min gross margin %{{help "Hide items whose gross spread is below this. Gross margin = (sell - buy) / sell; the default 7% clears fees at max skills. Clear for no floor."}}
      <input name="minmargin" value="{{index .Inputs "minmargin"}}" size="5"
        class="block mt-1 bg-slate-950 border border-slate-700 rounded px-2 py-1 text-sm text-slate-100"></label>
    <label class="text-xs text-slate-400">Max gross margin %{{help "Hide items whose gross spread is above this - usually a stale or manipulated price. Default 60%; clear it to show those outliers."}}
      <input name="maxmargin" value="{{index .Inputs "maxmargin"}}" size="5"
        class="block mt-1 bg-slate-950 border border-slate-700 rounded px-2 py-1 text-sm text-slate-100"></label>
    <label class="text-xs text-slate-400">Max sell price{{help "Budget cap on the item's sell price. Leave blank for no cap."}}
      <input name="maxsell" value="{{index .Inputs "maxsell"}}" size="12" placeholder="no cap"
        class="block mt-1 bg-slate-950 border border-slate-700 rounded px-2 py-1 text-sm text-slate-100"></label>
    <button type="submit" class="bg-emerald-600 hover:bg-emerald-500 text-slate-950 text-sm font-medium px-3 py-1.5 rounded">Apply</button>
    <a href="/?variant={{.Variant}}" class="text-sm text-slate-400 hover:text-slate-200 px-2 py-1.5">Reset</a>
    <span class="ml-auto text-xs text-slate-400">{{.Summary}}</span>
  </form>

  <p class="text-xs text-slate-500 mt-3">{{.Footnote}}</p>

  <table class="w-full text-sm border-collapse mt-3">
    <thead>
      <tr class="text-left border-b border-slate-700">
        <th class="py-2 pr-4 text-slate-400">Item</th>
        {{.HeaderBuy}}{{.HeaderSell}}{{.HeaderMargin}}{{.HeaderUnit}}{{.HeaderVol}}{{.HeaderDay}}
      </tr>
    </thead>
    <tbody>{{.Rows}}</tbody>
  </table>
</div>
`))

type variantAData struct {
	Variant, Sort string
	Summary       template.HTML
	Inputs                 map[string]string
	Footnote               template.HTML
	Rows                   template.HTML
	HeaderBuy, HeaderSell, HeaderMargin, HeaderUnit, HeaderVol, HeaderDay template.HTML
}

func renderVariantA(m pageModel) template.HTML {
	rows := mustExec(tableRowsTpl, m.Rows)
	d := variantAData{
		Variant: m.Variant, Sort: m.Sort, Summary: summaryText(m), Inputs: m.Inputs,
		Footnote: template.HTML(footnoteHTML), Rows: rows,
		HeaderBuy:    headerCell("Buy", "buy", m.Sort, ""),
		HeaderSell:   headerCell("Sell", "sell", m.Sort, ""),
		HeaderMargin: headerCell("Net margin", "margin", m.Sort, ""),
		HeaderUnit:   headerCell("ISK/unit", "iskunit", m.Sort, ""),
		HeaderVol:    headerCell("Vol/day", "volday", m.Sort, ""),
		HeaderDay:    headerCell("ISK/day", "iskday", m.Sort, " ↓"),
	}
	return mustExec(variantATpl, d)
}

// ---- variant B: sidebar panel ----

var variantBTpl = template.Must(template.New("b").Funcs(tplFuncs).Parse(`
<div class="flex gap-6">
  <aside class="w-72 shrink-0">
    <form id="filterform" hx-get="/" hx-target="#results" hx-swap="innerHTML" hx-push-url="true"
          class="rounded-xl bg-slate-900 border border-slate-800 p-4 space-y-4">
      <input type="hidden" name="sort" value="{{.Sort}}">
      <h2 class="text-sm font-semibold text-slate-200">Your filters</h2>
      <label class="block text-xs text-slate-400">Min daily volume{{help "Hide items whose average daily Heimatar volume is below this. Default 20 keeps dead markets out; clear it to show everything."}}
        <input name="minvol" value="{{index .Inputs "minvol"}}"
          class="mt-1 w-full bg-slate-950 border border-slate-700 rounded px-2 py-1.5 text-sm text-slate-100"></label>
      <label class="block text-xs text-slate-400">Min gross margin %{{help "Hide items whose gross spread is below this. Gross margin = (sell - buy) / sell; the default 7% clears fees at max skills. Clear for no floor."}}
        <input name="minmargin" value="{{index .Inputs "minmargin"}}"
          class="mt-1 w-full bg-slate-950 border border-slate-700 rounded px-2 py-1.5 text-sm text-slate-100"></label>
      <label class="block text-xs text-slate-400">Max gross margin %{{help "Hide items whose gross spread is above this - usually a stale or manipulated price. Default 60%; clear it to show those outliers."}}
        <input name="maxmargin" value="{{index .Inputs "maxmargin"}}"
          class="mt-1 w-full bg-slate-950 border border-slate-700 rounded px-2 py-1.5 text-sm text-slate-100"></label>
      <label class="block text-xs text-slate-400">Max sell price{{help "Budget cap on the item's sell price. Leave blank for no cap."}}
        <input name="maxsell" value="{{index .Inputs "maxsell"}}" placeholder="no cap"
          class="mt-1 w-full bg-slate-950 border border-slate-700 rounded px-2 py-1.5 text-sm text-slate-100"></label>
      <div class="flex gap-2 pt-1">
        <button type="submit" class="flex-1 bg-emerald-600 hover:bg-emerald-500 text-slate-950 text-sm font-medium px-3 py-1.5 rounded">Apply</button>
        <a href="/?variant={{.Variant}}" class="text-sm text-slate-400 hover:text-slate-200 px-3 py-1.5">Reset</a>
      </div>
    </form>

    <div class="rounded-xl bg-slate-900/60 border border-slate-800 p-4 mt-4">
      <h3 class="text-xs font-semibold uppercase tracking-wide text-slate-400">Always-on realism filters</h3>
      <p class="text-xs text-slate-500 mt-1">Cannot be turned off. <strong class="text-slate-300">{{.RealismHidden}} items hidden</strong>.</p>
      <ul class="mt-2 space-y-1 text-xs text-slate-400 list-disc list-inside">
        {{range .RealismRules}}<li>{{.}}</li>{{end}}
      </ul>
    </div>
  </aside>

  <main id="results" class="flex-1 min-w-0">
    <p class="text-xs text-slate-400 mb-3">{{.Summary}}</p>
    <table class="w-full text-sm border-collapse">
      <thead>
        <tr class="text-left border-b border-slate-700">
          <th class="py-2 pr-4 text-slate-400">Item</th>
          {{.HeaderBuy}}{{.HeaderSell}}{{.HeaderMargin}}{{.HeaderUnit}}{{.HeaderVol}}{{.HeaderDay}}
        </tr>
      </thead>
      <tbody>{{.Rows}}</tbody>
    </table>
    <p class="text-xs text-slate-500 mt-4">{{.Footnote}}</p>
  </main>
</div>
`))

type variantBData struct {
	Variant, Sort string
	Summary       template.HTML
	Inputs                 map[string]string
	RealismRules           []string
	RealismHidden          int
	Footnote               template.HTML
	Rows                   template.HTML
	HeaderBuy, HeaderSell, HeaderMargin, HeaderUnit, HeaderVol, HeaderDay template.HTML
}

func renderVariantB(m pageModel) template.HTML {
	rows := mustExec(tableRowsTpl, m.Rows)
	d := variantBData{
		Variant: m.Variant, Sort: m.Sort, Summary: summaryText(m), Inputs: m.Inputs,
		RealismRules: m.RealismRules, RealismHidden: m.RealismHidden,
		Footnote: template.HTML(footnoteHTML), Rows: rows,
		HeaderBuy:    headerCell("Buy", "buy", m.Sort, ""),
		HeaderSell:   headerCell("Sell", "sell", m.Sort, ""),
		HeaderMargin: headerCell("Net margin", "margin", m.Sort, ""),
		HeaderUnit:   headerCell("ISK/unit", "iskunit", m.Sort, ""),
		HeaderVol:    headerCell("Vol/day", "volday", m.Sort, ""),
		HeaderDay:    headerCell("ISK/day", "iskday", m.Sort, " ↓"),
	}
	return mustExec(variantBTpl, d)
}

// ---- variant C: chips + disclosure band ----

var variantCTpl = template.Must(template.New("c").Funcs(tplFuncs).Parse(`
<div id="results">
  <div class="flex items-center flex-wrap gap-2">
    {{range .Chips}}
      <span class="inline-flex items-center gap-2 text-xs rounded-full px-3 py-1 border {{if .IsSet}}border-slate-600 bg-slate-800 text-slate-200{{else}}border-slate-800 bg-slate-900 text-slate-500{{end}}">
        {{.Label}}
        <a href="{{.Href}}" class="hover:text-red-400" title="remove">&times;</a>
      </span>
    {{end}}
    <a href="/?variant={{.Variant}}" class="text-xs text-slate-400 hover:text-slate-200 underline ml-1">reset all</a>
    <span class="ml-auto text-xs text-slate-400">{{.Summary}}</span>
  </div>

  <details class="mt-3 rounded-lg bg-slate-900/60 border border-slate-800 px-4 py-2 text-xs text-slate-400">
    <summary class="cursor-pointer text-slate-300">
      <strong>{{.RealismHidden}}</strong> items hidden by always-on realism filters &mdash; why?
    </summary>
    <ul class="mt-2 space-y-1 list-disc list-inside">
      {{range .RealismRules}}<li>{{.}}</li>{{end}}
    </ul>
  </details>

  <form id="filterform" hx-get="/" hx-target="#results" hx-swap="innerHTML" hx-push-url="true"
        class="flex flex-wrap items-end gap-3 rounded-lg bg-slate-900 border border-slate-800 px-4 py-3 mt-3">
    <input type="hidden" name="sort" value="{{.Sort}}">
    <label class="text-xs text-slate-400">Min vol/day{{help "Hide items whose average daily Heimatar volume is below this. Default 20 keeps dead markets out; clear it to show everything."}}
      <input name="minvol" value="{{index .Inputs "minvol"}}" size="7" class="block mt-1 bg-slate-950 border border-slate-700 rounded px-2 py-1 text-sm text-slate-100"></label>
    <label class="text-xs text-slate-400">Min gross margin %{{help "Hide items whose gross spread is below this. Gross margin = (sell - buy) / sell; the default 7% clears fees at max skills. Clear for no floor."}}
      <input name="minmargin" value="{{index .Inputs "minmargin"}}" size="5" class="block mt-1 bg-slate-950 border border-slate-700 rounded px-2 py-1 text-sm text-slate-100"></label>
    <label class="text-xs text-slate-400">Max gross margin %{{help "Hide items whose gross spread is above this - usually a stale or manipulated price. Default 60%; clear it to show those outliers."}}
      <input name="maxmargin" value="{{index .Inputs "maxmargin"}}" size="5" class="block mt-1 bg-slate-950 border border-slate-700 rounded px-2 py-1 text-sm text-slate-100"></label>
    <label class="text-xs text-slate-400">Max sell price{{help "Budget cap on the item's sell price. Leave blank for no cap."}}
      <input name="maxsell" value="{{index .Inputs "maxsell"}}" size="12" placeholder="no cap" class="block mt-1 bg-slate-950 border border-slate-700 rounded px-2 py-1 text-sm text-slate-100"></label>
    <button type="submit" class="bg-emerald-600 hover:bg-emerald-500 text-slate-950 text-sm font-medium px-3 py-1.5 rounded">Apply</button>
  </form>

  <table class="w-full text-sm border-collapse mt-4">
    <thead>
      <tr class="text-left border-b border-slate-700">
        <th class="py-2 pr-4 text-slate-400">Item</th>
        {{.HeaderBuy}}{{.HeaderSell}}{{.HeaderMargin}}{{.HeaderUnit}}{{.HeaderVol}}{{.HeaderDay}}
      </tr>
    </thead>
    <tbody>{{.Rows}}</tbody>
  </table>
  <p class="text-xs text-slate-500 mt-4">{{.Footnote}}</p>
</div>
`))

type variantCData struct {
	Variant, Sort string
	Summary       template.HTML
	Inputs                 map[string]string
	Chips                  []chip
	RealismRules           []string
	RealismHidden          int
	Footnote               template.HTML
	Rows                   template.HTML
	HeaderBuy, HeaderSell, HeaderMargin, HeaderUnit, HeaderVol, HeaderDay template.HTML
}

func renderVariantC(m pageModel) template.HTML {
	rows := mustExec(tableRowsTpl, m.Rows)
	d := variantCData{
		Variant: m.Variant, Sort: m.Sort, Summary: summaryText(m), Inputs: m.Inputs,
		Chips: m.Chips, RealismRules: m.RealismRules, RealismHidden: m.RealismHidden,
		Footnote: template.HTML(footnoteHTML), Rows: rows,
		HeaderBuy:    headerCell("Buy", "buy", m.Sort, ""),
		HeaderSell:   headerCell("Sell", "sell", m.Sort, ""),
		HeaderMargin: headerCell("Net margin", "margin", m.Sort, ""),
		HeaderUnit:   headerCell("ISK/unit", "iskunit", m.Sort, ""),
		HeaderVol:    headerCell("Vol/day", "volday", m.Sort, ""),
		HeaderDay:    headerCell("ISK/day", "iskday", m.Sort, " ↓"),
	}
	return mustExec(variantCTpl, d)
}

// ---- page shell + floating switcher ----

var pageTpl = template.Must(template.New("page").Parse(`
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>eve-trader prototype — filter UI variant {{.Variant}}</title>
<script src="https://unpkg.com/htmx.org@1.9.12"></script>
<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-slate-950 text-slate-100 font-sans pb-28">
<header class="border-b border-slate-800 px-6 py-4">
  <h1 class="text-xl font-semibold">Rens Station Trading Opportunities</h1>
  <p class="text-xs text-slate-500 mt-1">v1.1 prototype: user filters, realism exclusions, capture-based ISK/day</p>
</header>
<main class="px-6 py-5 max-w-[110rem] mx-auto">
  {{.Body}}
</main>

<div class="fixed bottom-6 left-1/2 -translate-x-1/2 bg-slate-800/95 border border-slate-600 shadow-lg rounded-full flex items-center gap-3 px-3 py-2 text-sm">
  <a href="{{.PrevHref}}" class="px-2 py-1 rounded-full hover:bg-slate-700" aria-label="previous variant">&larr;</a>
  <span class="font-medium">{{.Variant}} &mdash; {{.VariantName}}</span>
  <a href="{{.NextHref}}" class="px-2 py-1 rounded-full hover:bg-slate-700" aria-label="next variant">&rarr;</a>
</div>

<script>
document.addEventListener('keydown', function(e) {
  var t = e.target;
  if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
  if (e.key === 'ArrowLeft') document.querySelector('[aria-label="previous variant"]').click();
  if (e.key === 'ArrowRight') document.querySelector('[aria-label="next variant"]').click();
});
</script>
</body>
</html>
`))

type shellData struct {
	Variant     string
	VariantName string
	PrevHref    string
	NextHref    string
	Body        template.HTML
}

func handle(w http.ResponseWriter, r *http.Request) {
	m := buildModel(r.URL.Query())
	var body template.HTML
	switch m.Variant {
	case "B":
		body = renderVariantB(m)
	case "C":
		body = renderVariantC(m)
	default:
		body = renderVariantA(m)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := pageTpl.Execute(w, shellData{
		Variant: m.Variant, VariantName: m.VariantName,
		PrevHref: m.PrevHref, NextHref: m.NextHref, Body: body,
	}); err != nil {
		log.Println(err)
	}
}

func main() {
	http.HandleFunc("/", handle)
	addr := ":8090"
	log.Println("prototype-filter-ui listening on", addr, "— open http://localhost:8090/?variant=A")
	log.Fatal(http.ListenAndServe(addr, nil))
}
