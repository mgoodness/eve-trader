package ledger_test

import (
	"database/sql"
	"math"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/dbtest"
	"github.com/mgoodness/eve-trader/ledger"
)

const rensLocation = 60004588

// seedTx inserts a wallet_transaction row. isBuy is stored 0/1.
func seedTx(t *testing.T, db *sql.DB, txID int64, date time.Time, typeID, quantity int, unitPrice float64, isBuy bool, locationID int64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO wallet_transaction (transaction_id, date, type_id, quantity, unit_price, is_buy, is_personal, journal_ref_id, location_id, client_id)
		VALUES (?, ?, ?, ?, ?, ?, 1, 0, ?, 0)`,
		txID, date.UTC().Format(time.RFC3339Nano), typeID, quantity, unitPrice, boolToInt(isBuy), locationID,
	); err != nil {
		t.Fatalf("seeding wallet_transaction %d: %v", txID, err)
	}
}

// seedJournal inserts wallet_journal rows. amounts are the ESI amounts:
// expenses (fees, tax) are negative.
func seedJournal(t *testing.T, db *sql.DB, entries ...[2]any) {
	t.Helper()
	for i, e := range entries {
		id := int64(1000 + i)
		refType := e[0].(string)
		amount := e[1].(float64)
		if _, err := db.Exec(`
			INSERT INTO wallet_journal (id, date, ref_type, amount, balance, description)
			VALUES (?, '2024-01-01T00:00:00Z', ?, ?, 0, '')`,
			id, refType, amount,
		); err != nil {
			t.Fatalf("seeding wallet_journal %d: %v", id, err)
		}
	}
}

// seedOrderSnapshot inserts one (order_id, issued) character_order row.
func seedOrderSnapshot(t *testing.T, db *sql.DB, orderID int64, issued time.Time, typeID int, locationID int64, isBuy bool, price float64, volumeRemain, volumeTotal int, state string) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO character_order (order_id, issued, type_id, location_id, is_buy_order, price, volume_remain, volume_total,
			min_volume, duration, state, is_corporation, region_id, order_range, escrow, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, 90, ?, 0, 0, 'station', 0, '2024-01-01T00:00:00Z')`,
		orderID, issued.UTC().Format(time.RFC3339Nano), typeID, locationID, boolToInt(isBuy), price, volumeRemain, volumeTotal, state,
	); err != nil {
		t.Fatalf("seeding character_order %d: %v", orderID, err)
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// pnlFixture is a self-contained ledger seed plus the figures it should
// produce, so each scenario reads as data (docs/spec/v2.md §8).
type pnlFixture struct {
	name          string
	seed          func(t *testing.T, db *sql.DB)
	wantReport    ledger.Report
	wantPositions map[int]ledger.Position // by type_id
}

func TestComputePnL(t *testing.T) {
	for _, f := range []pnlFixture{buySellFixture(), modifyFixture(), transferFixture(), sunkFixture(), vanishedOrderFixture(), pendingFixture()} {
		t.Run(f.name, func(t *testing.T) {
			db := dbtest.OpenDB(t)
			f.seed(t, db)
			got, err := ledger.ComputePnL(t.Context(), db, 123)
			if err != nil {
				t.Fatalf("ComputePnL() error = %v", err)
			}
			assertReport(t, got, f.wantReport)
			for typeID, want := range f.wantPositions {
				pos, ok := positionByType(got.Positions, typeID)
				if !ok {
					t.Fatalf("no position for type %d; got %+v", typeID, got.Positions)
				}
				assertPosition(t, pos, want)
			}
		})
	}
}

// buySellFixture: 1000 bought at 100, 400 sold at 150, with matching
// placement-fee orders and a Rens book. R_b = 3%, R_t = 7.5%.
func buySellFixture() pnlFixture {
	day1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)
	return pnlFixture{
		name: "buy and sell at weighted-average cost",
		seed: func(t *testing.T, db *sql.DB) {
			t.Helper()
			dbtest.SeedItem(t, db, 34, "Tritanium")
			dbtest.SeedSkills(t, db, 123, 0, 0)
			seedTx(t, db, 1, day1, 34, 1000, 100, true, rensLocation)
			seedTx(t, db, 2, day2, 34, 400, 150, false, rensLocation)
			seedOrderSnapshot(t, db, 1, day1, 34, rensLocation, true, 100, 0, 1000, "")
			seedOrderSnapshot(t, db, 2, day2, 34, rensLocation, false, 150, 0, 400, "")
			seedJournal(t, db, [2]any{"brokers_fee", -3000.0}, [2]any{"brokers_fee", -1800.0}, [2]any{"transaction_tax", -4500.0})
			dbtest.SeedOrder(t, db, 9001, 34, true, 140)
			dbtest.SeedOrder(t, db, 9002, 34, false, 160)
		},
		wantReport: ledger.Report{
			Realized:      12500,
			Unrealized:    24120,
			EstimatedFees: 9300,
			JournalFees:   9300,
			SunkFees:      0,
		},
		wantPositions: map[int]ledger.Position{
			34: {
				TypeID: 34, LocationID: rensLocation, Quantity: 600, AverageCost: 103,
				CostBasis: 61800, Realized: 12500, Unrealized: 24120,
				HasMarket: true, MarketSell: 160, LiquidationBuy: 140,
				EstimatedFees: 9300,
			},
		},
	}
}

