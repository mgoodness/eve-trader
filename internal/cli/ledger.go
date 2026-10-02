package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/config"
	"github.com/mgoodness/eve-trader/internal/engine"
	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/spf13/cobra"
)

// ESI cache windows for the ledger routes (spec §7; research
// esi-assets-and-orders.md §2.4, §1.5): open orders 1,200s, order history
// 3,600s, assets 3,600s.
const (
	characterOrdersTTL       = 1200 * time.Second
	characterOrderHistoryTTL = 3600 * time.Second
	characterAssetsTTL       = 3600 * time.Second
)

// LedgerPath resolves the trading-stock ledger's location — the flat JSON
// file at the state directory (spec §13, §16; ADR 0003).
func LedgerPath() (string, error) {
	dir, err := config.StateDir()
	if err != nil {
		return "", fmt.Errorf("resolving state directory: %w", err)
	}
	return filepath.Join(dir, "ledger.json"), nil
}

// ledgerPathFor returns cfg's ledger path, falling back to the default
// LedgerPath when cfg carries no StateDir override.
func ledgerPathFor(cfg Config) (string, error) {
	if cfg.StateDir != "" {
		return filepath.Join(cfg.StateDir, "ledger.json"), nil
	}
	return LedgerPath()
}

// LoadLedger reads the ledger at path. A missing file is not an error — it
// yields nil lots, so the first run after install starts from an empty
// ledger. A malformed file errors, naming the path.
func LoadLedger(path string) ([]engine.Lot, error) {
	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading ledger %s: %w", path, err)
	}

	var lots []engine.Lot
	if err := json.Unmarshal(body, &lots); err != nil {
		return nil, fmt.Errorf("parsing ledger %s: %w", path, err)
	}
	return lots, nil
}

