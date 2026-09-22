package server

import (
	"html/template"
	"math"
	"strconv"

	"github.com/mgoodness/eve-trader/ledger"
	"github.com/mgoodness/eve-trader/ranking"
)

// templateFuncs are the formatting helpers used by the opportunity-table
// templates.
var templateFuncs = template.FuncMap{
	"fmtISK": func(v float64) string { return formatThousands(round(v)) + " ISK" },
	"fmtPct": func(v float64) string { return trimFloat(v) + "%" },
	"fmtNum": func(v float64) string { return formatThousands(round(v)) },
	// fmtCaptureRate renders ranking.CaptureRate as a percentage so the
	// user-facing footnote can't silently drift from the constant.
	"fmtCaptureRate": func() string { return trimFloat(ranking.CaptureRate*100) + "%" },

	// Portfolio helpers.
	// fmtSignedISK prefixes a positive figure with "+", so realized and
	// unrealized gains and losses read unambiguously at a glance.
	"fmtSignedISK": func(v float64) string {
		s := formatThousands(round(v)) + " ISK"
		if v > 0 {
			return "+" + s
		}
		return s
	},
	"fmtPrice": func(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) },
	// fmtPriceRange headlines the conservative high end and shows the
	// confidently-allocated low end beside it, so a precise-looking number
	// never hides the unattributed-fee allocation guess (docs/spec/v2.md
	// §4.7). A collapsed range renders as the single headline price.
	"fmtPriceRange": func(low, high float64) string {
		h := strconv.FormatFloat(high, 'f', 2, 64)
		if high-low <= 1e-9 {
			return h
		}
		return h + " (low " + strconv.FormatFloat(low, 'f', 2, 64) + ")"
	},
	"fmtQty": func(n int) string { return formatThousands(int64(n)) },
	// fmtNetMargin renders a fractional net margin (0.038 -> "3.8% net").
	"fmtNetMargin": func(v float64) string { return trimFloat(v*100) + "% net" },
	"fmtLocation":  formatLocation,
	"statusClass":  statusClass,
}

// rensStationID is the character's execution venue, Rens VI - Moon 8 -
// Brutor Tribe Treasury, where orders rest and positions are anchored. The
// Opportunity list now ranks the whole region, but the Portfolio stays
// Rens-anchored, so this is still the one station shown by name. Positions
// can also sit at other locations after a transfer; ESI gives no name for
// those here, so they render by id.
const rensStationID = 60004588

// formatLocation names a location id for the Portfolio's Item/location
// column: Rens by name, other ids numerically, an unknown id as an em dash
// placeholder.
func formatLocation(id int64) string {
	switch {
	case id == rensStationID:
		return "Rens"
	case id == 0:
		return "unknown"
	default:
		return formatThousands(id)
	}
}

// statusClass maps a ledger status to the badge CSS class its row renders
// with.
func statusClass(status ledger.Status) string {
	switch status {
	case ledger.StatusAtTarget:
		return "ok"
	case ledger.StatusBelowTarget:
		return "warn"
	case ledger.StatusTransfer:
		return "transfer"
	default:
		return "muted-badge"
	}
}

// round rounds v to the nearest integer (half away from zero), rather
// than truncating toward zero -- so displayed ISK/unit and volume figures
// don't silently bias downward.
func round(v float64) int64 { return int64(math.Round(v)) }

// formatThousands renders an integer with comma thousands separators,
// e.g. 1234567 -> "1,234,567".
func formatThousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := false
	if len(s) > 0 && s[0] == '-' {
		neg = true
		s = s[1:]
	}
	out := make([]byte, 0, len(s)+len(s)/3)
	for i, c := range []byte(s) {
		if i != 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}

// trimFloat renders a float with up to one decimal place, dropping a
// trailing ".0".
func trimFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	if len(s) > 2 && s[len(s)-2:] == ".0" {
		return s[:len(s)-2]
	}
	return s
}
