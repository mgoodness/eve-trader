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
// "~/Library/Application Support/eve-trader" on macOS and disagree with the
// credentials file already provisioned on disk at ~/.config/eve-trader (see
// scripts/esi-sso-wizard.sh).
func ConfigDir() (string, error) {
	return xdgDir("XDG_CONFIG_HOME", ".config")
}

// CacheDir resolves eve-trader's disk cache directory (spec §13, ticket
// #17). Same XDG-over-os.UserCacheDir() reasoning as ConfigDir.
func CacheDir() (string, error) {
	return xdgDir("XDG_CACHE_HOME", ".cache")
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
