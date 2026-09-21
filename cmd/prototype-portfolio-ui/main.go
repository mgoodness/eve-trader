// PROTOTYPE — throwaway. UI variants for the v2 Portfolio view.
//
// Question: "What does the separate Portfolio view show, and how?"
// Three structurally different variants on one route, switchable via
// ?variant=, with a floating bottom switcher and ←/→ keyboard cycling.
//
// Run: go run ./cmd/prototype-portfolio-ui   (http://localhost:8099/?variant=A)
//
// Not production code: no tests, no persistence, mock data only.
package main

import (
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"strings"
)

type Position struct {
	Name        string
	Location    string
	Qty         int
	AvgCost     float64
	BestBuy     float64
	BestSell    float64
	Realized    float64
	Relists     int
	FeesAlloc   float64
	FeesUnattr  float64
	RelistLow   float64
	RelistHigh  float64
	Status      string // Clears | Relist to clear | No market | Closed | Transferred
	Disposition string // open | closed | transferred | no-market
	Note        string

	// Precomputed for the templates.
	UnitPnL    float64
	Unrealized float64
	CostPct    int
	MktPct     int
	HasRelist  bool
	StatusCls  string
}

// Mock data chosen to exercise the hard cases: a relist-heavy winner, an
// underwater position needing a relist, a realised winner and loser, a
// no-market position, and a priced transfer.
var positions = []Position{
	{Name: "Tritanium", Location: "Rens", Qty: 50000, AvgCost: 4.80, BestBuy: 5.10, BestSell: 5.41,
		Relists: 3, FeesAlloc: 12000, FeesUnattr: 3000, RelistLow: 5.33, RelistHigh: 5.36, Status: "Clears", Disposition: "open"},
	{Name: "Pyerite", Location: "Rens", Qty: 30000, AvgCost: 6.12, BestBuy: 5.80, BestSell: 6.05,
		Relists: 5, FeesAlloc: 9500, FeesUnattr: 2500, RelistLow: 6.44, RelistHigh: 6.49, Status: "Relist to clear", Disposition: "open"},
	{Name: "Heavy Water", Location: "Rens", Qty: 8000, AvgCost: 420, BestBuy: 430, BestSell: 455,
		Relists: 47, FeesAlloc: 250000, FeesUnattr: 96000, RelistLow: 468, RelistHigh: 481, Status: "Clears", Disposition: "open"},
	{Name: "Isogen", Location: "Rens", Qty: 12000, AvgCost: 91.20,
		Relists: 1, FeesAlloc: 4000, FeesUnattr: 500, Status: "No market", Disposition: "no-market", Note: "nothing on the Rens book"},
	{Name: "Mexallon", Location: "Rens", Qty: 0, Realized: 1234567,
		Relists: 11, FeesAlloc: 80000, FeesUnattr: 21000, Status: "Closed", Disposition: "closed"},
	{Name: "Morphite", Location: "Rens", Qty: 0, Realized: -45000,
		Relists: 4, FeesAlloc: 22000, FeesUnattr: 4000, Status: "Closed", Disposition: "closed"},
	{Name: "Nocxium", Location: "Transferred to alt", Qty: 2000, AvgCost: 780, Realized: 240000,
		Relists: 0, FeesAlloc: 0, Status: "Transferred", Disposition: "transferred", Note: "item-exchange contract @ 900 ISK"},
}

type totals struct {
	Realized, Unrealized, FeesAlloc, FeesUnattr, Total float64
	Open, Closed, Transferred, NoMarket                int
}

func isk(v float64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	n := fmt.Sprintf("%.0f", v)
	var out []byte
	for i, c := range []byte(n) {
		if i != 0 && (len(n)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out) + " ISK"
	}
	return string(out) + " ISK"
}

func signed(v float64) string {
	if v > 0 {
		return "+" + isk(v)
	}
	return isk(v)
}

func money(v float64) string { return fmt.Sprintf("%.2f", v) }

func statusCls(s string) string {
	switch s {
	case "Clears":
		return "ok"
	case "Relist to clear", "Underwater":
		return "warn"
	case "Transferred":
		return "transferred"
	default:
		return "muted-badge"
	}
}

