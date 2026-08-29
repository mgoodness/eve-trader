package web

import (
	"strconv"
	"strings"
)

// formatFloat renders f with thousands-separator grouping and exactly
// decimals digits after the decimal point, e.g.
// formatFloat(15000000, 2) == "15,000,000.00".
func formatFloat(f float64, decimals int) string {
	return groupThousands(strconv.FormatFloat(f, 'f', decimals, 64))
}

// formatInt renders n with thousands-separator grouping, e.g.
// formatInt(2541345125) == "2,541,345,125".
func formatInt(n int64) string {
	return groupThousands(strconv.FormatInt(n, 10))
}

// groupThousands inserts comma separators every three digits into a
// decimal number string's integer part, leaving an optional leading "-"
// and a ".fractional" suffix untouched, e.g. "-1234.56" -> "-1,234.56".
func groupThousands(s string) string {
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}

	intPart, fracPart := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, fracPart = s[:i], s[i:]
	}

	n := len(intPart)
	if n > 3 {
		var b strings.Builder
		lead := n % 3
		if lead == 0 {
			lead = 3
		}
		b.WriteString(intPart[:lead])
		for i := lead; i < n; i += 3 {
			b.WriteByte(',')
			b.WriteString(intPart[i : i+3])
		}
		intPart = b.String()
	}

	out := intPart + fracPart
	if neg {
		out = "-" + out
	}
	return out
}
