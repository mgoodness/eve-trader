package build

import "testing"

// TestDefaultCommitIsDevPlaceholder proves the un-injected default is a
// clear placeholder, not a blank string or zero value -- go test compiles
// this package without -ldflags -X, so Commit is reliably its zero-init
// value here (see the AC: "a plain go build/go run" case).
func TestDefaultCommitIsDevPlaceholder(t *testing.T) {
	if Commit != "dev" {
		t.Errorf("Commit = %q, want the default placeholder %q", Commit, "dev")
	}
}
