package scanner

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
	"github.com/mgoodness/eve-trader/internal/storage"
)

func openStore(t *testing.T) *storage.Store {
	t.Helper()

	store, err := storage.Open(filepath.Join(t.TempDir(), "eve-trader.db"))
	if err != nil {
		t.Fatalf("storage.Open: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	if err := store.Bootstrap(context.Background()); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	return store
}

func jitaMarketOrders(sellPrice, buyPrice float64) []esi.MarketOrder {
	return []esi.MarketOrder{
		{OrderID: 1, TypeID: 34, LocationID: hub.Jita.StationID, IsBuyOrder: false, Price: sellPrice},
		{OrderID: 2, TypeID: 34, LocationID: hub.Jita.StationID, IsBuyOrder: true, Price: buyPrice},
	}
}

func TestScanComputesFeeAdjustedMargin(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	fake := esi.NewFakeClient()
	fake.MarketOrdersResp = jitaMarketOrders(110, 100)
	fake.MarketHistoryResp = []esi.MarketHistoryEntry{{Date: time.Now(), Volume: 1000}}
	// No broker relations / accounting / standings -> base rates: 3% broker fee, 7.5% sales tax.
	fake.SkillsResp = esi.Skills{}
	fake.StandingsResp = nil

	opps, err := Scan(ctx, fake, store, hub.Jita, 95465499, time.Now())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var got *Opportunity
	for i := range opps {
		if opps[i].TypeID == 34 {
			got = &opps[i]
		}
	}
	if got == nil {
		t.Fatalf("no Opportunity for type 34 in %+v", opps)
	}

	wantMargin := Margin(100, 110, 3.0, 7.5)
	if !almostEqual(got.Margin, wantMargin) {
		t.Errorf("Margin = %v, want %v (base fee rates)", got.Margin, wantMargin)
	}
	if got.BestBuy != 100 || got.BestSell != 110 {
		t.Errorf("BestBuy/BestSell = %v/%v, want 100/110", got.BestBuy, got.BestSell)
	}
	if got.AvgDailyVolume != 1000 {
		t.Errorf("AvgDailyVolume = %v, want 1000", got.AvgDailyVolume)
	}
}

// TestScanMarginVariesWithFeeInputs is the AC-mandated test: Margin
// varies with fee-rate variation from fake skills/standings data.
func TestScanMarginVariesWithFeeInputs(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	runScan := func(skills esi.Skills, standings []esi.Standing) float64 {
		store := openStore(t)
		fake := esi.NewFakeClient()
		fake.MarketOrdersResp = jitaMarketOrders(110, 100)
		fake.MarketHistoryResp = []esi.MarketHistoryEntry{{Date: now, Volume: 1000}}
		fake.SkillsResp = skills
		fake.StandingsResp = standings

		opps, err := Scan(ctx, fake, store, hub.Jita, 95465499, now)
		if err != nil {
			t.Fatalf("Scan: %v", err)
		}
		for _, o := range opps {
			if o.TypeID == 34 {
				return o.Margin
			}
		}
		t.Fatal("no Opportunity for type 34")
		return 0
	}

	untrainedMargin := runScan(esi.Skills{}, nil)
	trainedMargin := runScan(
		esi.Skills{Skills: []esi.Skill{
			{SkillID: AccountingSkillID, ActiveSkillLevel: 5},
			{SkillID: BrokerRelationsSkillID, ActiveSkillLevel: 5},
		}},
		[]esi.Standing{
			{FromID: hub.Jita.OwnerFactionID, Standing: 10},
			{FromID: hub.Jita.OwnerCorpID, Standing: 10},
		},
	)

	if trainedMargin <= untrainedMargin {
		t.Errorf("trainedMargin = %v, want greater than untrainedMargin = %v (lower fees -> higher margin)", trainedMargin, untrainedMargin)
	}
}

func TestScanReusesCachedPricesWithinOrderTTL(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	fake := esi.NewFakeClient()
	fake.MarketOrdersResp = jitaMarketOrders(110, 100)
	fake.MarketHistoryResp = []esi.MarketHistoryEntry{{Date: time.Now(), Volume: 1000}}

	t1 := time.Now()
	if _, err := Scan(ctx, fake, store, hub.Jita, 1, t1); err != nil {
		t.Fatalf("Scan (t1): %v", err)
	}

	// Prices "move" in the underlying market, but well within the 5-min
	// order-book cache window Scan should still report the old, cached
	// numbers rather than re-fetching.
	fake.MarketOrdersResp = jitaMarketOrders(999, 1)
	t2 := t1.Add(1 * time.Minute)
	opps, err := Scan(ctx, fake, store, hub.Jita, 1, t2)
	if err != nil {
		t.Fatalf("Scan (t2): %v", err)
	}
	for _, o := range opps {
		if o.TypeID == 34 && o.BestSell != 110 {
			t.Errorf("BestSell = %v, want still 110 (cached, not re-fetched within 5 min)", o.BestSell)
		}
	}

	// Past the 5-min window: Scan should refresh and pick up the new price.
	t3 := t1.Add(6 * time.Minute)
	opps, err = Scan(ctx, fake, store, hub.Jita, 1, t3)
	if err != nil {
		t.Fatalf("Scan (t3): %v", err)
	}
	found := false
	for _, o := range opps {
		if o.TypeID == 34 {
			found = true
			if o.BestSell != 999 {
				t.Errorf("BestSell = %v, want 999 (refreshed after 5 min)", o.BestSell)
			}
		}
	}
	if !found {
		t.Fatal("no Opportunity for type 34 after refresh")
	}
}

func TestScanReusesCachedVolumeWithin24hEvenAfterOrdersRefresh(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	fake := esi.NewFakeClient()
	fake.MarketOrdersResp = jitaMarketOrders(110, 100)
	fake.MarketHistoryResp = []esi.MarketHistoryEntry{{Date: time.Now(), Volume: 1000}}

	t1 := time.Now()
	if _, err := Scan(ctx, fake, store, hub.Jita, 1, t1); err != nil {
		t.Fatalf("Scan (t1): %v", err)
	}

	// Volume "changes" and prices move too, but only the 5-min order
	// window has elapsed -- volume (24h window) must stay cached.
	fake.MarketHistoryResp = []esi.MarketHistoryEntry{{Date: t1, Volume: 999999}}
	fake.MarketOrdersResp = jitaMarketOrders(120, 90)
	t2 := t1.Add(6 * time.Minute)
	opps, err := Scan(ctx, fake, store, hub.Jita, 1, t2)
	if err != nil {
		t.Fatalf("Scan (t2): %v", err)
	}
	for _, o := range opps {
		if o.TypeID == 34 {
			if o.AvgDailyVolume != 1000 {
				t.Errorf("AvgDailyVolume = %v, want still 1000 (cached, not re-fetched within 24h)", o.AvgDailyVolume)
			}
			if o.BestSell != 120 {
				t.Errorf("BestSell = %v, want 120 (order book refreshed independently of volume)", o.BestSell)
			}
		}
	}
}

func TestScanIsIndependentPerHub(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	fake := esi.NewFakeClient()
	fake.MarketHistoryResp = []esi.MarketHistoryEntry{{Date: time.Now(), Volume: 1000}}

	fake.MarketOrdersResp = jitaMarketOrders(110, 100)
	jitaOpps, err := Scan(ctx, fake, store, hub.Jita, 1, time.Now())
	if err != nil {
		t.Fatalf("Scan(Jita): %v", err)
	}

	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 3, TypeID: 34, LocationID: hub.Rens.StationID, IsBuyOrder: false, Price: 60},
		{OrderID: 4, TypeID: 34, LocationID: hub.Rens.StationID, IsBuyOrder: true, Price: 50},
	}
	rensOpps, err := Scan(ctx, fake, store, hub.Rens, 1, time.Now())
	if err != nil {
		t.Fatalf("Scan(Rens): %v", err)
	}

	if len(jitaOpps) == 0 || jitaOpps[0].BestSell != 110 {
		t.Errorf("jitaOpps = %+v, want BestSell 110", jitaOpps)
	}
	if len(rensOpps) == 0 || rensOpps[0].BestSell != 60 {
		t.Errorf("rensOpps = %+v, want BestSell 60 (independent of Jita)", rensOpps)
	}
}

func TestScanSkipsItemsWithOnlyOneSidedMarket(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	fake := esi.NewFakeClient()
	fake.MarketOrdersResp = []esi.MarketOrder{
		{OrderID: 1, TypeID: 34, LocationID: hub.Jita.StationID, IsBuyOrder: false, Price: 110}, // sell only, no buy
	}
	fake.MarketHistoryResp = nil

	opps, err := Scan(ctx, fake, store, hub.Jita, 1, time.Now())
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	for _, o := range opps {
		if o.TypeID == 34 {
			t.Errorf("got an Opportunity for a one-sided market: %+v", o)
		}
	}
}
