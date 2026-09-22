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
	"github.com/mgoodness/eve-trader/server"
)

func TestSkillPollerUpdatesSkillsAndNextRender(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	fake := &esi.Fake{
		RefreshTokenToken: esi.Token{AccessToken: "access", CharacterID: 1},
		Skills:            esi.Skills{BrokerRelationsLevel: 0, AccountingLevel: 0},
	}
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")
	seedCandidate(t, sqlDB, 34, "Tritanium", 100, 120, 50)

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

func TestSkillPollerWithoutStoredTokenIsNoOp(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	gateway := &countingGateway{Fake: &esi.Fake{}}
	srv := server.New(gateway, sqlDB, testAuthConfig())

	if err := srv.NewSkillPoller(server.CharacterSkillsInterval).Poll(t.Context()); err != nil {
		t.Fatalf("Poll() error = %v, want nil with no stored token", err)
	}

	// Polling an absent token is a no-op, but the app is still
	// unauthenticated and must ask for a login.
	response := httptest.NewRecorder()
	srv.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Re-authenticate with EVE") {
		t.Fatalf("GET / with no stored token = %d %q, want re-authentication banner", response.Code, response.Body.String())
	}
	if got := gateway.calls.Load(); got != 0 {
		t.Errorf("RefreshToken calls = %d, want 0 with no stored token", got)
	}
}

func TestSkillPollerRefreshFailureRequiresReauthentication(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")
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

// TestSkillPollerStoresStandingsAndStationOwner is the fast-follow's fetch
// path: a single daily poll refreshes skills, the character's standings,
// and the Rens station owner (docs/spec/v2.md §9).
func TestSkillPollerStoresStandingsAndStationOwner(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	fake := &esi.Fake{
		RefreshTokenToken: esi.Token{AccessToken: "access", CharacterID: 1},
		Skills:            esi.Skills{BrokerRelationsLevel: 4},
		Standings: []esi.Standing{
			{FromID: 1000049, FromType: "npc_corp", Standing: 6.5},
			{FromID: 500002, FromType: "faction", Standing: 7.25},
			{FromID: 3000001, FromType: "agent", Standing: 1.0},
		},
		StationOwner: 1000049,
	}
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")

	srv := server.New(fake, sqlDB, testAuthConfig())
	if err := srv.NewSkillPoller(server.CharacterSkillsInterval).Poll(t.Context()); err != nil {
		t.Fatal(err)
	}

	var corpStanding, factionStanding float64
	if err := sqlDB.QueryRow(
		`SELECT standing FROM character_standing WHERE from_type = 'npc_corp' AND from_id = 1000049`).Scan(&corpStanding); err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.QueryRow(
		`SELECT standing FROM character_standing WHERE from_type = 'faction' AND from_id = 500002`).Scan(&factionStanding); err != nil {
		t.Fatal(err)
	}
	if corpStanding != 6.5 || factionStanding != 7.25 {
		t.Fatalf("stored standings = corp %v faction %v, want 6.5 / 7.25", corpStanding, factionStanding)
	}

	var owner int64
	if err := sqlDB.QueryRow(`SELECT owner_corp_id FROM station_owner WHERE station_id = 60004588`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != 1000049 {
		t.Fatalf("stored Rens owner = %d, want 1000049", owner)
	}
}

// TestSkillPollerStandingsForbiddenLatchesReauth covers the extra consent:
// a stored token that still refreshes but predates the standings scope gets
// a 403 from the standings route, which must latch the same
// "Re-authenticate with EVE" banner a refresh failure does.
func TestSkillPollerStandingsForbiddenLatchesReauth(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	fake := &esi.Fake{
		RefreshTokenToken: esi.Token{AccessToken: "access", CharacterID: 1},
		Skills:            esi.Skills{},
		FetchStandingsErr: &esi.HTTPError{StatusCode: 403, Status: "403 Forbidden"},
	}
	dbtest.SeedToken(t, sqlDB, 1, testAuthConfig().TokenKey, "refresh")

	srv := server.New(fake, sqlDB, testAuthConfig())
	if err := srv.NewSkillPoller(server.CharacterSkillsInterval).Poll(t.Context()); err == nil {
		t.Fatal("Poll() error = nil, want standings 403 failure")
	}

	response := httptest.NewRecorder()
	srv.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	if response.Code != 200 || !strings.Contains(response.Body.String(), "Re-authenticate with EVE") {
		t.Fatalf("GET / after standings 403 = %d %q, want re-authentication banner", response.Code, response.Body.String())
	}
}
