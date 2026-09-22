package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/mgoodness/eve-trader/esi"
)

// CharacterSkillsInterval is how often the character's fee-relevant skills
// and standings are refreshed. Standings change slowly and ESI caches the
// standings route for ~1 h, so a daily cadence is comfortably safe
// (docs/spec/v2.md §9).
const CharacterSkillsInterval = 24 * time.Hour

// SkillPoller refreshes the stored character's skills and standings, and
// the Rens station owner, once per day. It uses the same refresh and
// re-authentication path as authenticated HTTP requests.
type SkillPoller struct {
	server   *Server
	interval time.Duration
}

// NewSkillPoller creates a background character-skill refresher.
func (s *Server) NewSkillPoller(interval time.Duration) *SkillPoller {
	if interval <= 0 {
		interval = CharacterSkillsInterval
	}
	return &SkillPoller{server: s, interval: interval}
}

// Poll refreshes the stored token and refreshes the character's skills,
// standings, and the Rens station owner. No stored token means there is
// nothing to poll yet (the first-boot state): that is a no-op, not a
// polling failure.
func (p *SkillPoller) Poll(ctx context.Context) error {
	token, ok, err := p.server.refreshAuthentication(ctx, true)
	if errors.Is(err, errNoStoredToken) {
		return nil
	}
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	var characterID int
	if err := p.server.db.QueryRowContext(ctx, `SELECT character_id FROM esi_token LIMIT 1`).Scan(&characterID); err != nil {
		return fmt.Errorf("loading stored character ID: %w", err)
	}
	return p.server.refreshTradingProfile(ctx, characterID, token.AccessToken)
}

// refreshTradingProfile fetches and stores everything the fee math needs
// beyond raw ledger records: the character's fee/tax skills, their NPC
// standings, and the Rens station's owner. It is the single fetch path
// shared by the login callback and the daily poller, so both keep the same
// tables current (docs/spec/v2.md §9).
//
// A token that refreshes fine but lacks the standings scope is a distinct
// re-consent case: the standings call returns 403, which latches the same
// "Re-authenticate with EVE" banner a refresh failure does. The skills are
// still stored first, so the banner is the only thing the user must clear.
func (s *Server) refreshTradingProfile(ctx context.Context, characterID int, accessToken string) error {
	now := nowUTC()

	skills, err := s.gateway.FetchCharacterSkills(ctx, characterID, accessToken)
	if err != nil {
		return fmt.Errorf("fetching character skills: %w", err)
	}
	if err := s.upsertCharacterSkill(ctx, characterID, skills, now); err != nil {
		return fmt.Errorf("storing character skills: %w", err)
	}

	standings, err := s.gateway.FetchCharacterStandings(ctx, characterID, accessToken)
	if err != nil {
		s.latchIfInsufficientScope(err)
		return fmt.Errorf("fetching character standings: %w", err)
	}
	if err := s.upsertCharacterStandings(ctx, characterID, standings, now); err != nil {
		return fmt.Errorf("storing character standings: %w", err)
	}

	owner, err := s.gateway.FetchStationOwner(ctx, esi.RensStationID)
	if err != nil {
		return fmt.Errorf("fetching Rens station owner: %w", err)
	}
	if err := s.upsertStationOwner(ctx, esi.RensStationID, owner, now); err != nil {
		return fmt.Errorf("storing Rens station owner: %w", err)
	}
	return nil
}

// upsertCharacterStandings replaces the character's stored standings with
// the fresh set. A standing the character no longer has is deleted rather
// than left stale, so a lost standing stops reducing the fee.
func (s *Server) upsertCharacterStandings(ctx context.Context, characterID int, standings []esi.Standing, updatedAt string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM character_standing WHERE character_id = ?`, characterID); err != nil {
		return err
	}
	for _, st := range standings {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO character_standing (character_id, from_id, from_type, standing, updated_at)
			 VALUES (?, ?, ?, ?, ?)`,
			characterID, st.FromID, st.FromType, st.Standing, updatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// upsertStationOwner stores (or replaces) stationID's owner corporation.
func (s *Server) upsertStationOwner(ctx context.Context, stationID, ownerCorpID int64, updatedAt string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO station_owner (station_id, owner_corp_id, updated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT (station_id) DO UPDATE SET
		   owner_corp_id = excluded.owner_corp_id,
		   updated_at = excluded.updated_at`,
		stationID, ownerCorpID, updatedAt)
	return err
}

// Run performs an immediate refresh and then refreshes daily.
func (p *SkillPoller) Run(ctx context.Context) {
	if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
		slog.Error("polling character trading profile", "err", err)
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
				slog.Error("polling character trading profile", "err", err)
			}
		}
	}
}
