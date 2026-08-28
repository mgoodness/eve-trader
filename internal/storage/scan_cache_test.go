package storage

import (
	"context"
	"testing"
	"time"
)

func TestSaveAndLoadCharacterSkills(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	fetchedAt := time.Now().UTC().Truncate(time.Second)
	if err := store.SaveCharacterSkills(ctx, map[int32]int32{16622: 5, 3446: 4}, fetchedAt); err != nil {
		t.Fatalf("SaveCharacterSkills: %v", err)
	}

	levels, got, ok, err := store.LoadCharacterSkills(ctx)
	if err != nil {
		t.Fatalf("LoadCharacterSkills: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if levels[16622] != 5 || levels[3446] != 4 {
		t.Errorf("levels = %v, want {16622:5, 3446:4}", levels)
	}
	if !got.Equal(fetchedAt) {
		t.Errorf("fetchedAt = %v, want %v", got, fetchedAt)
	}
}

func TestLoadCharacterSkillsWhenNoneSaved(t *testing.T) {
	_, _, ok, err := openBootstrapped(t).LoadCharacterSkills(context.Background())
	if err != nil {
		t.Fatalf("LoadCharacterSkills: %v", err)
	}
	if ok {
		t.Error("ok = true, want false")
	}
}

func TestSaveCharacterSkillsReplacesPreviousSet(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	if err := store.SaveCharacterSkills(ctx, map[int32]int32{16622: 3}, time.Now()); err != nil {
		t.Fatalf("SaveCharacterSkills(first): %v", err)
	}
	if err := store.SaveCharacterSkills(ctx, map[int32]int32{16622: 5}, time.Now()); err != nil {
		t.Fatalf("SaveCharacterSkills(second): %v", err)
	}

	levels, _, _, err := store.LoadCharacterSkills(ctx)
	if err != nil {
		t.Fatalf("LoadCharacterSkills: %v", err)
	}
	if len(levels) != 1 || levels[16622] != 5 {
		t.Errorf("levels = %v, want {16622:5} (replaced, not accumulated)", levels)
	}
}

func TestSaveAndLoadCharacterStandings(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	fetchedAt := time.Now().UTC().Truncate(time.Second)
	if err := store.SaveCharacterStandings(ctx, map[int32]float64{1000035: 5.0, 500001: 2.0}, fetchedAt); err != nil {
		t.Fatalf("SaveCharacterStandings: %v", err)
	}

	standings, got, ok, err := store.LoadCharacterStandings(ctx)
	if err != nil {
		t.Fatalf("LoadCharacterStandings: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if standings[1000035] != 5.0 || standings[500001] != 2.0 {
		t.Errorf("standings = %v, want {1000035:5.0, 500001:2.0}", standings)
	}
	if !got.Equal(fetchedAt) {
		t.Errorf("fetchedAt = %v, want %v", got, fetchedAt)
	}
}

func TestLoadCharacterStandingsWhenNoneSaved(t *testing.T) {
	_, _, ok, err := openBootstrapped(t).LoadCharacterStandings(context.Background())
	if err != nil {
		t.Fatalf("LoadCharacterStandings: %v", err)
	}
	if ok {
		t.Error("ok = true, want false")
	}
}

func TestScanCacheMetaRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	_, ok, err := store.LoadScanCacheMeta(ctx, "Jita")
	if err != nil {
		t.Fatalf("LoadScanCacheMeta (before save): %v", err)
	}
	if ok {
		t.Fatal("ok = true before any save, want false")
	}

	ordersAt := time.Now().Add(-time.Minute).UTC().Truncate(time.Second)
	volumeAt := time.Now().Add(-time.Hour).UTC().Truncate(time.Second)
	want := ScanCacheMeta{Hub: "Jita", OrdersFetchedAt: ordersAt, VolumeFetchedAt: volumeAt}
	if err := store.SaveScanCacheMeta(ctx, want); err != nil {
		t.Fatalf("SaveScanCacheMeta: %v", err)
	}

	got, ok, err := store.LoadScanCacheMeta(ctx, "Jita")
	if err != nil {
		t.Fatalf("LoadScanCacheMeta: %v", err)
	}
	if !ok {
		t.Fatal("ok = false, want true")
	}
	if !got.OrdersFetchedAt.Equal(want.OrdersFetchedAt) || !got.VolumeFetchedAt.Equal(want.VolumeFetchedAt) {
		t.Errorf("got = %+v, want %+v", got, want)
	}

	// A different Hub's meta must be independent.
	if _, ok, err := store.LoadScanCacheMeta(ctx, "Rens"); err != nil {
		t.Fatalf("LoadScanCacheMeta(Rens): %v", err)
	} else if ok {
		t.Error("Rens meta exists after only saving Jita's")
	}
}

func TestScanCacheMetaUpdatesOnlyGivenFields(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	t1 := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	if err := store.SaveScanCacheMeta(ctx, ScanCacheMeta{Hub: "Jita", OrdersFetchedAt: t1, VolumeFetchedAt: t1}); err != nil {
		t.Fatalf("SaveScanCacheMeta(first): %v", err)
	}

	t2 := time.Now().UTC().Truncate(time.Second)
	if err := store.SaveScanCacheMeta(ctx, ScanCacheMeta{Hub: "Jita", OrdersFetchedAt: t2, VolumeFetchedAt: t1}); err != nil {
		t.Fatalf("SaveScanCacheMeta(second): %v", err)
	}

	got, _, err := store.LoadScanCacheMeta(ctx, "Jita")
	if err != nil {
		t.Fatalf("LoadScanCacheMeta: %v", err)
	}
	if !got.OrdersFetchedAt.Equal(t2) {
		t.Errorf("OrdersFetchedAt = %v, want %v (updated)", got.OrdersFetchedAt, t2)
	}
	if !got.VolumeFetchedAt.Equal(t1) {
		t.Errorf("VolumeFetchedAt = %v, want %v (unchanged)", got.VolumeFetchedAt, t1)
	}
}

func f64ptr(v float64) *float64 { return &v }

func TestSaveOpportunityPricesThenLoad(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	prices := map[int32]OpportunityPrice{
		34: {BestBuy: f64ptr(5.10), BestSell: f64ptr(5.45)},
		35: {BestBuy: nil, BestSell: f64ptr(11.0)},
	}
	if err := store.SaveOpportunityPrices(ctx, "Jita", prices); err != nil {
		t.Fatalf("SaveOpportunityPrices: %v", err)
	}

	got, err := store.LoadOpportunityCache(ctx, "Jita")
	if err != nil {
		t.Fatalf("LoadOpportunityCache: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[34].BestBuy == nil || *got[34].BestBuy != 5.10 {
		t.Errorf("got[34].BestBuy = %v, want 5.10", got[34].BestBuy)
	}
	if got[35].BestBuy != nil {
		t.Errorf("got[35].BestBuy = %v, want nil (no buy orders)", *got[35].BestBuy)
	}
}

func TestSaveOpportunityVolumesDoesNotClobberPrices(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	prices := map[int32]OpportunityPrice{34: {BestBuy: f64ptr(5.10), BestSell: f64ptr(5.45)}}
	if err := store.SaveOpportunityPrices(ctx, "Jita", prices); err != nil {
		t.Fatalf("SaveOpportunityPrices: %v", err)
	}
	if err := store.SaveOpportunityVolumes(ctx, "Jita", map[int32]float64{34: 12345.6}); err != nil {
		t.Fatalf("SaveOpportunityVolumes: %v", err)
	}

	got, err := store.LoadOpportunityCache(ctx, "Jita")
	if err != nil {
		t.Fatalf("LoadOpportunityCache: %v", err)
	}
	entry := got[34]
	if entry.BestBuy == nil || *entry.BestBuy != 5.10 {
		t.Errorf("BestBuy = %v, want still 5.10 (volume save must not clobber prices)", entry.BestBuy)
	}
	if entry.AvgVolume == nil || *entry.AvgVolume != 12345.6 {
		t.Errorf("AvgVolume = %v, want 12345.6", entry.AvgVolume)
	}
}

func TestSavePricesDoesNotClobberVolume(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	if err := store.SaveOpportunityVolumes(ctx, "Jita", map[int32]float64{34: 999}); err != nil {
		t.Fatalf("SaveOpportunityVolumes: %v", err)
	}
	if err := store.SaveOpportunityPrices(ctx, "Jita", map[int32]OpportunityPrice{34: {BestBuy: f64ptr(1), BestSell: f64ptr(2)}}); err != nil {
		t.Fatalf("SaveOpportunityPrices: %v", err)
	}

	got, err := store.LoadOpportunityCache(ctx, "Jita")
	if err != nil {
		t.Fatalf("LoadOpportunityCache: %v", err)
	}
	if got[34].AvgVolume == nil || *got[34].AvgVolume != 999 {
		t.Errorf("AvgVolume = %v, want still 999 (price save must not clobber volume)", got[34].AvgVolume)
	}
}

func TestOpportunityCacheIsScopedPerHub(t *testing.T) {
	ctx := context.Background()
	store := openBootstrapped(t)

	if err := store.SaveOpportunityPrices(ctx, "Jita", map[int32]OpportunityPrice{34: {BestBuy: f64ptr(1), BestSell: f64ptr(2)}}); err != nil {
		t.Fatalf("SaveOpportunityPrices: %v", err)
	}

	got, err := store.LoadOpportunityCache(ctx, "Rens")
	if err != nil {
		t.Fatalf("LoadOpportunityCache(Rens): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Rens cache = %v, want empty (Jita's save must not leak into Rens)", got)
	}
}
