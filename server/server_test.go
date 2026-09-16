package server_test

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgoodness/eve-trader/db"
	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/server"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}

// TestIndexRespondsOK demonstrates the test-harness convention the rest of
// the build follows: a black-box HTTP test (httptest.Server) against a
// real SQLite database and a fake ESIGateway.
func TestIndexRespondsOK(t *testing.T) {
	sqlDB := openTestDB(t)
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