// SaveLedger writes lots to path as mode-600 JSON, creating the state
// directory (0700) and replacing the file atomically (temp file + rename),
// mirroring config.SaveCredentials. The atomic write means a crash
// mid-save can never leave a partially-written ledger behind.
func SaveLedger(path string, lots []engine.Lot) error {
	if lots == nil {
		lots = []engine.Lot{}
	}
	body, err := json.MarshalIndent(lots, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding ledger %s: %w", path, err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating state directory for %s: %w", path, err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("writing ledger %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return fmt.Errorf("writing ledger %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing ledger %s: %w", path, err)
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return fmt.Errorf("writing ledger %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("writing ledger %s: %w", path, err)
	}
	return nil
}

// ReconcileLedger is the shared reconcile step (spec §7): it loads the
// stored ledger, fetches the character's open orders, order history, and
// assets (reusing the run's single access token, PilotFacts.AccessToken),
// folds them through the pure engine.Reconcile, and atomically saves the
// result. Both `recommend` and the `ledger` inspection command call it.
func ReconcileLedger(ctx context.Context, cfg Config) ([]engine.Lot, []engine.ReconcileNote, error) {
	path, err := ledgerPathFor(cfg)
	if err != nil {
		return nil, nil, err
	}

	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		return nil, nil, err
	}

	facts, err := pilotFacts(ctx, cfg, store)
	if err != nil {
		return nil, nil, fmt.Errorf("reading live pilot facts: %w", err)
	}

	client := esi.NewClient(esi.ClientOptions{
		BaseURL:    cfg.ESIBaseURL,
		UserAgent:  cfg.UserAgent,
		CompatDate: cfg.CompatDate,
	})

	orders, err := cachedCharacterOrders(ctx, store, client, facts.CharacterID, facts.AccessToken)
	if err != nil {
		return nil, nil, err
	}
	history, err := cachedCharacterOrderHistory(ctx, store, client, facts.CharacterID, facts.AccessToken)
	if err != nil {
		return nil, nil, err
	}
	assets, err := cachedCharacterAssets(ctx, store, client, facts.CharacterID, facts.AccessToken)
	if err != nil {
		return nil, nil, err
	}

	lots, err := LoadLedger(path)
	if err != nil {
		return nil, nil, err
	}

	updated, notes := engine.Reconcile(lots, orders, history, assets, cfg.TradeStationID)
	if err := SaveLedger(path, updated); err != nil {
		return nil, nil, err
	}
	return updated, notes, nil
}

func cachedCharacterOrders(ctx context.Context, store *cache.Store, client *esi.Client, characterID int32, accessToken string) ([]engine.CharacterOrder, error) {
	key := fmt.Sprintf("character-orders:%d", characterID)
	if body, fresh, err := store.Get(key); err == nil && fresh {
		var orders []engine.CharacterOrder
		if err := json.Unmarshal(body, &orders); err == nil {
			return orders, nil
		}
	}

	orders, err := client.CharacterOrders(ctx, characterID, accessToken)
	if err != nil {
		return nil, err
	}
	if body, err := json.Marshal(orders); err == nil {
		_ = store.Set(key, body, characterOrdersTTL)
	}
	return orders, nil
}

func cachedCharacterOrderHistory(ctx context.Context, store *cache.Store, client *esi.Client, characterID int32, accessToken string) ([]engine.CharacterOrder, error) {
	key := fmt.Sprintf("character-orders-history:%d", characterID)
	if body, fresh, err := store.Get(key); err == nil && fresh {
		var history []engine.CharacterOrder
		if err := json.Unmarshal(body, &history); err == nil {
			return history, nil
		}
	}

	history, err := client.CharacterOrderHistory(ctx, characterID, accessToken)
	if err != nil {
		return nil, err
	}
	if body, err := json.Marshal(history); err == nil {
		_ = store.Set(key, body, characterOrderHistoryTTL)
	}
	return history, nil
}

func cachedCharacterAssets(ctx context.Context, store *cache.Store, client *esi.Client, characterID int32, accessToken string) ([]engine.Asset, error) {
	key := fmt.Sprintf("character-assets:%d", characterID)
	if body, fresh, err := store.Get(key); err == nil && fresh {
		var assets []engine.Asset
		if err := json.Unmarshal(body, &assets); err == nil {
			return assets, nil
		}
	}

	assets, err := client.CharacterAssets(ctx, characterID, accessToken)
	if err != nil {
		return nil, err
	}
	if body, err := json.Marshal(assets); err == nil {
		_ = store.Set(key, body, characterAssetsTTL)
	}
	return assets, nil
}

// newLedgerCmd builds the `ledger` inspection command (spec §7): it
// reconciles the stored ledger against live ESI, saves it, and prints the
// lots — no region-market fetch or buy pipeline, so the pilot can inspect
// the ledger without a full `recommend` run.
func newLedgerCmd(cfg Config) *cobra.Command {
	return &cobra.Command{
		Use:   "ledger",
		Short: "Reconcile and inspect the trading-stock ledger",
		RunE: func(cmd *cobra.Command, args []string) error {
			lots, notes, err := ReconcileLedger(cmd.Context(), cfg)
			if err != nil {
				return err
			}
			for _, n := range notes {
				if n.Kind == engine.NoteDriftClamp {
					fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", n.Detail)
					continue
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s\n", n.Kind, n.Detail)
			}
			return renderLots(cmd.OutOrStdout(), lots)
		},
	}
}

// renderLots prints the ledger as a dense table: id, type, status, total
// and available quantity, and acquisition price ("-" for a seeded lot with
// no known cost, ADR 0004).
func renderLots(w io.Writer, lots []engine.Lot) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "LOT ID\tTYPE ID\tSTATUS\tTOTAL\tAVAILABLE\tACQUISITION PRICE")
	for _, lot := range lots {
		price := "-"
		if lot.AcquisitionPrice != nil {
			price = strconv.FormatFloat(*lot.AcquisitionPrice, 'f', 2, 64)
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%d\t%d\t%s\n",
			lot.LotID, lot.TypeID, lot.Status, lot.QuantityTotal, lot.QuantityAvailable, price)
	}
	return tw.Flush()
}
