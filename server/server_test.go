package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/server"
)

// TestIndexRespondsOK demonstrates the test-harness convention the rest of
// the build follows: a black-box HTTP test (httptest.Server) against a
// real SQLite database and a fake ESIGateway.
func TestIndexRespondsOK(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	fake := &esi.Fake{}

	srv := httptest.NewServer(server.New(fake, sqlDB))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatalf("GET / error = %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("GET / body is empty")
	}
}
