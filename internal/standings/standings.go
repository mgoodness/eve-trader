// Package standings resolves the character standings that reduce the
// broker fee at a given NPC station (docs/spec/v2.md §9). The fee formula
// needs two numbers: the character's standing toward the corporation that
// owns the station, and toward that corporation's faction. The owner is
// stored from GET /universe/stations/{id} and the faction comes from the
// vendored npcfactions table; a missing owner, faction, or standing all
// resolve to 0 (no reduction), matching ESI's "no entry means no standing".
package standings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/mgoodness/eve-trader/internal/npcfactions"
)

// Standings holds the character's unmodified standings toward the station
// owner corporation and its faction, each on ESI's −10…+10 scale. Zero is
// "no standing" and leaves the broker fee unchanged.
type Standings struct {
	Corp    float64
	Faction float64
}

// Load returns the standings that apply at stationID for characterID. It
// reads the stored station owner (a public ESI fact, refreshed by the
// skill poller) and that character's standings, mapping the owner to its
// faction through the vendored SDE table. No stored owner, no character,
// or no matching standing is not an error: the corresponding field is 0,
// so the fee stays the skills-only v2 baseline rather than failing a page
// render.
func Load(ctx context.Context, db *sql.DB, characterID int, stationID int64) (Standings, error) {
	if characterID == 0 {
		return Standings{}, nil
	}

	var ownerCorp int64
	err := db.QueryRowContext(ctx,
		`SELECT owner_corp_id FROM station_owner WHERE station_id = ?`, stationID).Scan(&ownerCorp)
	if errors.Is(err, sql.ErrNoRows) {
		return Standings{}, nil
	}
	if err != nil {
		return Standings{}, fmt.Errorf("loading station %d owner: %w", stationID, err)
	}

	s := Standings{}
	s.Corp, err = loadStanding(ctx, db, characterID, "npc_corp", ownerCorp)
	if err != nil {
		return Standings{}, err
	}
	if faction, ok := npcfactions.Faction(int(ownerCorp)); ok {
		s.Faction, err = loadStanding(ctx, db, characterID, "faction", int64(faction))
		if err != nil {
			return Standings{}, err
		}
	}
	return s, nil
}

// loadStanding reads the standing characterID holds toward fromType/fromID.
// Missing means 0 (no reduction), not an error.
func loadStanding(ctx context.Context, db *sql.DB, characterID int, fromType string, fromID int64) (float64, error) {
	var standing float64
	err := db.QueryRowContext(ctx,
		`SELECT standing FROM character_standing
		 WHERE character_id = ? AND from_type = ? AND from_id = ?`,
		characterID, fromType, fromID).Scan(&standing)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("loading %s standing for %d: %w", fromType, fromID, err)
	}
	return standing, nil
}
