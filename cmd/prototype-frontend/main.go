// Command prototype-frontend is a THROWAWAY prototype answering:
// "What should the opportunity-list frontend look and behave like?"
// (wayfinder ticket: Prototype the opportunity-list frontend, eve-trader#8)
//
// Three structurally different variants of the same page, switchable via
// ?variant=A|B|C, plus ?authed=false to preview the "needs re-auth" state
// decided in eve-trader#6. Run with: go run ./cmd/prototype-frontend
//
// This code is not meant to be promoted as-is — see docs/agents skills
// "prototype" for the capture/cleanup convention. Whatever variant wins
// gets rewritten properly against the real ranking query and schema (#5, #7).
package main

import (
	"bytes"
	"html/template"
	"log"
	"net/http"
	"sort"
)

// Opportunity is prototype-only fake data shaped like the eventual ranking
// query's output (see eve-trader#5 formula, eve-trader#7 schema).
type Opportunity struct {
	TypeID        int
	Name          string
	PriceBuy      float64
	PriceSell     float64
	MarginPct     float64
	ProfitPerUnit float64 // π
	VolumePerDay  float64 // V_d (Heimatar-region, per #5's documented approximation)
	ISKPerDay     float64 // EDP = π × V_d
}

type CharacterSkills struct {
	Name                 string
	BrokerRelationsLevel int
	AccountingLevel      int
}

var character = CharacterSkills{Name: "Trader McTraderface", BrokerRelationsLevel: 4, AccountingLevel: 3}

var opportunities = []Opportunity{
	{34, "Tritanium", 4.85, 5.35, 9.3, 0.31, 850000, 263500},
	{35, "Pyerite", 11.20, 12.80, 12.5, 1.12, 210000, 235200},
	{44992, "PLEX", 4850000, 5120000, 5.3, 172870, 1.2, 207444},
	{22, "Mexallon", 62.50, 71.00, 12.0, 5.98, 34000, 203320},
	{36, "Isogen", 105.00, 118.50, 11.4, 9.87, 18500, 182595},
	{16273, "Nitrogen Isotopes", 92.10, 99.75, 7.7, 5.02, 32000, 160640},
	{2718, "Antimatter Charge L", 210.00, 245.00, 14.3, 27.66, 5200, 143832},
	{11399, "Morphite", 8850, 9640, 8.2, 583.4, 210, 122514},
	{9832, "Republic Fleet EM Shield Amplifier", 1450000, 1680000, 13.7, 148600, 0.7, 104020},
	{34275, "Skill Injector", 640000000, 665000000, 3.8, 15840000, 0.006, 95040},
	{16274, "Oxygen Isotopes", 88.40, 94.90, 6.9, 4.83, 19000, 91770},
	{40, "Nocxium", 895.00, 985.00, 9.1, 62.30, 1400, 87220},
}

func rankedByISKPerDay() []Opportunity {
	out := make([]Opportunity, len(opportunities))
	copy(out, opportunities)
	sort.Slice(out, func(i, j int) bool { return out[i].ISKPerDay > out[j].ISKPerDay })
	return out
}

const footnoteHTML = `Volume figures are <strong>Heimatar-region-wide</strong>, not Rens-station-specific
	&mdash; ESI has no station-scoped order history for NPC stations (see research #2).
	Treat V_d and ISK/day as an approximation of Rens's actual liquidity.`

// ---- shared helpers ----

var tplFuncs = template.FuncMap{
	"fmtISK": func(v float64) string {
		neg := v < 0
		if neg {
			v = -v
		}
		s := formatThousands(int64(v))
		if neg {
			return "-" + s + " ISK"
		}
		return s + " ISK"
	},
	"fmtPct": func(v float64) string { return trimFloat(v) + "%" },
	"fmtNum": func(v float64) string { return formatThousands(int64(v)) },
}

