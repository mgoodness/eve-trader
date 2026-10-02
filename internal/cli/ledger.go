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
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
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
	lots, notes, _, err := reconcileLedger(ctx, cfg)
	return lots, notes, err
}

// reconcileLedger is ReconcileLedger's implementation, additionally
// returning the open character orders it fetched: the sell plan needs them
// to reserve held stock already covered by an open station sell order
// (spec §11). It mints the run's pilot facts, so a caller that already has
// them (BuildResult) should call reconcileLedgerWithPilotFacts instead to
// avoid a second refresh-token exchange.
func reconcileLedger(ctx context.Context, cfg Config) ([]engine.Lot, []engine.ReconcileNote, []engine.CharacterOrder, error) {
	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		return nil, nil, nil, err
	}

	facts, err := pilotFacts(ctx, cfg, store)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("reading live pilot facts: %w", err)
	}

	return reconcileLedgerWithPilotFacts(ctx, cfg, store, facts)
}

// reconcileLedgerWithPilotFacts folds fresh ESI snapshots into the stored
// ledger using caller-supplied pilot facts (spec §7; decision 6): a run
// that already minted its one access token — BuildResult, via
// AllocatedUniverse — passes it here rather than triggering a second
// refresh-token exchange, which would replay an already-rotated refresh
// token.
func reconcileLedgerWithPilotFacts(ctx context.Context, cfg Config, store *cache.Store, facts PilotFacts) ([]engine.Lot, []engine.ReconcileNote, []engine.CharacterOrder, error) {
	path, err := ledgerPathFor(cfg)
	if err != nil {
		return nil, nil, nil, err
	}

	client := esi.NewClient(esi.ClientOptions{
		BaseURL:    cfg.ESIBaseURL,
		UserAgent:  cfg.UserAgent,
		CompatDate: cfg.CompatDate,
	})

	orders, err := cachedCharacterOrders(ctx, store, client, facts.CharacterID, facts.AccessToken)
	if err != nil {
		return nil, nil, nil, err
	}
	history, err := cachedCharacterOrderHistory(ctx, store, client, facts.CharacterID, facts.AccessToken)
	if err != nil {
		return nil, nil, nil, err
	}
	assets, err := cachedCharacterAssets(ctx, store, client, facts.CharacterID, facts.AccessToken)
	if err != nil {
		return nil, nil, nil, err
	}

	lots, err := LoadLedger(path)
	if err != nil {
		return nil, nil, nil, err
	}

	updated, notes := engine.Reconcile(lots, orders, history, assets, cfg.TradeStationID)
	if err := SaveLedger(path, updated); err != nil {
		return nil, nil, nil, err
	}
	return updated, notes, orders, nil
}

// ownOrderIDSet fetches the pilot's currently open orders
// (cachedCharacterOrders) and returns their OrderIDs as a set (ticket #51):
// engine.Universe excludes any region or station order whose id is in this
// set from the effective books it builds, so a type never gets priced
// against the pilot's own order. Personal and corp-wallet orders
// (CharacterOrder.IsCorporation) are both included -- v1 has no
// multi-character/corp distinction to make excluding only one of them
// meaningful. A caller that already minted this run's access token (any
// PilotFacts) passes it here rather than triggering a second refresh-token
// exchange; the result is a live ESI call only the first time in a run --
// every later call within the route's 1,200s TTL (characterOrdersTTL) hits
// the warm disk cache instead.
func ownOrderIDSet(ctx context.Context, cfg Config, store *cache.Store, facts PilotFacts) (map[int64]bool, error) {
	client := esi.NewClient(esi.ClientOptions{
		BaseURL:    cfg.ESIBaseURL,
		UserAgent:  cfg.UserAgent,
		CompatDate: cfg.CompatDate,
	})

	orders, err := cachedCharacterOrders(ctx, store, client, facts.CharacterID, facts.AccessToken)
	if err != nil {
		return nil, err
	}

	ids := make(map[int64]bool, len(orders))
	for _, o := range orders {
		ids[o.OrderID] = true
	}
	return ids, nil
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
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", n.Detail)
					continue
				}
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s: %s\n", n.Kind, n.Detail)
			}
			names, nameWarnings, err := populateLotNames(cmd.Context(), cfg, lots)
			if err != nil {
				return err
			}
			for _, w := range nameWarnings {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}
			return renderLots(cmd.OutOrStdout(), lots, names)
		},
	}
}

// renderLots prints the ledger as a dense table: id, item, status, total
// and available quantity, when it was acquired, and acquisition price ("-"
// for a seeded lot with no known cost, ADR 0004). names resolves TypeID to
// a display name (internal/cli/names.go); displayName's "type <id>"
// placeholder covers an id the lookup genuinely couldn't resolve. The lot
// id itself stays as the opaque, exact identifier — ACQUIRED is the
// human-scannable way to tell same-item lots apart.
func renderLots(w io.Writer, lots []engine.Lot, names map[int32]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "LOT ID\tITEM\tSTATUS\tTOTAL\tAVAILABLE\tACQUIRED\tACQUISITION PRICE")
	for _, lot := range lots {
		price := "-"
		if lot.AcquisitionPrice != nil {
			price = strconv.FormatFloat(*lot.AcquisitionPrice, 'f', 2, 64)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
			lot.LotID, displayName(lot.TypeID, names[lot.TypeID]), lot.Status, lot.QuantityTotal, lot.QuantityAvailable,
			lot.AcquiredAt.Format("2006-01-02"), price)
	}
	return tw.Flush()
}
