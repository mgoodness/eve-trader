package config_test

import (
	"path/filepath"
	"testing"

	"github.com/mgoodness/eve-trader/internal/config"
)

func TestConfigDirHonorsXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg-config")

	got, err := config.ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if want := filepath.Join("/xdg-config", "eve-trader"); got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestConfigDirFallsBackToDotConfigUnderHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "/home/pilot")

	got, err := config.ConfigDir()
	if err != nil {
		t.Fatalf("ConfigDir: %v", err)
	}
	if want := filepath.Join("/home/pilot", ".config", "eve-trader"); got != want {
		t.Errorf("ConfigDir() = %q, want %q", got, want)
	}
}

func TestCacheDirHonorsXDGCacheHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/xdg-cache")

	got, err := config.CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	if want := filepath.Join("/xdg-cache", "eve-trader"); got != want {
		t.Errorf("CacheDir() = %q, want %q", got, want)
	}
}

func TestCacheDirFallsBackToDotCacheUnderHome(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("HOME", "/home/pilot")

	got, err := config.CacheDir()
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	if want := filepath.Join("/home/pilot", ".cache", "eve-trader"); got != want {
		t.Errorf("CacheDir() = %q, want %q", got, want)
	}
}
