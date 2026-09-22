// Package dbtest holds the shared seeding helpers used by every package's
// black-box tests against a real SQLite database (the #11 test-harness
// convention). Kept under internal/ since it's test-support code, not
// part of eve-trader's public surface.
package dbtest

import (
	"database/sql"
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/db"
	"github.com/mgoodness/eve-trader/internal/tokencrypt"
)

// OpenDB opens a fresh in-memory SQLite database with the v1 schema
// applied, closing it automatically at test cleanup.
func OpenDB(t *testing.T) *sql.DB {
	t.Helper()
	sqlDB, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("db.Open() error = %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return sqlDB
}

// SeedItem inserts an item_type row.
func SeedItem(t *testing.T, sqlDB *sql.DB, typeID int, name string) {
	t.Helper()
	if _, err := sqlDB.Exec(
		`INSERT INTO item_type (type_id, name, updated_at) VALUES (?, ?, '2024-01-01T00:00:00Z')`,
		typeID, name,
	); err != nil {
		t.Fatalf("seeding item_type %d: %v", typeID, err)
	}
}

// RensStationID is the station the character trades at, and the location
// SeedOrder and SeedBook place their seeded orders at. The region-wide book
// these helpers write to spans every station, but most fixtures only care
// about the Rens-anchored consumers, so the Rens value is the default.
const RensStationID = 60004588

// SeedOrder inserts a market_order row for typeID at Rens.
func SeedOrder(t *testing.T, sqlDB *sql.DB, orderID int64, typeID int, isBuy bool, price float64) {
	t.Helper()
	SeedOrderAt(t, sqlDB, orderID, typeID, RensStationID, isBuy, price)
}

// SeedOrderAt inserts a market_order row for typeID at an explicit station,
// letting a fixture mix region stations into the same book.
func SeedOrderAt(t *testing.T, sqlDB *sql.DB, orderID int64, typeID int, locationID int64, isBuy bool, price float64) {
	t.Helper()
	if _, err := sqlDB.Exec(
		`INSERT INTO market_order (order_id, type_id, location_id, is_buy_order, price, volume_remain, volume_total, min_volume, issued, duration, updated_at)
		 VALUES (?, ?, ?, ?, ?, 1000, 1000, 1, '2024-01-01T00:00:00Z', 90, '2024-01-01T00:00:00Z')`,
		orderID, typeID, locationID, isBuy, price,
	); err != nil {
		t.Fatalf("seeding market_order %d: %v", orderID, err)
	}
}

// SeedBook seeds two orders per side for typeID, with the second order on
// each side inside the always-on realism filter's 5% near-best band, so the
// seeded spread is not a single-order spread. buy and sell are the best
// (highest buy, lowest sell) prices; the extras sit just behind them.
func SeedBook(t *testing.T, sqlDB *sql.DB, typeID int, buy, sell float64) {
	t.Helper()
	SeedOrder(t, sqlDB, int64(typeID*10), typeID, true, buy)
	SeedOrder(t, sqlDB, int64(typeID*10+1), typeID, true, buy*0.99)
	SeedOrder(t, sqlDB, int64(typeID*10+2), typeID, false, sell)
	SeedOrder(t, sqlDB, int64(typeID*10+3), typeID, false, sell*1.01)
}

// HistoryDay describes one market_history row to seed. A zero Date gets
// the next consecutive date in the fixture's sequence (starting
// 2024-01-01). NullPrices seeds NULL average/highest/lowest, mimicking a
// row written before the price-fields migration and re-fetch.
//
// OrderCount defaults to 0 (a day with no trades); callers that want a
// trade-day set it explicitly.
type HistoryDay struct {
	Date       string
	Volume     int
	OrderCount int
	Average    float64
	Highest    float64
	Lowest     float64
	NullPrices bool
}

// SeedHistoryDays inserts one market_history row per day, generating
// consecutive dates for any day whose Date is blank.
func SeedHistoryDays(t *testing.T, sqlDB *sql.DB, typeID int, days ...HistoryDay) {
	t.Helper()
	for i, day := range days {
		date := day.Date
		if date == "" {
			date = time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i).Format("2006-01-02")
		}
		if day.NullPrices {
			if _, err := sqlDB.Exec(
				`INSERT INTO market_history (type_id, date, volume, order_count) VALUES (?, ?, ?, ?)`,
				typeID, date, day.Volume, day.OrderCount,
			); err != nil {
				t.Fatalf("seeding unpriced market_history for type %d: %v", typeID, err)
			}
			continue
		}
		if _, err := sqlDB.Exec(
			`INSERT INTO market_history (type_id, date, volume, order_count, average, highest, lowest) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			typeID, date, day.Volume, day.OrderCount, day.Average, day.Highest, day.Lowest,
		); err != nil {
			t.Fatalf("seeding market_history for type %d: %v", typeID, err)
		}
	}
}

// SeedHistory inserts one priced, fully-traded market_history day per
// volume, on consecutive early-January 2024 dates. Prices are flat
// (average=highest=lowest=1) so the default fixture clears the realism
// filters; tests that care about the price fields use SeedHistoryDays.
func SeedHistory(t *testing.T, sqlDB *sql.DB, typeID int, volumes ...int) {
	t.Helper()
	days := make([]HistoryDay, len(volumes))
	for i, v := range volumes {
		days[i] = HistoryDay{Volume: v, OrderCount: 5, Average: 1, Highest: 1, Lowest: 1}
	}
	SeedHistoryDays(t, sqlDB, typeID, days...)
}

// SeedSkills inserts the single character_skill row with Advanced Broker
// Relations at level 0.
func SeedSkills(t *testing.T, sqlDB *sql.DB, characterID, brokerRelations, accounting int) {
	t.Helper()
	SeedSkillsWithAdvancedBrokerRelations(t, sqlDB, characterID, brokerRelations, accounting, 0)
}

// SeedSkillsWithAdvancedBrokerRelations inserts the single character_skill
// row including the in-place re-list fee skill (Advanced Broker Relations).
func SeedSkillsWithAdvancedBrokerRelations(t *testing.T, sqlDB *sql.DB, characterID, brokerRelations, accounting, advancedBrokerRelations int) {
	t.Helper()
	if _, err := sqlDB.Exec(
		`INSERT INTO character_skill (character_id, broker_relations_level, accounting_level, advanced_broker_relations_level, updated_at)
		 VALUES (?, ?, ?, ?, '2024-01-01T00:00:00Z')`,
		characterID, brokerRelations, accounting, advancedBrokerRelations,
	); err != nil {
		t.Fatalf("seeding character_skill: %v", err)
	}
}

// SeedStanding inserts one character_standing row with the unmodified
// standing (ESI's −10…+10 scale) for fromType/fromID.
func SeedStanding(t *testing.T, sqlDB *sql.DB, characterID, fromID int, fromType string, standing float64) {
	t.Helper()
	if _, err := sqlDB.Exec(
		`INSERT INTO character_standing (character_id, from_id, from_type, standing, updated_at)
		 VALUES (?, ?, ?, ?, '2024-01-01T00:00:00Z')`,
		characterID, fromID, fromType, standing,
	); err != nil {
		t.Fatalf("seeding character_standing (%s %d): %v", fromType, fromID, err)
	}
}

// SeedStationOwner inserts the owner_corp_id that GET /universe/stations/{id}
// reports for stationID.
func SeedStationOwner(t *testing.T, sqlDB *sql.DB, stationID, ownerCorpID int64) {
	t.Helper()
	if _, err := sqlDB.Exec(
		`INSERT INTO station_owner (station_id, owner_corp_id, updated_at)
		 VALUES (?, ?, '2024-01-01T00:00:00Z')`,
		stationID, ownerCorpID,
	); err != nil {
		t.Fatalf("seeding station_owner %d: %v", stationID, err)
	}
}

// SeedToken inserts the single esi_token row, AES-GCM-encrypting
// refreshToken with tokenKey exactly as the auth callback does. Tests that
// exercise authenticated routes (the opportunity table) need this: without a
// stored token the server serves the re-authentication banner instead.
func SeedToken(t *testing.T, sqlDB *sql.DB, characterID int, tokenKey, refreshToken string) {
	t.Helper()
	ciphertext, err := tokencrypt.Encrypt(tokenKey, refreshToken)
	if err != nil {
		t.Fatalf("encrypting refresh token: %v", err)
	}
	if _, err := sqlDB.Exec(
		`INSERT INTO esi_token (character_id, owner_hash, encrypted_refresh_token, updated_at)
		 VALUES (?, 'owner', ?, '2024-01-01T00:00:00Z')`,
		characterID, ciphertext,
	); err != nil {
		t.Fatalf("seeding esi_token: %v", err)
	}
}