// mustExec executes tpl with data and returns the result as safe HTML,
// logging (not panicking) on error so one bad render doesn't take the
// whole prototype server down.
func mustExec(tpl *template.Template, data any) template.HTML {
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, data); err != nil {
		log.Printf("template error in %s: %v", tpl.Name(), err)
		return template.HTML("<div class='text-red-400'>template error — see server log</div>")
	}
	return template.HTML(buf.String())
}

// ---- page layout ----

var pageTpl = template.Must(template.New("page").Funcs(tplFuncs).Parse(`
<!doctype html>
<html>
<head>
<meta charset="utf-8">
<title>eve-trader prototype — variant {{.Variant}}</title>
<script src="https://unpkg.com/htmx.org@1.9.12"></script>
<script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-slate-950 text-slate-100 font-sans pb-24">
<header class="border-b border-slate-800 px-6 py-4 flex items-center justify-between">
  <div>
    <h1 class="text-xl font-semibold">Rens Station Trading Opportunities</h1>
    <p class="text-xs text-slate-400 mt-1">{{.FootnoteHTML}}</p>
  </div>
  <div class="text-right text-sm text-slate-300">
    <div>{{.Character.Name}}</div>
    <div class="text-xs text-slate-500">Broker Relations {{.Character.BrokerRelationsLevel}} &middot; Accounting {{.Character.AccountingLevel}}</div>
  </div>
</header>

<main class="p-6">
{{if not .Authed}}
  <div class="max-w-xl mx-auto mt-24 text-center border border-amber-700 bg-amber-950/40 rounded-xl p-8">
    <h2 class="text-lg font-semibold text-amber-300 mb-2">Re-authenticate with EVE</h2>
    <p class="text-sm text-slate-300 mb-6">Your ESI refresh token is no longer valid. Polling is paused until you sign back in.</p>
    <a href="/auth/login" class="inline-block bg-amber-600 hover:bg-amber-500 text-slate-950 font-medium px-5 py-2 rounded-lg">Log in with EVE Online</a>
  </div>
{{else}}
  {{.Body}}
{{end}}
</main>

<div class="fixed bottom-6 left-1/2 -translate-x-1/2 bg-slate-800/95 border border-slate-600 shadow-lg rounded-full flex items-center gap-3 px-3 py-2 text-sm">
  <a href="?variant={{.PrevVariant}}&authed={{.AuthedParam}}" class="px-2 py-1 rounded-full hover:bg-slate-700" aria-label="previous variant">&larr;</a>
  <span class="font-medium">{{.Variant}} &mdash; {{.VariantName}}</span>
  <a href="?variant={{.NextVariant}}&authed={{.AuthedParam}}" class="px-2 py-1 rounded-full hover:bg-slate-700" aria-label="next variant">&rarr;</a>
  <span class="text-slate-500">|</span>
  <a href="?variant={{.Variant}}&authed={{if .Authed}}false{{else}}true{{end}}" class="px-2 py-1 rounded-full hover:bg-slate-700 text-xs text-slate-300">
    {{if .Authed}}preview: needs re-auth{{else}}preview: authed{{end}}
  </a>
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

type pageData struct {
	Variant      string
	VariantName  string
	PrevVariant  string
	NextVariant  string
	Authed       bool
	AuthedParam  string
	Character    CharacterSkills
	FootnoteHTML template.HTML
	Body         template.HTML
}

var variantOrder = []string{"A", "B", "C"}
var variantNames = map[string]string{
	"A": "Dense sortable table",
	"B": "Ranked opportunity cards",
	"C": "Master-detail, show the math",
}

func neighbor(cur string, delta int) string {
	for i, v := range variantOrder {
		if v == cur {
			n := (i + delta + len(variantOrder)) % len(variantOrder)
			return variantOrder[n]
		}
	}
	return variantOrder[0]
}

func render(w http.ResponseWriter, variant string, authed bool, body template.HTML) {
	authedParam := "true"
	if !authed {
		authedParam = "false"
	}
	data := pageData{
		Variant:      variant,
		VariantName:  variantNames[variant],
		PrevVariant:  neighbor(variant, -1),
		NextVariant:  neighbor(variant, 1),
		Authed:       authed,
		AuthedParam:  authedParam,
		Character:    character,
		FootnoteHTML: template.HTML(footnoteHTML),
		Body:         body,
	}
	if err := pageTpl.Execute(w, data); err != nil {
		log.Println(err)
	}
}

// ---- variant A: dense sortable table ----

var variantATpl = template.Must(template.New("a").Funcs(tplFuncs).Parse(`
<div class="max-w-6xl mx-auto">
  <table class="w-full text-sm border-collapse">
    <thead>
      <tr class="text-left text-slate-400 border-b border-slate-700">
        <th class="py-2 pr-4">Item</th>
        <th class="py-2 pr-4 cursor-pointer hover:text-slate-200" hx-get="/partials/sort?by=buy" hx-target="#rows" hx-swap="innerHTML">Buy</th>
        <th class="py-2 pr-4 cursor-pointer hover:text-slate-200" hx-get="/partials/sort?by=sell" hx-target="#rows" hx-swap="innerHTML">Sell</th>
        <th class="py-2 pr-4 cursor-pointer hover:text-slate-200" hx-get="/partials/sort?by=margin" hx-target="#rows" hx-swap="innerHTML">Margin</th>
        <th class="py-2 pr-4 cursor-pointer hover:text-slate-200" hx-get="/partials/sort?by=profit" hx-target="#rows" hx-swap="innerHTML">ISK/unit</th>
        <th class="py-2 pr-4 cursor-pointer hover:text-slate-200" hx-get="/partials/sort?by=volume" hx-target="#rows" hx-swap="innerHTML">Vol/day</th>
        <th class="py-2 pr-4 cursor-pointer hover:text-slate-200 text-slate-200" hx-get="/partials/sort?by=iskday" hx-target="#rows" hx-swap="innerHTML">ISK/day &darr;</th>
      </tr>
    </thead>
    <tbody id="rows">
      {{.RowsHTML}}
    </tbody>
  </table>
</div>
`))

type variantAData struct{ RowsHTML template.HTML }

var rowsATpl = template.Must(template.New("rowsA").Funcs(tplFuncs).Parse(`
{{range .}}
<tr class="border-b border-slate-800 hover:bg-slate-900">
  <td class="py-2 pr-4">{{.Name}}</td>
  <td class="py-2 pr-4 text-slate-300">{{fmtISK .PriceBuy}}</td>
  <td class="py-2 pr-4 text-slate-300">{{fmtISK .PriceSell}}</td>
  <td class="py-2 pr-4">{{fmtPct .MarginPct}}</td>
  <td class="py-2 pr-4">{{fmtISK .ProfitPerUnit}}</td>
  <td class="py-2 pr-4 text-slate-400">{{fmtNum .VolumePerDay}}</td>
  <td class="py-2 pr-4 font-semibold text-emerald-400">{{fmtISK .ISKPerDay}}</td>
</tr>
{{end}}
`))

func handleVariantA(w http.ResponseWriter, authed bool) {
	rowsHTML := mustExec(rowsATpl, rankedByISKPerDay())
	body := mustExec(variantATpl, variantAData{RowsHTML: rowsHTML})
	render(w, "A", authed, body)
}

func handleSortPartial(w http.ResponseWriter, r *http.Request) {
	by := r.URL.Query().Get("by")
	rows := rankedByISKPerDay()
	switch by {
	case "buy":
		sort.Slice(rows, func(i, j int) bool { return rows[i].PriceBuy < rows[j].PriceBuy })
	case "sell":
		sort.Slice(rows, func(i, j int) bool { return rows[i].PriceSell < rows[j].PriceSell })
	case "margin":
		sort.Slice(rows, func(i, j int) bool { return rows[i].MarginPct > rows[j].MarginPct })
	case "profit":
		sort.Slice(rows, func(i, j int) bool { return rows[i].ProfitPerUnit > rows[j].ProfitPerUnit })
	case "volume":
		sort.Slice(rows, func(i, j int) bool { return rows[i].VolumePerDay > rows[j].VolumePerDay })
	default: // iskday, already sorted
	}
	if err := rowsATpl.Execute(w, rows); err != nil {
		log.Println(err)
	}
}

// ---- variant B: ranked opportunity cards (hero + list) ----

var variantBTpl = template.Must(template.New("b").Funcs(tplFuncs).Parse(`
<div class="max-w-3xl mx-auto space-y-6">
  <div class="rounded-2xl bg-gradient-to-br from-emerald-900/60 to-slate-900 border border-emerald-700 p-6">
    <div class="text-xs uppercase tracking-wide text-emerald-400 mb-1">Top opportunity</div>
    <div class="text-2xl font-bold">{{.Top.Name}}</div>
    <div class="text-4xl font-bold text-emerald-400 mt-2">{{fmtISK .Top.ISKPerDay}}<span class="text-base text-slate-400 font-normal">/day</span></div>
    <div class="flex gap-6 mt-4 text-sm text-slate-300">
      <div>Buy {{fmtISK .Top.PriceBuy}}</div>
      <div>Sell {{fmtISK .Top.PriceSell}}</div>
      <div>Margin {{fmtPct .Top.MarginPct}}</div>
      <div>Vol/day {{fmtNum .Top.VolumePerDay}}</div>
    </div>
  </div>

  <div id="card-list" class="space-y-3">
    {{.CardsHTML}}
  </div>
  <div class="text-center">
    <button class="text-sm text-slate-400 hover:text-slate-200 underline"
      hx-get="/partials/more?from=6" hx-target="#card-list" hx-swap="beforeend" hx-trigger="click">
      Load more opportunities
    </button>
  </div>
</div>
`))

type variantBData struct {
	Top       Opportunity
	CardsHTML template.HTML
}

var cardsBTpl = template.Must(template.New("cardsB").Funcs(tplFuncs).Parse(`
{{range .}}
<div class="rounded-xl bg-slate-900 border border-slate-800 p-4 flex items-center justify-between">
  <div>
    <div class="font-medium">{{.Name}}</div>
    <div class="text-xs text-slate-500 mt-1">Margin {{fmtPct .MarginPct}} &middot; Vol/day {{fmtNum .VolumePerDay}}</div>
  </div>
  <div class="text-right">
    <div class="font-semibold text-emerald-400">{{fmtISK .ISKPerDay}}</div>
    <div class="text-xs text-slate-500">per day</div>
  </div>
</div>
{{end}}
`))

func handleVariantB(w http.ResponseWriter, authed bool) {
	ranked := rankedByISKPerDay()
	rest := ranked[1:6]
	cardsHTML := mustExec(cardsBTpl, rest)
	body := mustExec(variantBTpl, variantBData{Top: ranked[0], CardsHTML: cardsHTML})
	render(w, "B", authed, body)
}

func handleMorePartial(w http.ResponseWriter, r *http.Request) {
	rows := rankedByISKPerDay()
	from := 6
	if len(rows) > from {
		rows = rows[from:]
	} else {
		rows = nil
	}
	if err := cardsBTpl.Execute(w, rows); err != nil {
		log.Println(err)
	}
}

// ---- variant C: master-detail, show the math ----

var variantCTpl = template.Must(template.New("c").Funcs(tplFuncs).Parse(`
<div class="max-w-5xl mx-auto flex gap-6">
  <div class="w-64 shrink-0 space-y-1">
    {{range $i, $o := .Ranked}}
    <div class="rounded-lg px-3 py-2 cursor-pointer hover:bg-slate-800 {{if eq $i 0}}bg-slate-800{{end}}"
      hx-get="/partials/detail?id={{$o.TypeID}}" hx-target="#detail" hx-swap="innerHTML">
      <div class="text-sm">{{$o.Name}}</div>
      <div class="text-xs text-emerald-400">{{fmtISK $o.ISKPerDay}}/day</div>
    </div>
    {{end}}
  </div>
  <div id="detail" class="flex-1">
    {{.DetailHTML}}
  </div>
</div>
`))

type variantCData struct {
	Ranked     []Opportunity
	DetailHTML template.HTML
}

var detailCTpl = template.Must(template.New("detailC").Funcs(tplFuncs).Parse(`
<div class="rounded-xl bg-slate-900 border border-slate-800 p-6">
  <h2 class="text-xl font-semibold mb-4">{{.Name}}</h2>
  <div class="space-y-2 text-sm font-mono bg-slate-950 rounded-lg p-4">
    <div>P_s (sell)&nbsp;&nbsp;= {{fmtISK .PriceSell}}</div>
    <div>P_b (buy)&nbsp;&nbsp;&nbsp;= {{fmtISK .PriceBuy}}</div>
    <div>R_b (broker) = 3% &minus; 0.3%&times;{{.BrokerLevel}} = applied</div>
    <div>R_t (tax)&nbsp;&nbsp;&nbsp;&nbsp;= 7.5%&times;(1&minus;0.11&times;{{.AccountingLevel}}) = applied</div>
    <div class="pt-2 border-t border-slate-800">&pi; (profit/unit) = {{fmtISK .ProfitPerUnit}}</div>
    <div>V_d (vol/day)&nbsp;&nbsp; = {{fmtNum .VolumePerDay}}</div>
    <div class="pt-2 border-t border-slate-800 text-emerald-400 font-semibold">EDP = &pi;&times;V_d = {{fmtISK .ISKPerDay}}/day</div>
  </div>
  <div class="mt-4 text-sm text-slate-400">Gross margin: {{fmtPct .MarginPct}}</div>
</div>
`))

type detailView struct {
	Opportunity
	BrokerLevel     int
	AccountingLevel int
}

func handleVariantC(w http.ResponseWriter, authed bool) {
	ranked := rankedByISKPerDay()
	detailHTML := mustExec(detailCTpl, detailView{ranked[0], character.BrokerRelationsLevel, character.AccountingLevel})
	body := mustExec(variantCTpl, variantCData{Ranked: ranked, DetailHTML: detailHTML})
	render(w, "C", authed, body)
}

func handleDetailPartial(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	view := detailView{rankedByISKPerDay()[0], character.BrokerRelationsLevel, character.AccountingLevel}
	for _, o := range opportunities {
		if itoa(o.TypeID) == id {
			view = detailView{o, character.BrokerRelationsLevel, character.AccountingLevel}
			break
		}
	}
	if err := detailCTpl.Execute(w, view); err != nil {
		log.Println(err)
	}
}

// ---- entrypoint ----

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		variant := r.URL.Query().Get("variant")
		if variant == "" {
			variant = "A"
		}
		authed := r.URL.Query().Get("authed") != "false"
		switch variant {
		case "B":
			handleVariantB(w, authed)
		case "C":
			handleVariantC(w, authed)
		default:
			handleVariantA(w, authed)
		}
	})
	http.HandleFunc("/partials/sort", handleSortPartial)
	http.HandleFunc("/partials/more", handleMorePartial)
	http.HandleFunc("/partials/detail", handleDetailPartial)
	http.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		// Stub: real flow is decided in eve-trader#6.
		http.Redirect(w, r, "/?variant=A&authed=true", http.StatusFound)
	})

	addr := ":8090"
	log.Println("prototype-frontend listening on", addr, "— open http://localhost:8090/?variant=A")
	log.Fatal(http.ListenAndServe(addr, nil))
}
