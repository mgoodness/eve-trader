package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"errors"

	"github.com/mgoodness/eve-trader/esi"
	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/internal/tokencrypt"
	"github.com/mgoodness/eve-trader/server"
)

func TestSkillPollerUpdatesSkillsAndNextRender(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	fake := &esi.Fake{
		RefreshTokenToken: esi.Token{AccessToken: "access", CharacterID: 1},
		Skills:            esi.Skills{BrokerRelationsLevel: 0, AccountingLevel: 0},
	}
	ciphertext, err := tokencrypt.Encrypt(testAuthConfig().TokenKey, "refresh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO esi_token (character_id, owner_hash, encrypted_refresh_token, updated_at) VALUES (1, 'owner', ?, 'now')`, ciphertext); err != nil {
		t.Fatal(err)
	}
	dbtest.SeedItem(t, sqlDB, 34, "Tritanium")
	dbtest.SeedOrder(t, sqlDB, 1, 34, true, 100)
	dbtest.SeedOrder(t, sqlDB, 2, 34, false, 120)
	dbtest.SeedHistory(t, sqlDB, 34, 50, 50)

	srv := server.New(fake, sqlDB, testAuthConfig())
	poller := srv.NewSkillPoller(server.CharacterSkillsInterval)
	if err := poller.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}

	httpServer := httptest.NewServer(srv)
	defer httpServer.Close()
	before := getRenderedBody(t, httpServer.URL)
	if !strings.Contains(before, "4 ISK") {
		t.Fatalf("initial render missing level-0 fee/tax output: %s", before)
	}

	fake.Skills = esi.Skills{BrokerRelationsLevel: 5, AccountingLevel: 5}
	if err := poller.Poll(t.Context()); err != nil {
		t.Fatal(err)
	}
	after := getRenderedBody(t, httpServer.URL)
	if !strings.Contains(after, "13 ISK") {
		t.Fatalf("next render missing updated fee/tax output: %s", after)
	}
	if before == after {
		t.Fatal("render did not change after skill refresh")
	}
}

func TestSkillPollerRefreshFailureRequiresReauthentication(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	ciphertext, err := tokencrypt.Encrypt(testAuthConfig().TokenKey, "refresh")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO esi_token (character_id, owner_hash, encrypted_refresh_token, updated_at) VALUES (1, 'owner', ?, 'now')`, ciphertext); err != nil {
		t.Fatal(err)
	}
	fake := &esi.Fake{RefreshTokenErr: errors.New("refresh token revoked")}
	srv := server.New(fake, sqlDB, testAuthConfig())
	if err := srv.NewSkillPoller(server.CharacterSkillsInterval).Poll(t.Context()); err == nil {
		t.Fatal("Poll() error = nil, want refresh failure")
	}

	response := httptest.NewRecorder()
	srv.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), "Re-authenticate with EVE") {
		t.Fatalf("GET / after refresh failure = %d %q, want re-authentication banner", response.Code, response.Body.String())
	}
}

func getRenderedBody(t *testing.T, baseURL string) string {
	t.Helper()
	response, err := http.Get(baseURL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("GET / status = %d, want 200", response.StatusCode)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