// modifyFixture: an observed in-place modify on a partially filled,
// cancelled sell order. R_b = 3%, ABR = 5 (modify fraction 20%), R_t = 7.5%.
// The journal carries an extra broker fee (an unobserved modify), which
// must land in the unattributed bucket.
func modifyFixture() pnlFixture {
	day1 := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC)
	day1Fill := time.Date(2024, 1, 1, 13, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 1, 2, 12, 0, 0, 0, time.UTC)
	return pnlFixture{
		name: "in-place modify, sunk remainder, unattributed fee",
		seed: func(t *testing.T, db *sql.DB) {
			t.Helper()
			dbtest.SeedItem(t, db, 35, "Pyerite")
			dbtest.SeedSkillsWithAdvancedBrokerRelations(t, db, 123, 0, 0, 5)
			// Buy 10000 @ 8 with a matching order; cost basis 82400, avg 8.24.
			seedTx(t, db, 3, day1, 35, 10000, 8, true, rensLocation)
			seedOrderSnapshot(t, db, 3, day1, 35, rensLocation, true, 8, 0, 10000, "")
			// Sell 6000 @ 10 filled before the modify.
			seedTx(t, db, 4, day1Fill, 35, 6000, 10, false, rensLocation)
			// Sell order: placed at 10 (fee 3000), repriced to 12 with 4000
			// remaining (modify fee 528), then cancelled — 4000 units unfilled.
			seedOrderSnapshot(t, db, 10, day1, 35, rensLocation, false, 10, 10000, 10000, "")
			seedOrderSnapshot(t, db, 10, day2, 35, rensLocation, false, 12, 4000, 10000, "cancelled")
			// Journal: buy 2400, placement 3000, modify 528, an unobserved
			// modify 999, and sales tax 4500.
			seedJournal(t, db,
				[2]any{"brokers_fee", -2400.0},
				[2]any{"brokers_fee", -3000.0},
				[2]any{"brokers_fee", -528.0},
				[2]any{"brokers_fee", -999.0},
				[2]any{"transaction_tax", -4500.0},
			)
		},
		wantReport: ledger.Report{
			Realized:         3943.2,
			EstimatedFees:    9016.8,
			JournalFees:      11427,
			SunkFees:         1411.2,
			UnattributedFees: 999,
		},
		wantPositions: map[int]ledger.Position{
			35: {
				TypeID: 35, LocationID: rensLocation, Quantity: 4000, AverageCost: 8.24,
				CostBasis: 32960, Realized: 3943.2, HasMarket: false,
				EstimatedFees: 9016.8, SellRelists: 1,
			},
		},
	}
}

