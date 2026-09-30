package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/config"
	"github.com/mgoodness/eve-trader/internal/engine"
	"github.com/mgoodness/eve-trader/internal/esi"
)

// ESI cache windows for the pilot-facts routes (spec §6): skills 60s,
// standings 3,600s.
const (
	skillsTTL    = 60 * time.Second
	standingsTTL = 3600 * time.Second
)

// pilotFacts mints a fresh access token from the stored refresh token,
// persists any rotated refresh token back to credentials.json, decodes the
// character id from the access token's JWT sub claim, and reads the
// character's skills and standings to derive the pilot's live fee rates
// and order limit (spec §4, §12; ticket #16). No code path here assumes
// max skills: a skill or standing ESI doesn't report for the character is
// treated as untrained/zero by engine.DeriveFees and engine.OrderLimit.
func pilotFacts(ctx context.Context, cfg Config, store *cache.Store) (engine.Fees, int, error) {
	if cfg.Credentials.RefreshToken == "" {
		return engine.Fees{}, 0, fmt.Errorf("no stored refresh token in credentials.json; run scripts/esi-sso-wizard.sh to authorize eve-trader")
	}

	client := esi.NewClient(esi.ClientOptions{
		BaseURL:    cfg.ESIBaseURL,
		SSOBaseURL: cfg.SSOBaseURL,
		UserAgent:  cfg.UserAgent,
		CompatDate: cfg.CompatDate,
	})

	token, err := client.RefreshAccessToken(ctx, cfg.Credentials.ClientID, cfg.Credentials.RefreshToken)
	if err != nil {
		return engine.Fees{}, 0, fmt.Errorf("refreshing access token: %w", err)
	}

	if err := persistRotatedRefreshToken(cfg, token.RefreshToken); err != nil {
		return engine.Fees{}, 0, err
	}

	characterID, err := esi.CharacterIDFromAccessToken(token.AccessToken)
	if err != nil {
		return engine.Fees{}, 0, fmt.Errorf("decoding character id from access token: %w", err)
	}

	skills, err := cachedSkills(ctx, store, client, characterID, token.AccessToken)
	if err != nil {
		return engine.Fees{}, 0, err
	}

	standings, err := cachedStandings(ctx, store, client, characterID, token.AccessToken)
	if err != nil {
		return engine.Fees{}, 0, err
	}

	var factionStanding, corpStanding float64
	for _, s := range standings {
		switch {
		case s.FromType == "faction" && s.FromID == cfg.RegionFactionID:
			factionStanding = s.Standing
		case s.FromType == "npc_corp" && s.FromID == cfg.StationOwnerCorpID:
			corpStanding = s.Standing
		}
	}

	return engine.DeriveFees(skills, factionStanding, corpStanding), engine.OrderLimit(skills), nil
}

// persistRotatedRefreshToken overwrites credentials.json with a rotated
// refresh token (spec §12: "rotates — persist the returned one every
// time"). A rotated token is one that ESI returned and that differs from
// what was already stored; an unchanged or absent rotation is a no-op.
// cfg.ConfigDir empty (DefaultConfig, no disk-backed credentials file)
// also skips persistence — there is nowhere to write it back to.
func persistRotatedRefreshToken(cfg Config, rotated string) error {
	if rotated == "" || rotated == cfg.Credentials.RefreshToken || cfg.ConfigDir == "" {
		return nil
	}

	creds := cfg.Credentials
	creds.RefreshToken = rotated
	path := filepath.Join(cfg.ConfigDir, "credentials.json")
	if err := config.SaveCredentials(path, creds); err != nil {
		return fmt.Errorf("persisting rotated refresh token: %w", err)
	}
	return nil
}

func cachedSkills(ctx context.Context, store *cache.Store, client *esi.Client, characterID int32, accessToken string) (map[int32]int, error) {
	key := fmt.Sprintf("skills:%d", characterID)
	if body, fresh, err := store.Get(key); err == nil && fresh {
		var skills map[int32]int
		if err := json.Unmarshal(body, &skills); err == nil {
			return skills, nil
		}
	}

	skills, err := client.Skills(ctx, characterID, accessToken)
	if err != nil {
		return nil, fmt.Errorf("fetching skills: %w", err)
	}
	if body, err := json.Marshal(skills); err == nil {
		_ = store.Set(key, body, skillsTTL)
	}
	return skills, nil
}

func cachedStandings(ctx context.Context, store *cache.Store, client *esi.Client, characterID int32, accessToken string) ([]esi.Standing, error) {
	key := fmt.Sprintf("standings:%d", characterID)
	if body, fresh, err := store.Get(key); err == nil && fresh {
		var standings []esi.Standing
		if err := json.Unmarshal(body, &standings); err == nil {
			return standings, nil
		}
	}

	standings, err := client.Standings(ctx, characterID, accessToken)
	if err != nil {
		return nil, fmt.Errorf("fetching standings: %w", err)
	}
	if body, err := json.Marshal(standings); err == nil {
		_ = store.Set(key, body, standingsTTL)
	}
	return standings, nil
}
