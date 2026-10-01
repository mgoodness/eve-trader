package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mgoodness/eve-trader/internal/config"
)

func TestDefaultsMatchTheDocumentedValues(t *testing.T) {
	got := config.Defaults()

	want := config.Values{
		Budget:       150_000_000,
		TargetMargin: 0.10,
		Delta:        100,
		HorizonDays:  3,
		MinOrder:     1_000_000,
		CaptureRate:  0.20,
		Filters: config.FilterThresholds{
			GrossMarginCeiling: 0.80,
			ThinBookMinOrders:  2,
			ThinBookBandPct:    0.05,
			MinHistoryDays:     7,
			MinLiquidityADV:    20,
			PriceBandLow:       0.75,
			PriceBandHigh:      1.25,
		},
	}

	if got != want {
		t.Errorf("Defaults() = %+v, want %+v", got, want)
	}
}

func TestLoadFallsBackToDefaultsWhenTheFileIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != config.Defaults() {
		t.Errorf("Load(missing file) = %+v, want %+v", got, config.Defaults())
	}
}

func TestLoadOverridesOnlyTheFieldsSetInTheFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("delta = 250\nbudget = 50000000\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	want := config.Defaults()
	want.Delta = 250
	want.Budget = 50000000
	if got != want {
		t.Errorf("Load(overrides) = %+v, want %+v", got, want)
	}
}

func TestLoadReturnsAClearErrorForMalformedTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("this is not valid toml ====="), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("Load(malformed) returned no error, want one")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("Load(malformed) error %q does not name the file path %q", err, path)
	}
}

func TestLoadFallsBackToDefaultsWhenTheConfigDirectoryIsMissing(t *testing.T) {
	// The first-run case (spec §13): a pilot who has never written a config has
	// no config directory at all. This must use the documented defaults, not
	// produce the "clear error" ticket #15's AC reserved for a malformed file.
	path := filepath.Join(t.TempDir(), "no-such-dir", "config.toml")

	got, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load(missing directory): %v", err)
	}
	if got != config.Defaults() {
		t.Errorf("Load(missing directory) = %+v, want defaults %+v", got, config.Defaults())
	}
}
