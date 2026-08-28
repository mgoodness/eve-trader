package storage

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
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

func TestLoadTokenBeforeSaveReturnsErrNoToken(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	_, err := store.LoadToken(ctx)
	if !errors.Is(err, ErrNoToken) {
		t.Fatalf("LoadToken error = %v, want ErrNoToken", err)
	}
}

func TestSaveTokenThenLoadTokenRoundTrips(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	want := Token{
		AccessToken:  "access-1",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(20 * time.Minute).UTC().Truncate(time.Second),
	}
	if err := store.SaveToken(ctx, want); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}

	got, err := store.LoadToken(ctx)
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if got.AccessToken != want.AccessToken {
		t.Errorf("AccessToken = %q, want %q", got.AccessToken, want.AccessToken)
	}
	if got.RefreshToken != want.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q", got.RefreshToken, want.RefreshToken)
	}
	if !got.ExpiresAt.Equal(want.ExpiresAt) {
		t.Errorf("ExpiresAt = %v, want %v", got.ExpiresAt, want.ExpiresAt)
	}
}

func TestSaveTokenOverwritesPreviousToken(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	first := Token{AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresAt: time.Now().Add(20 * time.Minute)}
	second := Token{AccessToken: "access-2", RefreshToken: "refresh-2", ExpiresAt: time.Now().Add(40 * time.Minute)}

	if err := store.SaveToken(ctx, first); err != nil {
		t.Fatalf("SaveToken(first): %v", err)
	}
	if err := store.SaveToken(ctx, second); err != nil {
		t.Fatalf("SaveToken(second): %v", err)
	}

	got, err := store.LoadToken(ctx)
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if got.RefreshToken != second.RefreshToken {
		t.Errorf("RefreshToken = %q, want %q (the newer token)", got.RefreshToken, second.RefreshToken)
	}

	var rowCount int
	if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_token`).Scan(&rowCount); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if rowCount != 1 {
		t.Errorf("oauth_token row count = %d, want 1 (single-row table)", rowCount)
	}
}

func openBootstrapped(t *testing.T) *Store {
	t.Helper()

	store, err := Open(filepath.Join(t.TempDir(), "eve-trader.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if err := store.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	return store
}
