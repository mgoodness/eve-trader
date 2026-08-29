package numfmt

import "testing"

func TestFormatFloat(t *testing.T) {
	cases := []struct {
		name     string
		f        float64
		decimals int
		want     string
	}{
		{"zero", 0, 2, "0.00"},
		{"under a thousand", 999.5, 2, "999.50"},
		{"exactly a thousand", 1000, 2, "1,000.00"},
		{"millions", 15000000, 2, "15,000,000.00"},
		{"billions, no decimals", 2541345125, 0, "2,541,345,125"},
		{"negative margin", -1234.56, 2, "-1,234.56"},
		{"negative under a thousand", -42.5, 2, "-42.50"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatFloat(c.f, c.decimals); got != c.want {
				t.Errorf("FormatFloat(%v, %d) = %q, want %q", c.f, c.decimals, got, c.want)
			}
		})
	}
}

func TestFormatInt(t *testing.T) {
	cases := []struct {
		name string
		n    int64
		want string
	}{
		{"zero", 0, "0"},
		{"under a thousand", 999, "999"},
		{"exactly a thousand", 1000, "1,000"},
		{"thousands", 12000, "12,000"},
		{"millions", 2100000, "2,100,000"},
		{"billions", 2541345125, "2,541,345,125"},
		{"negative", -1234, "-1,234"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatInt(c.n); got != c.want {
				t.Errorf("FormatInt(%d) = %q, want %q", c.n, got, c.want)
			}
		})
	}
}