func pctOf(v, total float64) int {
	if total <= 0 {
		return 0
	}
	p := int(math.Round(v / total * 100))
	if p < 0 {
		p = 0
	}
	if p > 100 {
		p = 100
	}
	return p
}

var funcs = template.FuncMap{
	"isk": isk, "signed": signed, "money": money, "vol": isk,
	"statusCls": statusCls, "pctOf": pctOf,
	"add": func(a, b float64) float64 { return a + b },
}

func buildViewData() (viewData, []Position) {
	ps := make([]Position, len(positions))
	copy(ps, positions)
	for i := range ps {
		p := &ps[i]
		p.StatusCls = statusCls(p.Status)
		if p.Disposition == "open" {
			p.UnitPnL = p.BestSell - p.AvgCost
			p.Unrealized = float64(p.Qty) * p.UnitPnL
			max := math.Max(p.AvgCost, p.BestSell)
			p.CostPct = pctOf(p.AvgCost, max)
			p.MktPct = pctOf(p.BestSell-p.AvgCost, max)
		}
		p.HasRelist = p.RelistHigh > 0
	}

	var t totals
	var d viewData
	for _, p := range ps {
		t.Realized += p.Realized
		t.FeesAlloc += p.FeesAlloc
		t.FeesUnattr += p.FeesUnattr
		switch p.Disposition {
		case "open":
			t.Open++
			t.Unrealized += p.Unrealized
			if p.Status == "Clears" {
				d.Clears = append(d.Clears, p)
			} else {
				d.Relist = append(d.Relist, p)
			}
		case "closed":
			t.Closed++
		case "transferred":
			t.Transferred++
			d.Transferred = append(d.Transferred, p)
		case "no-market":
			t.NoMarket++
			d.NoMarket = append(d.NoMarket, p)
		}
	}
	t.Total = t.Realized + t.Unrealized
	d.T = t
	d.P = ps
	d.ClearsN, d.RelistN, d.TransferredN, d.NoMarketN = len(d.Clears), len(d.Relist), len(d.Transferred), len(d.NoMarket)
	return d, ps
}

type viewData struct {
	T                                         totals
	P                                         []Position
	Clears, Relist, Transferred, NoMarket     []Position
	ClearsN, RelistN, TransferredN, NoMarketN int
}

const sharedDefs = `
{{define "stats"}}
<div class="stats">
  <div class="stat"><span class="muted">Realised</span><strong class="{{if lt .T.Realized 0.0}}neg{{else}}pos{{end}}">{{signed .T.Realized}}</strong></div>
  <div class="stat"><span class="muted">Unrealised</span><strong class="{{if lt .T.Unrealized 0.0}}neg{{else}}pos{{end}}">{{signed .T.Unrealized}}</strong></div>
  <div class="stat"><span class="muted">Fees (est)</span><strong>{{isk .T.FeesAlloc}}</strong></div>
  <div class="stat"><span class="muted">Unattributed fees</span><strong class="warnc">{{isk .T.FeesUnattr}}</strong></div>
</div>
{{end}}
{{define "footnote"}}<p class="footnote">Profit figures are net of estimated broker fees and sales tax; per-item fees are estimates because ESI does not link a fee to an order or item. Unattributed fees are mostly re-lists. Portfolio data can be up to an hour stale.</p>{{end}}
`

