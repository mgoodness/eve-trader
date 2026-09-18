package main

import "strconv"

// formatThousands renders an integer with comma thousands separators, e.g.
// 1234567 -> "1,234,567". Prototype-only helper, not for production use.
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
// trailing ".0". Prototype-only helper.
func trimFloat(v float64) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	if len(s) > 2 && s[len(s)-2:] == ".0" {
		return s[:len(s)-2]
	}
	return s
}

func itoa(n int) string { return strconv.Itoa(n) }