// transferFixture: a priced manual transfer (gain reported separately from
// trading P/L) and a zero-price contract transfer (valued at cost, no gain).
func transferFixture() pnlFixture {
	day1 := time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 2, 2, 10, 0, 0, 0, time.UTC)
	day3 := time.Date(2024, 2, 3, 10, 0, 0, 0, time.UTC)
	return pnlFixture{
		name: "priced and cost-valued transfers",
		seed: func(t *testing.T, db *sql.DB) {
			t.Helper()
			dbtest.SeedItem(t, db, 36, "Nocxium")
			dbtest.SeedItem(t, db, 37, "Isogen")
			dbtest.SeedSkills(t, db, 123, 0, 0)

			// Priced manual transfer: buy 1000 @ 10 (fee 300, avg 10.30),
			// transfer 200 for 3000 ISK.
			seedTx(t, db, 5, day1, 36, 1000, 10, true, rensLocation)
			seedOrderSnapshot(t, db, 5, day1, 36, rensLocation, true, 10, 0, 1000, "")
			if _, err := db.Exec(`
				INSERT INTO manual_transfer (type_id, location_id, quantity, date, price, created_at)
				VALUES (36, ?, 200, ?, 3000, '2024-02-02T00:00:00Z')`,
				rensLocation, day2.UTC().Format(time.RFC3339Nano),
			); err != nil {
				t.Fatal(err)
			}

			// Cost-valued contract transfer: buy 500 @ 20 (no order snapshot,
			// so the fee falls back to max(100, 300) = 300), then a finished
			// item-exchange contract hands off 100 units at zero price.
			seedTx(t, db, 6, day1, 37, 500, 20, true, rensLocation)
			seedContract(t, db, 7001, 123, 900, "item_exchange", 0, day3)
			seedContractItem(t, db, 7001, 1, 37, 100, true)

			seedJournal(t, db, [2]any{"brokers_fee", -300.0}, [2]any{"brokers_fee", -300.0})
		},
		wantReport: ledger.Report{
			Realized:         0,
			EstimatedFees:    600,
			JournalFees:      600,
			TransferGainLoss: 940,
		},
		wantPositions: map[int]ledger.Position{
			36: {
				TypeID: 36, LocationID: rensLocation, Quantity: 800, AverageCost: 10.3,
				CostBasis: 8240, EstimatedFees: 300,
				TransferGainLoss: 940, TransferredQuantity: 200,
			},
			37: {
				TypeID: 37, LocationID: rensLocation, Quantity: 400, AverageCost: 20.6,
				CostBasis: 8240, EstimatedFees: 300,
				TransferGainLoss: 0, TransferredQuantity: 100,
			},
		},
	}
}

// sunkFixture: a cancelled order that never filled. Its whole broker fee is
// sunk, not attributed to any position (docs/spec/v2.md §4.4).
func sunkFixture() pnlFixture {
	day1 := time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)
	return pnlFixture{
		name: "zero-fill cancelled order goes to sunk",
		seed: func(t *testing.T, db *sql.DB) {
			t.Helper()
			dbtest.SeedItem(t, db, 38, "Mexallon")
			dbtest.SeedSkills(t, db, 123, 0, 0)
			seedOrderSnapshot(t, db, 20, day1, 38, rensLocation, true, 10, 1000, 1000, "cancelled")
			seedJournal(t, db, [2]any{"brokers_fee", -300.0})
		},
		wantReport: ledger.Report{
			JournalFees: 300,
			SunkFees:    300,
		},
		wantPositions: map[int]ledger.Position{},
	}
}

// vanishedOrderFixture: fully-filled orders vanish from the order routes, so
// their fee is estimated per fill from the placement formula.
func vanishedOrderFixture() pnlFixture {
	day1 := time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)
	day2 := time.Date(2024, 3, 2, 10, 0, 0, 0, time.UTC)
	return pnlFixture{
		name: "vanished fully-filled orders fall back to per-fill fees",
		seed: func(t *testing.T, db *sql.DB) {
			t.Helper()
			dbtest.SeedItem(t, db, 39, "Morphite")
			dbtest.SeedSkills(t, db, 123, 0, 0)
			seedTx(t, db, 7, day1, 39, 1000, 10, true, rensLocation)
			seedTx(t, db, 8, day2, 39, 500, 15, false, rensLocation)
			seedJournal(t, db,
				[2]any{"brokers_fee", -300.0},
				[2]any{"brokers_fee", -225.0},
				[2]any{"transaction_tax", -562.5},
			)
		},
		wantReport: ledger.Report{
			Realized:      1562.5,
			EstimatedFees: 1087.5,
			JournalFees:   1087.5,
		},
		wantPositions: map[int]ledger.Position{
			39: {
				TypeID: 39, LocationID: rensLocation, Quantity: 500, AverageCost: 10.3,
				CostBasis: 5150, Realized: 1562.5, EstimatedFees: 1087.5,
			},
		},
	}
}

