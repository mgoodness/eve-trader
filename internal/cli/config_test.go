package cli_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mgoodness/eve-trader/internal/cli"
	"github.com/mgoodness/eve-trader/internal/config"
)

func withXDGDirs(t *testing.T) (configDir, cacheDir string) {
	t.Helper()
	configHome := t.TempDir()
	cacheHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	t.Setenv("XDG_CACHE_HOME", cacheHome)
	return filepath.Join(configHome, "eve-trader"), filepath.Join(cacheHome, "eve-trader")
}

func writeConfigDirFile(t *testing.T, configDir, name, body string, perm os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(configDir, name), []byte(body), perm); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLoadConfigFallsBackToDefaultsWhenNothingIsOnDisk(t *testing.T) {
	_, cacheDir := withXDGDirs(t)

	cfg, err := cli.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Values != config.Defaults() {
		t.Errorf("cfg.Values = %+v, want defaults %+v", cfg.Values, config.Defaults())
	}
	if cfg.CacheDir != cacheDir {
		t.Errorf("cfg.CacheDir = %q, want %q", cfg.CacheDir, cacheDir)
	}
	if cfg.Credentials != (config.Credentials{}) {
		t.Errorf("cfg.Credentials = %+v, want zero value", cfg.Credentials)
	}
}

func TestLoadConfigReadsConfigTOMLAndCredentialsFromTheConfigDirectory(t *testing.T) {
	configDir, _ := withXDGDirs(t)
	writeConfigDirFile(t, configDir, "config.toml", "delta = 250\n", 0o644)
	writeConfigDirFile(t, configDir, "credentials.json", validCredentialsJSON, 0o600)

	cfg, err := cli.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Values.Delta != 250 {
		t.Errorf("cfg.Values.Delta = %v, want 250", cfg.Values.Delta)
	}
	if cfg.Credentials.ClientID != "abc123" {
		t.Errorf("cfg.Credentials.ClientID = %q, want %q", cfg.Credentials.ClientID, "abc123")
	}
}

func TestLoadConfigReturnsAClearErrorForMalformedConfigTOML(t *testing.T) {
	configDir, _ := withXDGDirs(t)
	writeConfigDirFile(t, configDir, "config.toml", "not valid toml =====", 0o644)

	_, err := cli.LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig() returned no error for malformed config.toml, want one")
	}
}

func TestLoadConfigReturnsAClearErrorForCredentialsWithWrongMode(t *testing.T) {
	configDir, _ := withXDGDirs(t)
	writeConfigDirFile(t, configDir, "credentials.json", validCredentialsJSON, 0o644)

	_, err := cli.LoadConfig()
	if err == nil {
		t.Fatal("LoadConfig() returned no error for mode-644 credentials, want one")
	}
}

const validCredentialsJSON = `{
  "client_id": "abc123",
  "client_secret": "shh",
  "redirect_uri": "http://127.0.0.1:8000/callback",
  "refresh_token": "refresh-abc"
}`
