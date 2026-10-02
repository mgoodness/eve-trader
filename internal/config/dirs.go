package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// ConfigDir resolves eve-trader's config directory: credentials.json and
// config.toml both live directly under it (spec §13). It honors
// $XDG_CONFIG_HOME if set, falling back to ~/.config/eve-trader otherwise —
// deliberately not os.UserConfigDir(), which would resolve to
// "~/Library/Application Support/eve-trader" on macOS and disagree with
// ~/.config/eve-trader, where `eve-trader login` writes credentials.json.
func ConfigDir() (string, error) {
	return xdgDir("XDG_CONFIG_HOME", ".config")
}

// CacheDir resolves eve-trader's disk cache directory (spec §13, ticket
// #17). Same XDG-over-os.UserCacheDir() reasoning as ConfigDir.
func CacheDir() (string, error) {
	return xdgDir("XDG_CACHE_HOME", ".cache")
}

// StateDir resolves eve-trader's persistent-state directory (spec §13,
// §16), which holds the trading-stock ledger (spec §7, ADR 0003). Same
// XDG-over-os.UserConfigDir() reasoning as ConfigDir; the XDG default for
// user state is $XDG_STATE_HOME, falling back to ~/.local/state.
func StateDir() (string, error) {
	return xdgDir("XDG_STATE_HOME", filepath.Join(".local", "state"))
}

func xdgDir(envVar, fallbackUnderHome string) (string, error) {
	if dir := os.Getenv(envVar); dir != "" {
		return filepath.Join(dir, "eve-trader"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, fallbackUnderHome, "eve-trader"), nil
}
