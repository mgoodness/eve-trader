package cache_test

import (
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/cache"
)

func TestSetThenGetReturnsTheStoredValueWhileFresh(t *testing.T) {
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := store.Set("region-orders:10000030:11399", []byte(`{"hello":"world"}`), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}

	body, fresh, err := store.Get("region-orders:10000030:11399")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !fresh {
		t.Fatalf("got fresh=false right after Set, want true")
	}
	if string(body) != `{"hello":"world"}` {
		t.Errorf("got body %q, want %q", body, `{"hello":"world"}`)
	}
}

func TestGetOnAMissingKeyReportsNotFound(t *testing.T) {
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	body, fresh, err := store.Get("never-set")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fresh || body != nil {
		t.Errorf("got body=%v fresh=%v, want nil/false for a missing key", body, fresh)
	}
}

func TestGetReportsStaleOnceTheTTLHasPassed(t *testing.T) {
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := store.Set("k", []byte("v"), -time.Second); err != nil {
		t.Fatalf("Set: %v", err)
	}

	body, fresh, err := store.Get("k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if fresh {
		t.Errorf("got fresh=true for an already-expired entry, want false")
	}
	if string(body) != "v" {
		t.Errorf("got body %q, want %q (a stale entry's body is still readable)", body, "v")
	}
}
