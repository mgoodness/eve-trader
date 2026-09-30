// Package cli is the thin CLI adapter (spec §5): it owns ESI and disk
// access, and renders the pure engine package's Result as JSON. It performs
// no engine logic itself.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/engine"
	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/spf13/cobra"
)

// regionOrdersTTL is the ESI cache window for the region-orders feed (spec
// §6).
const regionOrdersTTL = 300 * time.Second

// Config is everything Run needs to produce a Result for one candidate
// type. DefaultConfig fills in the walking skeleton's compiled-in values;
// tests override ESIBaseURL and CacheDir to point at a fake server and a
// temp directory.
type Config struct {
	ESIBaseURL string
	UserAgent  string
	CompatDate string
	CacheDir   string

	RegionID       int32
	TradeStationID int64
	TradeSystemID  int32

	TypeID int32
	Name   string
	Delta  float64
	Fees   engine.Fees
}

// DefaultConfig is the config main.go runs with: live ESI, the Heimatar/Rens
// constants (spec §2), and a single compiled-in candidate. The fee rates are
// the pilot's real, hardcoded rates (spec §4) — a placeholder until ticket
// #16 reads skills and standings live from ESI.
func DefaultConfig() Config {
	return Config{
		UserAgent:      "eve-trader/0.1 (+https://github.com/mgoodness/eve-trader)",
		CompatDate:     "2026-09-30",
		CacheDir:       defaultCacheDir(),
		RegionID:       10000030, // Heimatar
		TradeStationID: 60004588, // Rens VI - Moon 8 - Brutor Tribe Treasury
		TradeSystemID:  30002510, // Rens
		TypeID:         11399,    // Morphite: liquid and two-sided at Rens
		Name:           "Morphite",
		Delta:          100,
		Fees:           engine.Fees{Broker: 0.018, SalesTax: 0.05025},
	}
}

func defaultCacheDir() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "eve-trader")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "eve-trader")
	}
	return filepath.Join(home, ".cache", "eve-trader")
}

// Run fetches the region orders for cfg.TypeID (from cache if fresh,
// otherwise from ESI), prices the front of queue, and writes the JSON
// Result to w.
func Run(ctx context.Context, cfg Config, w io.Writer) error {
	orders, err := regionOrders(ctx, cfg)
	if err != nil {
		return fmt.Errorf("fetching region orders: %w", err)
	}

	params := engine.Params{
		RegionID:       cfg.RegionID,
		TradeStationID: cfg.TradeStationID,
		TradeSystemID:  cfg.TradeSystemID,
		Delta:          cfg.Delta,
		Fees:           cfg.Fees,
	}
	result := engine.Recommend(orders, cfg.TypeID, cfg.Name, params, time.Now().UTC())

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func regionOrders(ctx context.Context, cfg Config) ([]engine.Order, error) {
	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		return nil, err
	}

	key := fmt.Sprintf("region-orders:%d:%d", cfg.RegionID, cfg.TypeID)
	if body, fresh, err := store.Get(key); err == nil && fresh {
		var orders []engine.Order
		if err := json.Unmarshal(body, &orders); err == nil {
			return orders, nil
		}
	}

	client := esi.NewClient(esi.ClientOptions{
		BaseURL:    cfg.ESIBaseURL,
		UserAgent:  cfg.UserAgent,
		CompatDate: cfg.CompatDate,
	})
	orders, err := client.RegionOrders(ctx, cfg.RegionID, "all", cfg.TypeID)
	if err != nil {
		return nil, err
	}

	if body, err := json.Marshal(orders); err == nil {
		_ = store.Set(key, body, regionOrdersTTL)
	}
	return orders, nil
}

// NewRootCmd builds the eve-trader command tree, configured with cfg.
// main.go calls it with DefaultConfig(); tests call it with overrides
// pointing at a fake ESI server and a temp cache dir.
func NewRootCmd(cfg Config) *cobra.Command {
	root := &cobra.Command{
		Use:           "eve-trader",
		Short:         "Station-trading recommendations for EVE Online",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newRecommendCmd(cfg))
	return root
}

func newRecommendCmd(cfg Config) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "recommend",
		Short: "Recommend buy/sell prices for the candidate universe",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !jsonOutput {
				return fmt.Errorf("table output isn't implemented yet (ticket #23); pass --json")
			}
			return Run(cmd.Context(), cfg, cmd.OutOrStdout())
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit the JSON result")
	return cmd
}