const variantATmpl = `{{define "variantA"}}<section class="results">
  {{template "stats" .}}
  <div class="tabs">
    <span class="tab active">Open positions ({{.T.Open}})</span>
    <span class="tab">Closed / realised ({{.T.Closed}})</span>
    <span class="tab">Transfers ({{.T.Transferred}})</span>
  </div>
  <table>
    <thead><tr><th>Item</th><th>Qty</th><th>Avg cost</th><th>Mkt sell</th><th>Unreal P/L</th><th>Realized</th><th>Relists</th><th>Est. fees</th><th>Relist clears at</th><th>Status</th></tr></thead>
    <tbody>
    {{range .P}}
      <tr>
        <td>{{.Name}} <span class="muted">{{.Location}}</span></td>
        {{if eq .Disposition "no-market"}}
          <td>{{.Qty}}</td><td>{{money .AvgCost}}</td><td class="muted">—</td><td class="muted">—</td><td class="muted">—</td>
        {{else if eq .Disposition "closed"}}
          <td class="muted">—</td><td class="muted">—</td><td class="muted">—</td><td class="muted">—</td><td class="{{if lt .Realized 0.0}}neg{{else}}pos{{end}}">{{signed .Realized}}</td>
        {{else}}
          <td>{{.Qty}}</td><td>{{money .AvgCost}}</td><td>{{if .HasRelist}}{{money .BestSell}}{{else}}<span class="muted">—</span>{{end}}</td>
          <td class="{{if lt .Unrealized 0.0}}neg{{else}}pos{{end}}">{{signed .Unrealized}}</td><td class="muted">—</td>
        {{end}}
        <td>{{.Relists}}</td>
        <td>{{vol .FeesAlloc}}<span class="est"> est</span></td>
        {{if .HasRelist}}<td><strong>{{money .RelistLow}}–{{money .RelistHigh}}</strong></td>{{else}}<td class="muted">—</td>{{end}}
        <td><span class="badge {{.StatusCls}}">{{.Status}}</span></td>
      </tr>
    {{end}}
    </tbody>
  </table>
  {{template "footnote" .}}
</section>{{end}}`

const variantBTmpl = `{{define "variantB"}}<section class="results">
  <div class="hero">
    <div>
      <div class="muted">Estimated trading P/L</div>
      <div class="hero-num">{{signed .T.Total}}</div>
      <div class="muted">{{signed .T.Realized}} realised &middot; {{signed .T.Unrealized}} unrealised</div>
    </div>
    <div class="fees">
      <div class="muted">Fee friction</div>
      <div class="bar"><span style="width:{{pctOf .T.FeesAlloc (add .T.FeesAlloc .T.FeesUnattr)}}%"></span></div>
      <div class="muted">{{isk .T.FeesAlloc}} allocated <span class="est">est</span></div>
      <div class="unattr">{{isk .T.FeesUnattr}} unattributed (mostly re-lists)</div>
    </div>
  </div>
  <div class="grid">
    {{range .P}}
    <div class="pcard">
      <div class="pcard-head"><strong>{{.Name}}</strong><span class="muted">{{.Location}}</span></div>
      {{if eq .Disposition "open"}}
        <div class="muted">{{.Qty}} units @ {{money .AvgCost}}</div>
        <div class="meter"><span class="cost" style="width:{{.CostPct}}%"></span><span class="mkt" style="width:{{.MktPct}}%"></span></div>
        <div class="row"><span class="muted">cost {{money .AvgCost}}</span><span class="muted">mkt {{money .BestSell}}</span></div>
        <div class="{{if lt .UnitPnL 0.0}}neg{{else}}pos{{end}}">{{signed .UnitPnL}} / unit</div>
        <div class="relist">relists {{.Relists}} &middot; clears at <strong>{{money .RelistLow}}–{{money .RelistHigh}}</strong></div>
      {{else if eq .Disposition "closed"}}
        <div class="muted">closed position</div>
        <div class="{{if lt .Realized 0.0}}neg{{else}}pos{{end}} big">{{signed .Realized}}</div>
        <div class="muted">{{.Relists}} re-lists &middot; {{vol .FeesAlloc}} fees est</div>
      {{else if eq .Disposition "transferred"}}
        <div class="badge transferred">Transferred</div>
        <div class="muted">{{.Note}}</div>
        <div class="pos">transfer gain {{signed .Realized}}</div>
      {{else}}
        <div class="badge no-market">No market</div>
        <div class="muted">{{.Note}}</div>
        <div class="muted">{{.Qty}} units @ {{money .AvgCost}}</div>
      {{end}}
    </div>
    {{end}}
  </div>
  {{template "footnote" .}}
</section>{{end}}`

