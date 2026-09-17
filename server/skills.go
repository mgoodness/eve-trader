package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

const CharacterSkillsInterval = 24 * time.Hour

// SkillPoller refreshes the stored character's skills once per day. It uses
// the same refresh and re-authentication path as authenticated HTTP requests.
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

// Poll refreshes the stored token, fetches the current skills, and replaces
// the character_skill row. No stored token means there is nothing to poll yet
// (the first-boot state): that is a no-op, not a polling failure.
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
	skills, err := p.server.gateway.FetchCharacterSkills(ctx, characterID, token.AccessToken)
	if err != nil {
		return fmt.Errorf("fetching character skills: %w", err)
	}
	if err := p.server.upsertCharacterSkill(ctx, characterID, skills, nowUTC()); err != nil {
		return fmt.Errorf("storing character skills: %w", err)
	}
	return nil
}

// Run performs an immediate refresh and then refreshes daily.
func (p *SkillPoller) Run(ctx context.Context) {
	if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
		slog.Error("polling character skills", "err", err)
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := p.Poll(ctx); err != nil && ctx.Err() == nil {
				slog.Error("polling character skills", "err", err)
			}
		}
	}
}
