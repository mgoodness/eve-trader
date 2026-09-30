// Package cli is the thin CLI adapter (spec §5): it owns ESI and disk
// access, and renders the pure engine package's Result as JSON. It performs
// no engine logic itself.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mgoodness/eve-trader/internal/cache"
	"github.com/mgoodness/eve-trader/internal/config"
	"github.com/mgoodness/eve-trader/internal/engine"
	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/spf13/cobra"
)

// regionOrdersTTL is the ESI cache window for the region-orders feed (spec
// §6).
const regionOrdersTTL = 300 * time.Second

// Config is everything Run needs to produce a Result for one candidate
// type. DefaultConfig fills in the walking skeleton's compiled-in values;
// LoadConfig additionally resolves config.toml/credentials.json/the cache
// directory from disk (spec §13). Tests call DefaultConfig and override
// ESIBaseURL and CacheDir to point at a fake server and a temp directory.
type Config struct {
	ESIBaseURL string
	SSOBaseURL string
	UserAgent  string
	CompatDate string
	CacheDir   string

	// ConfigDir is where credentials.json lives, so Run can persist a
	// rotated refresh token back to disk (spec §12). Set by LoadConfig;
	// left empty by DefaultConfig, in which case Run skips persistence (no
	// disk-backed credentials file to rotate).
	ConfigDir string

	RegionID       int32
	TradeStationID int64
	TradeSystemID  int32

	// StationOwnerCorpID and RegionFactionID identify whose standings
	// reduce the NPC-station broker fee (spec §4; research
	// eve-market-mechanics-and-esi.md §5, §7.3): the trade station's
	// owning corporation and the region's controlling faction.
	StationOwnerCorpID int32
	RegionFactionID    int32

	TypeID int32
	Name   string

	// Fees is a placeholder filled in from DefaultConfig for callers that
	// never call Run (e.g. future adapters that price a candidate
	// directly); Run itself always overwrites it with live skills/
	// standings-derived fees (ticket #16) before pricing.
	Fees engine.Fees

	// Values holds the configurable run parameters (spec §13): built from
	// config.Defaults(), overlaid by config.toml (LoadConfig), overlaid in
	// turn by recommend's CLI flags.
	Values config.Values

	// Credentials is read from credentials.json by LoadConfig. It is never
	// settable by a CLI flag (spec §13). Run requires it: ticket #16 mints
	// a live access token from the stored refresh token on every run.
	Credentials config.Credentials
}

// DefaultConfig is the config main.go runs with: live ESI, the Heimatar/Rens
// constants (spec §2), a single compiled-in candidate, and the documented
// config defaults (spec §13). The fee rates are the pilot's real, hardcoded
// rates (spec §4) — a placeholder until ticket #16 reads skills and
// standings live from ESI. Callers that want config.toml/credentials.json
// read from disk should call LoadConfig instead.
func DefaultConfig() Config {
	return Config{
		UserAgent:          "eve-trader/0.1 (+https://github.com/mgoodness/eve-trader)",
		CompatDate:         "2026-09-30",
		CacheDir:           defaultCacheDir(),
		RegionID:           10000030, // Heimatar
		TradeStationID:     60004588, // Rens VI - Moon 8 - Brutor Tribe Treasury
		TradeSystemID:      30002510, // Rens
		StationOwnerCorpID: 1000049,  // Brutor Tribe
		RegionFactionID:    500002,   // Minmatar Republic
		TypeID:             11399,    // Morphite: liquid and two-sided at Rens
		Name:               "Morphite",
		Fees:               engine.Fees{Broker: 0.018, SalesTax: 0.05025},
		Values:             config.Defaults(),
	}
}

// LoadConfig resolves eve-trader's on-disk configuration and state surface
// (spec §13) on top of DefaultConfig: config.toml's values (or the
// documented defaults, if it doesn't exist), the credentials file (mode
// 600, never settable by a flag), and the disk cache directory. A missing
// config.toml is not an error; a malformed config.toml, or a
// present-but-wrong-mode or malformed credentials.json, is — both name the
// offending path. A missing credentials.json is not an error either: no
// command needs ESI auth yet (ticket #16 is the first that will).
func LoadConfig() (Config, error) {
	cfg := DefaultConfig()

	configDir, err := config.ConfigDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolving config directory: %w", err)
	}
	cfg.ConfigDir = configDir

	values, err := config.Load(filepath.Join(configDir, "config.toml"))
	if err != nil {
		return Config{}, err
	}
	cfg.Values = values

	creds, err := config.LoadCredentials(filepath.Join(configDir, "credentials.json"))
	switch {
	case errors.Is(err, config.ErrCredentialsNotFound):
		// Fine: nothing needs ESI auth yet.
	case err != nil:
		return Config{}, err
	default:
		cfg.Credentials = creds
	}

	cacheDir, err := config.CacheDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolving cache directory: %w", err)
	}
	cfg.CacheDir = cacheDir

	return cfg, nil
}