const variantCTmpl = `{{define "variantC"}}<section class="results">
  {{template "stats" .}}
  <div class="queue">
    <h2>List now — clears fees <span class="count">{{.ClearsN}}</span></h2>
    {{range .Clears}}<div class="act">
      <div><strong>{{.Name}}</strong> <span class="muted">{{.Qty}} @ {{.Location}}</span></div>
      <div class="act-cta">list above <strong>{{money .RelistLow}} ISK</strong></div>
      <div class="muted">{{.Relists}} re-lists &middot; fees {{vol .FeesAlloc}} est</div>
    </div>{{end}}

    <h2>Relist to clear <span class="count">{{.RelistN}}</span></h2>
    {{range .Relist}}<div class="act warn">
      <div><strong>{{.Name}}</strong> <span class="muted">{{.Qty}} @ {{.Location}}</span></div>
      <div class="act-cta">market {{money .BestSell}} &rarr; clears at <strong>{{money .RelistLow}}–{{money .RelistHigh}}</strong></div>
      <div class="muted">underwater at the current book; {{.Relists}} re-lists &middot; fees {{vol .FeesAlloc}} est</div>
    </div>{{end}}

    <h2>Transfers <span class="count">{{.TransferredN}}</span></h2>
    {{range .Transferred}}<div class="act muted-act">
      <div><strong>{{.Name}}</strong> <span class="muted">{{.Qty}} units</span></div>
      <div class="act-cta">left the character — {{.Note}}</div>
      <div class="pos">transfer gain {{signed .Realized}}</div>
    </div>{{end}}

    <h2>No market <span class="count">{{.NoMarketN}}</span></h2>
    {{range .NoMarket}}<div class="act muted-act">
      <div><strong>{{.Name}}</strong> <span class="muted">{{.Qty}} units</span></div>
      <div class="act-cta muted">no Rens book — nothing to price against</div>
    </div>{{end}}
  </div>
  {{template "footnote" .}}
</section>{{end}}`

