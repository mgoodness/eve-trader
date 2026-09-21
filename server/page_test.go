package server_test

import (
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/internal/dbtest"
)

// TestFootnoteWrapsToBrowserWidth guards the header footnote against a
// fixed width cap: the footnote must size to its containing block (the
// browser content width) so it wraps with the window, not at an arbitrary
// fixed measure. The suite is Go-only with no browser seam, so this asserts
// the rendered `.footnote` CSS rule declares no width cap rather than
// measuring layout.
func TestFootnoteWrapsToBrowserWidth(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedSkills(t, sqlDB, 1, 4, 3)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh-token")
	seedCandidate(t, sqlDB, 34, "Passing Ore", 100, 120, 40)

	body := renderIndex(t, sqlDB)

	rule := cssRule(t, body, ".footnote")
	for _, banned := range []string{"max-width", " width:"} {
		if strings.Contains(rule, banned) {
			t.Errorf(".footnote rule must not cap its width (%q), got: %s", banned, rule)
		}
	}
}

// cssRule returns the declaration block for the first `selector` rule in the
// page's inline <style>, e.g. cssRule(body, ".footnote") -> "color: ...".
func cssRule(t *testing.T, body, selector string) string {
	t.Helper()
	start := strings.Index(body, selector+" {")
	if start == -1 {
		t.Fatalf("rendered page has no %q rule", selector)
	}
	start += len(selector) + 2
	end := strings.Index(body[start:], "}")
	if end == -1 {
		t.Fatalf("unterminated %q rule", selector)
	}
	return body[start : start+end]
}
