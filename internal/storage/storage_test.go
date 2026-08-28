package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestBootstrapCreatesSchema(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "eve-trader.db")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if err := store.Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	// Bootstrap must be idempotent: safe to call again against an
	// already-bootstrapped file (e.g. every startup).
	if err := store.Bootstrap(ctx); err != nil {
		t.Fatalf("second Bootstrap: %v", err)
	}

	var name string
	row := store.DB.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'oauth_token'`)
	if err := row.Scan(&name); err != nil {
		t.Fatalf("oauth_token table not found: %v", err)
	}
}

func TestPing(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "eve-trader.db")

	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if err := store.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