const pageTmpl = `{{define "page"}}<!doctype html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>eve-trader prototype portfolio — variant {{.Variant}}</title>
<style>
  body { font-family: system-ui, sans-serif; background: #0b1120; color: #e2e8f0; margin: 0; padding: 0 1.5rem 5rem; }
  header { border-bottom: 1px solid #1e293b; padding: 1.2rem 0; }
  h1 { margin: 0 0 .25rem; font-size: 1.15rem; }
  .nav { display:flex; gap:1rem; margin-top:.5rem; font-size:.85rem; }
  .nav a { color:#64748b; text-decoration:none; } .nav a.active { color:#34d399; font-weight:600; }
  .footnote { color: #94a3b8; font-size: .75rem; margin: 1rem 0 0; }
  .stats { display:grid; grid-template-columns: repeat(4,1fr); gap:.75rem; margin:1rem 0; }
  .stat { background:#0f172a; border:1px solid #1e293b; border-radius:.5rem; padding:.6rem .8rem; font-size:.8rem; }
  .stat span { display:block; } .stat strong { font-size:1.05rem; }
  .muted { color:#64748b; } .pos { color:#34d399; } .neg { color:#f87171; } .warnc { color:#fbbf24; } .est { color:#64748b; font-size:.7rem; }
  .tabs { display:flex; gap:1rem; font-size:.8rem; border-bottom:1px solid #1e293b; margin:1rem 0 0; }
  .tab { padding:.4rem .2rem; color:#64748b; } .tab.active { color:#e2e8f0; border-bottom:2px solid #34d399; }
  table { width:100%; border-collapse:collapse; font-size:.82rem; }
  th,td { text-align:left; padding:.5rem .6rem; border-bottom:1px solid #1e293b; white-space:nowrap; }
  th { color:#94a3b8; font-weight:600; font-size:.72rem; text-transform:uppercase; letter-spacing:.03em; }
  .badge { font-size:.7rem; padding:.15rem .45rem; border-radius:99px; border:1px solid #334155; color:#cbd5e1; }
  .badge.ok { border-color:#059669; color:#34d399; } .badge.warn { border-color:#b45309; color:#fbbf24; }
  .badge.transferred { border-color:#4338ca; color:#a5b4fc; } .badge.no-market, .badge.muted-badge { border-color:#334155; color:#94a3b8; }
  .hero { display:grid; grid-template-columns: 1.4fr 1fr; gap:1.5rem; align-items:center; background:#0f172a; border:1px solid #1e293b; border-radius:.6rem; padding:1.2rem; margin:1rem 0; }
  .hero-num { font-size:2.2rem; font-weight:700; color:#34d399; }
  .bar { height:.5rem; background:#1e293b; border-radius:99px; overflow:hidden; margin:.4rem 0; }
  .bar span { display:block; height:100%; background:#34d399; }
  .fees .unattr { color:#fbbf24; font-size:.78rem; margin-top:.3rem; }
  .grid { display:grid; grid-template-columns: repeat(3,1fr); gap:.9rem; }
  .pcard { background:#0f172a; border:1px solid #1e293b; border-radius:.6rem; padding:.9rem; font-size:.82rem; display:flex; flex-direction:column; gap:.3rem; }
  .pcard-head { display:flex; justify-content:space-between; }
  .meter { display:flex; height:.55rem; border-radius:99px; overflow:hidden; background:#1e293b; margin:.3rem 0; }
  .meter .cost { background:#475569; } .meter .mkt { background:#34d399; }
  .row { display:flex; justify-content:space-between; font-size:.72rem; }
  .relist { margin-top:.3rem; border-top:1px dashed #1e293b; padding-top:.4rem; color:#cbd5e1; }
  .big { font-size:1.3rem; font-weight:700; }
  .queue h2 { font-size:.85rem; text-transform:uppercase; letter-spacing:.05em; color:#94a3b8; margin:1.4rem 0 .5rem; }
  .count { background:#1e293b; color:#e2e8f0; border-radius:99px; padding:.05rem .5rem; font-size:.75rem; }
  .act { display:grid; grid-template-columns: 1.2fr 1.4fr 1fr; gap:1rem; align-items:center; background:#0f172a; border:1px solid #1e293b; border-left:3px solid #34d399; border-radius:.5rem; padding:.7rem .9rem; margin:.4rem 0; font-size:.84rem; }
  .act.warn { border-left-color:#fbbf24; } .act.muted-act { border-left-color:#334155; opacity:.8; }
  .act-cta strong { font-size:1rem; }
  .switcher { position:fixed; bottom:1.2rem; left:50%; transform:translateX(-50%); display:flex; align-items:center; gap:.9rem; background:#1e293b; border:1px solid #475569; border-radius:99px; padding:.5rem .9rem; font-size:.85rem; box-shadow:0 6px 20px rgba(0,0,0,.5); }
  .switcher a { color:#e2e8f0; text-decoration:none; font-size:1.1rem; padding:0 .3rem; }
  .switcher .name { font-weight:600; }
</style></head>
<body>
<header>
  <h1>Rens Station Trading Opportunities</h1>
  <div class="nav"><a href="#">Opportunities</a><a href="#" class="active">Portfolio</a></div>
  <p class="footnote">Prototype — variant {{.Variant}} ({{.VariantName}}): UI-only, mock data, no persistence.</p>
</header>
{{.Body}}
<div class="switcher">
  <a href="?variant={{.Prev}}" title="previous">&larr;</a>
  <span class="name">{{.Variant}} — {{.VariantName}}</span>
  <a href="?variant={{.Next}}" title="next">&rarr;</a>
</div>
<script>
document.addEventListener('keydown', function(e){
  var t = e.target;
  if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
  if (e.key === 'ArrowLeft') location.search = '?variant={{.Prev}}';
  if (e.key === 'ArrowRight') location.search = '?variant={{.Next}}';
});
</script>
</body></html>{{end}}`

var tmpl = template.Must(template.New("").Funcs(funcs).Parse(
	sharedDefs + variantATmpl + variantBTmpl + variantCTmpl + pageTmpl))

type variantDef struct{ Key, Name, Tmpl string }

var variants = []variantDef{
	{"A", "Dense table", "variantA"},
	{"B", "P/L dashboard", "variantB"},
	{"C", "Action queue", "variantC"},
}

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		key := strings.ToUpper(r.URL.Query().Get("variant"))
		idx := 0
		for i, v := range variants {
			if v.Key == key {
				idx = i
			}
		}
		d, _ := buildViewData()
		var body strings.Builder
		if err := tmpl.ExecuteTemplate(&body, variants[idx].Tmpl, d); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = tmpl.ExecuteTemplate(w, "page", map[string]any{
			"Variant":     variants[idx].Key,
			"VariantName": variants[idx].Name,
			"Prev":        variants[(idx+len(variants)-1)%len(variants)].Key,
			"Next":        variants[(idx+1)%len(variants)].Key,
			"Body":        template.HTML(body.String()),
		})
	})
	log.Println("prototype on http://localhost:8099/?variant=A")
	log.Fatal(http.ListenAndServe(":8099", nil))
}
