package server

import (
	"html/template"
	"math"
	"strconv"

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
