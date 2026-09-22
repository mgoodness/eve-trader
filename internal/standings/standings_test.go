package standings_test

import (
	"testing"

	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/internal/standings"
)

// rensStation is the station every trade in scope takes place at.
const rensStation = 60004588

func TestLoadResolvesCorpAndFactionStandings(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	// Brutor Tribe (1000049) owns Rens and belongs to Minmatar Republic
	// (500002) in the vendored SDE table.
	dbtest.SeedStationOwner(t, sqlDB, rensStation, 1000049)
	dbtest.SeedStanding(t, sqlDB, 1, 1000049, "npc_corp", 5.5)
	dbtest.SeedStanding(t, sqlDB, 1, 500002, "faction", 8.25)

	got, err := standings.Load(t.Context(), sqlDB, 1, rensStation)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Corp != 5.5 || got.Faction != 8.25 {
		t.Errorf("Load() = %+v, want Corp 5.5, Faction 8.25", got)
	}
}

func TestLoadMissingStandingsResolveToZero(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedStationOwner(t, sqlDB, rensStation, 1000049)
	// Only the corp standing exists; the faction standing has no row.
	dbtest.SeedStanding(t, sqlDB, 1, 1000049, "npc_corp", -3)

	got, err := standings.Load(t.Context(), sqlDB, 1, rensStation)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Corp != -3 || got.Faction != 0 {
		t.Errorf("Load() = %+v, want Corp -3, Faction 0", got)
	}
}

func TestLoadWithoutStationOwnerIsZero(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)

	got, err := standings.Load(t.Context(), sqlDB, 1, rensStation)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Corp != 0 || got.Faction != 0 {
		t.Errorf("Load() = %+v, want zero standings when the owner is unknown", got)
	}
}

func TestLoadOwnerWithoutFactionSkipsFactionTerm(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	// Doomheim (1000001) is an internal NPC corp the SDE gives no factionID.
	dbtest.SeedStationOwner(t, sqlDB, rensStation, 1000001)
	dbtest.SeedStanding(t, sqlDB, 1, 1000001, "npc_corp", 4)
	// A faction standing for the same character is irrelevant without a
	// faction mapping.
	dbtest.SeedStanding(t, sqlDB, 1, 500002, "faction", 9)

	got, err := standings.Load(t.Context(), sqlDB, 1, rensStation)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Corp != 4 || got.Faction != 0 {
		t.Errorf("Load() = %+v, want Corp 4, Faction 0 for a factionless owner", got)
	}
}

// TestLoadScopesStandingsToCharacter guards the character predicate: a
// second tracked character's standing toward the same owner must not leak
// into the requested character's fee.
func TestLoadScopesStandingsToCharacter(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedStationOwner(t, sqlDB, rensStation, 1000049)
	dbtest.SeedStanding(t, sqlDB, 1, 1000049, "npc_corp", 9)
	dbtest.SeedStanding(t, sqlDB, 2, 1000049, "npc_corp", -4)

	got, err := standings.Load(t.Context(), sqlDB, 2, rensStation)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Corp != -4 {
		t.Errorf("Load(character 2) Corp = %v, want -4 (character 1's 9 must not leak)", got.Corp)
	}
}

// TestLoadZeroCharacterIsZero asserts the first-boot state (no stored token,
// character id 0) resolves to no standings rather than another character's.
func TestLoadZeroCharacterIsZero(t *testing.T) {
	sqlDB := dbtest.OpenDB(t)
	dbtest.SeedStationOwner(t, sqlDB, rensStation, 1000049)
	dbtest.SeedStanding(t, sqlDB, 1, 1000049, "npc_corp", 9)

	got, err := standings.Load(t.Context(), sqlDB, 0, rensStation)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Corp != 0 || got.Faction != 0 {
		t.Errorf("Load(character 0) = %+v, want zero", got)
	}
}