func defaultCacheDir() string {
	dir, err := config.CacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "eve-trader")
	}
	return dir
}

// Run mints a live access token from the pilot's stored refresh token,
// derives fee rates and the order limit from live skills and standings
// (spec §4, §12; ticket #16), fetches the region orders for cfg.TypeID
// (from cache if fresh, otherwise from ESI), prices the front of queue, and
// writes the JSON Result to w.
func Run(ctx context.Context, cfg Config, w io.Writer) error {
	store, err := cache.Open(cfg.CacheDir)
	if err != nil {
		return err
	}

	fees, _, err := pilotFacts(ctx, cfg, store)
	if err != nil {
		return fmt.Errorf("reading live pilot facts: %w", err)
	}

	orders, err := regionOrders(ctx, cfg, store)
	if err != nil {
		return fmt.Errorf("fetching region orders: %w", err)
	}

	params := engine.Params{
		RegionID:       cfg.RegionID,
		TradeStationID: cfg.TradeStationID,
		TradeSystemID:  cfg.TradeSystemID,
		Delta:          cfg.Values.Delta,
		Fees:           fees,
	}
	result := engine.Recommend(orders, cfg.TypeID, cfg.Name, params, time.Now().UTC())

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func regionOrders(ctx context.Context, cfg Config, store *cache.Store) ([]engine.Order, error) {
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

// newRecommendCmd builds the recommend command. Its flags default to
// cfg.Values (already config.toml-overlaid defaults, spec §13) and override
// whichever ones the pilot passes; the merged result is what Run sees.
// Credentials are deliberately not a flag (spec §13) — cfg.Credentials is
// the only source.
func newRecommendCmd(cfg Config) *cobra.Command {
	var jsonOutput bool
	values := cfg.Values

	cmd := &cobra.Command{
		Use:   "recommend",
		Short: "Recommend buy/sell prices for the candidate universe",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !jsonOutput {
				return fmt.Errorf("table output isn't implemented yet (ticket #23); pass --json")
			}
			runCfg := cfg
			runCfg.Values = values
			return Run(cmd.Context(), runCfg, cmd.OutOrStdout())
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "emit the JSON result")
	cmd.Flags().Int64Var(&values.Budget, "budget", values.Budget, "trading budget, self-reported ISK")
	cmd.Flags().Float64Var(&values.TargetMargin, "target-margin", values.TargetMargin, "minimum net margin a candidate must clear to be recommended")
	cmd.Flags().Float64Var(&values.Delta, "delta", values.Delta, "aggression tick (\u03b4), in ISK, for front-of-queue prices")
	cmd.Flags().IntVar(&values.HorizonDays, "horizon", values.HorizonDays, "capture horizon in days")
	cmd.Flags().Int64Var(&values.MinOrder, "min-order", values.MinOrder, "minimum committed capital, in ISK, to post a partial fill")
	cmd.Flags().Float64Var(&values.CaptureRate, "capture-rate", values.CaptureRate, "fraction of 30-day ADV assumed capturable per day")
	cmd.Flags().Float64Var(&values.Filters.GrossMarginCeiling, "gross-margin-ceiling", values.Filters.GrossMarginCeiling, "drop candidates with gross margin above this fraction")
	cmd.Flags().IntVar(&values.Filters.ThinBookMinOrders, "thin-book-min-orders", values.Filters.ThinBookMinOrders, "minimum orders required within the thin-book band, on each side")
	cmd.Flags().Float64Var(&values.Filters.ThinBookBandPct, "thin-book-band-pct", values.Filters.ThinBookBandPct, "price band, as a fraction of best, used by the thin-book filter")
	cmd.Flags().IntVar(&values.Filters.MinHistoryDays, "min-history-days", values.Filters.MinHistoryDays, "minimum days of 30-day history required")
	cmd.Flags().Float64Var(&values.Filters.MinLiquidityADV, "min-liquidity-adv", values.Filters.MinLiquidityADV, "minimum 30-day average daily volume, in units")
	cmd.Flags().Float64Var(&values.Filters.PriceBandLow, "price-band-low", values.Filters.PriceBandLow, "drop if best bid is below this fraction of the 30-day low")
	cmd.Flags().Float64Var(&values.Filters.PriceBandHigh, "price-band-high", values.Filters.PriceBandHigh, "drop if best ask is above this fraction of the 30-day high")

	return cmd
}
