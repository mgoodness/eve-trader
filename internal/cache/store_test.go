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

func TestSetWithETagThenGetWithETagReturnsTheStoredETagWhileFresh(t *testing.T) {
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := store.SetWithETag("region-orders:10000030:page:1", []byte(`[]`), `"abc123"`, time.Minute); err != nil {
		t.Fatalf("SetWithETag: %v", err)
	}

	body, etag, fresh, err := store.GetWithETag("region-orders:10000030:page:1")
	if err != nil {
		t.Fatalf("GetWithETag: %v", err)
	}
	if !fresh {
		t.Fatalf("got fresh=false right after SetWithETag, want true")
	}
	if string(body) != `[]` {
		t.Errorf("got body %q, want %q", body, `[]`)
	}
	if etag != `"abc123"` {
		t.Errorf("got etag %q, want %q", etag, `"abc123"`)
	}
}

func TestGetWithETagReturnsTheETagOfAStaleEntryForRevalidation(t *testing.T) {
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := store.SetWithETag("k", []byte("v"), `"etag"`, -time.Second); err != nil {
		t.Fatalf("SetWithETag: %v", err)
	}

	body, etag, fresh, err := store.GetWithETag("k")
	if err != nil {
		t.Fatalf("GetWithETag: %v", err)
	}
	if fresh {
		t.Errorf("got fresh=true for an already-expired entry, want false")
	}
	if string(body) != "v" || etag != `"etag"` {
		t.Errorf("got body=%q etag=%q, want %q/%q (a stale entry's body and etag are still readable, for revalidation)", body, etag, "v", `"etag"`)
	}
}

func TestGetWithETagOnAMissingKeyReportsNotFound(t *testing.T) {
	store, err := cache.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	body, etag, fresh, err := store.GetWithETag("never-set")
	if err != nil {
		t.Fatalf("GetWithETag: %v", err)
	}
	if fresh || body != nil || etag != "" {
		t.Errorf("got body=%v etag=%q fresh=%v, want nil/\"\"/false for a missing key", body, etag, fresh)
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