// pendingFixture: an open, partially-filled buy order. The unfilled half's
// placement fee is pending -- charged cash but not yet in cost basis -- not
// unattributed (docs/spec/v2.md §4.4).
func pendingFixture() pnlFixture {
	day1 := time.Date(2024, 4, 1, 10, 0, 0, 0, time.UTC)
	return pnlFixture{
		name: "open partially-filled order fee is pending",
		seed: func(t *testing.T, db *sql.DB) {
			t.Helper()
			dbtest.SeedItem(t, db, 40, "Tritanium")
			dbtest.SeedSkills(t, db, 123, 0, 0)
			seedTx(t, db, 9, day1, 40, 1000, 10, true, rensLocation)
			seedOrderSnapshot(t, db, 30, day1, 40, rensLocation, true, 10, 1000, 2000, "")
			seedJournal(t, db, [2]any{"brokers_fee", -600.0})
		},
		wantReport: ledger.Report{
			EstimatedFees: 300,
			PendingFees:   300,
			JournalFees:   600,
		},
		wantPositions: map[int]ledger.Position{
			40: {
				TypeID: 40, LocationID: rensLocation, Quantity: 1000, AverageCost: 10.3,
				CostBasis: 10300, EstimatedFees: 300,
			},
		},
	}
}

func positionByType(positions []ledger.Position, typeID int) (ledger.Position, bool) {
	for _, p := range positions {
		if p.TypeID == typeID {
			return p, true
		}
	}
	return ledger.Position{}, false
}

func assertReport(t *testing.T, got, want ledger.Report) {
	t.Helper()
	for name, pair := range map[string][2]float64{
		"Realized":         {got.Realized, want.Realized},
		"Unrealized":       {got.Unrealized, want.Unrealized},
		"EstimatedFees":    {got.EstimatedFees, want.EstimatedFees},
		"UnattributedFees": {got.UnattributedFees, want.UnattributedFees},
		"SunkFees":         {got.SunkFees, want.SunkFees},
		"PendingFees":      {got.PendingFees, want.PendingFees},
		"JournalFees":      {got.JournalFees, want.JournalFees},
		"TransferGainLoss": {got.TransferGainLoss, want.TransferGainLoss},
	} {
		if !almostEqual(pair[0], pair[1]) {
			t.Errorf("Report.%s = %v, want %v", name, pair[0], pair[1])
		}
	}
}

func assertPosition(t *testing.T, got, want ledger.Position) {
	t.Helper()
	if got.TypeID != want.TypeID || got.LocationID != want.LocationID {
		t.Errorf("position identity = (%d, %d), want (%d, %d)", got.TypeID, got.LocationID, want.TypeID, want.LocationID)
	}
	for name, pair := range map[string][2]float64{
		"AverageCost":      {got.AverageCost, want.AverageCost},
		"CostBasis":        {got.CostBasis, want.CostBasis},
		"Realized":         {got.Realized, want.Realized},
		"Unrealized":       {got.Unrealized, want.Unrealized},
		"MarketSell":       {got.MarketSell, want.MarketSell},
		"LiquidationBuy":   {got.LiquidationBuy, want.LiquidationBuy},
		"EstimatedFees":    {got.EstimatedFees, want.EstimatedFees},
		"TransferGainLoss": {got.TransferGainLoss, want.TransferGainLoss},
	} {
		if !almostEqual(pair[0], pair[1]) {
			t.Errorf("Position[%d].%s = %v, want %v", got.TypeID, name, pair[0], pair[1])
		}
	}
	for name, pair := range map[string][2]int{
		"Quantity":            {got.Quantity, want.Quantity},
		"TransferredQuantity": {got.TransferredQuantity, want.TransferredQuantity},
		"BuyRelists":          {got.BuyRelists, want.BuyRelists},
		"SellRelists":         {got.SellRelists, want.SellRelists},
	} {
		if pair[0] != pair[1] {
			t.Errorf("Position[%d].%s = %d, want %d", got.TypeID, name, pair[0], pair[1])
		}
	}
	if got.HasMarket != want.HasMarket {
		t.Errorf("Position[%d].HasMarket = %v, want %v", got.TypeID, got.HasMarket, want.HasMarket)
	}
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-6
}
