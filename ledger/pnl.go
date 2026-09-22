// Package ledger reads portfolio facts from eve-trader's append-only local
// ledger of raw ESI records (docs/spec/v2.md §5). Positions and P/L are
// derived on read, never persisted. This file holds the P/L engine: it
// replays the character's filled transactions and transfers chronologically
// to produce weighted-average-cost positions, realized and unrealized P/L,
// and honest fee buckets (docs/spec/v2.md §4).
package ledger

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/mgoodness/eve-trader/internal/fees"
	"github.com/mgoodness/eve-trader/internal/skills"
)

// Skills holds the fee/tax-relevant skill levels the P/L engine needs. It
// is the shared skills.Skills type, so a missing row resolves the same way
// here as in the ranking.
type Skills = skills.Skills

// LoadSkills reads the single character_skill row. A missing row resolves
// to level-0 skills rather than an error.
func LoadSkills(ctx context.Context, db *sql.DB) (Skills, error) {
	return skills.Load(ctx, db)
}

// Status is the decision a position's row demands: whether the current
// Rens best sell already clears its costs, whether it does not, whether it
// left the character, or whether there is nothing to price against. It
// drives the Portfolio's At-target / Below-target / Transfers / No-market
// / Closed grouping (docs/spec/v2.md §7).
type Status string

const (
	// StatusAtTarget means the current best sell covers cost plus estimated
	// sell fees -- the market clears break-even. Status is measured against
	// break-even (0% net) independently of the view's Target net margin, so
	// the group means "costs are covered" at any target setting.
	StatusAtTarget Status = "At target now"
	// StatusBelowTarget means the current best sell would sell at a loss.
	StatusBelowTarget Status = "Below target"
	// StatusTransfer marks goods that left the character without a sale.
	StatusTransfer Status = "Transferred"
	// StatusNoMarket marks a held position with no Rens best sell.
	StatusNoMarket Status = "No market"
	// StatusClosed marks a position fully disposed of by sales.
	StatusClosed Status = "Closed"
)

// Position is one derived (item, location) row.
type Position struct {
	TypeID              int
	Name                string
	LocationID          int64
	Quantity            int
	AverageCost         float64
	CostBasis           float64
	Realized            float64
	Unrealized          float64
	HasMarket           bool
	MarketSell          float64 // current Rens best sell (gross)
	LiquidationBuy      float64 // current Rens best buy (gross)
	EstimatedFees       float64 // buy + sell + sales tax allocated to this position
	TransferGainLoss    float64 // priced transfer value minus average cost; excluded from Realized
	TransferredQuantity int
	BuyRelists          int // inferred from order snapshots; may undercount
	SellRelists         int
	// BreakEvenLow/High bound the list price covering cost + estimated sell
	// fees at zero profit (docs/spec/v2.md §4.7). Low uses only confidently
	// allocated fees; high also shares the position's slice of the
	// unattributed-fee bucket. The conservative high end is the headline.
	BreakEvenLow    float64
	BreakEvenHigh   float64
	TargetLow       float64 // break-even plus the view's target net margin
	TargetHigh      float64
	MarketNetMargin float64 // net margin at the current best sell; <= 0 means below break-even
	Status          Status
}

// RelistGain is one resting sell order worth moving: a raise-only re-list
// to the market's best sell among other traders that still nets more after
// the in-place modify fee and the extra sales tax on the increase
// (docs/spec/v2.md §4.8). One row per order, independent of the position's
// target.
type RelistGain struct {
	OrderID      int64
	TypeID       int
	Name         string
	LocationID   int64
	VolumeRemain int
	OldPrice     float64 // the order's current resting price
	NewPrice     float64 // the market's best sell among other traders
	NetGain      float64
}

// Report is the whole portfolio's derived P/L.
type Report struct {
	Positions        []Position
	RelistGains      []RelistGain // resting sell orders worth moving (§4.8)
	Realized         float64      // trading realized P/L, excluding transfer gain/loss
	Unrealized       float64
	EstimatedFees    float64
	UnattributedFees float64
	SunkFees         float64
	PendingFees      float64 // charged open-order fees not yet in cost basis
	JournalFees      float64 // actual brokers_fee + transaction_tax from the journal
	TransferGainLoss float64
	TargetNetMargin  float64 // view-level target net margin used for the Target range (0 by default)
}

// ComputePnL derives the portfolio report from the ledger at the default
// 0% target net margin, so each Target equals its Break-even. characterID
// is the tracked character, used to decide which side of a contract gave
// up the goods. The position list is ordered by type then location.
func ComputePnL(ctx context.Context, db *sql.DB, characterID int) (Report, error) {
	return ComputePnLForTarget(ctx, db, characterID, 0)
}

// ComputePnLForTarget derives the portfolio report with a view-level target
// net margin (a fraction, e.g. 0.05 for 5%), carried from the Portfolio's
// URL control (docs/spec/v2.md §4.7). It only moves the Target range; the
// break-even, market-implied margin, and status are independent of it.
func ComputePnLForTarget(ctx context.Context, db *sql.DB, characterID int, targetNetMargin float64) (Report, error) {
	skills, err := LoadSkills(ctx, db)
	if err != nil {
		return Report{}, err
	}
	rb := fees.BrokerFeeRate(skills.BrokerRelationsLevel)
	rt := fees.SalesTaxRate(skills.AccountingLevel)
	abr := float64(skills.AdvancedBrokerRelationsLevel)

	transactions, err := loadTransactions(ctx, db)
	if err != nil {
		return Report{}, err
	}
	orders, err := loadOrders(ctx, db, rb, abr)
	if err != nil {
		return Report{}, err
	}
	transfers, err := LoadTransfers(ctx, db, characterID)
	if err != nil {
		return Report{}, err
	}

	// Attribute each transaction to a re-list chain, then compute the
	// per-transaction fee share.
	orderFees := matchTransactions(transactions, orders, rb)
	relistCounts := countRelists(orders)

	journalFees, err := loadJournalFees(ctx, db)
	if err != nil {
		return Report{}, err
	}
	prices, err := loadMarketPrices(ctx, db)
	if err != nil {
		return Report{}, err
	}
	names, err := loadNames(ctx, db)
	if err != nil {
		return Report{}, err
	}

	events := buildEvents(transactions, orderFees, transfers, rt)
	state, report := replay(events)

	// Remaining held quantity is marked to the Rens best sell, net of an
	// estimated fresh sell fee and sales tax; no market means no fabricated
	// price (docs/spec/v2.md §4.6).
	var sunk, pending float64
	for _, o := range orders {
		sunk += o.SunkFee
		pending += o.Pending
	}
	report.SunkFees = sunk
	report.PendingFees = pending
	report.JournalFees = journalFees
	report.TargetNetMargin = targetNetMargin
	// Pending open-order fees are charged cash but not yet in cost basis;
	// they are neither unattributed nor sunk (docs/spec/v2.md §4.4).
	report.UnattributedFees = journalFees - report.EstimatedFees - report.SunkFees - report.PendingFees

	// The forecast's high end shares the unattributed bucket across open
	// positions; a negative bucket (journal missing fees) shares nothing.
	sharing := report.UnattributedFees
	if sharing < 0 {
		sharing = 0
	}
	positions := finalizePositions(state, prices, names, relistCounts, rt, rb, targetNetMargin, sharing)
	for _, p := range positions {
		report.Unrealized += p.Unrealized
	}
	report.Positions = positions

	// The re-list gain compares each resting sell order against the best
	// sell among *other* traders, so the character's own orders -- which the
	// public book also carries -- are excluded (docs/spec/v2.md §4.8).
	own := make(map[int64]bool, len(orders))
	for _, o := range orders {
		own[o.ID] = true
	}
	otherBestSells, err := loadOtherBestSells(ctx, db, own)
	if err != nil {
		return Report{}, err
	}
	report.RelistGains = relistGains(orders, otherBestSells, names, rb, rt, abr)
	return report, nil
}

// transaction is a wallet_transaction row plus its attributed fee.
type transaction struct {
	ID         int64
	Date       time.Time
	TypeID     int
	Quantity   int
	UnitPrice  float64
	IsBuy      bool
	LocationID int64
	Fee        float64
}

func loadTransactions(ctx context.Context, db *sql.DB) ([]transaction, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT transaction_id, date, type_id, quantity, unit_price, is_buy, location_id
		FROM wallet_transaction
		ORDER BY date, transaction_id`)
	if err != nil {
		return nil, fmt.Errorf("querying wallet transactions: %w", err)
	}
	defer rows.Close()

	var out []transaction
	for rows.Next() {
		var (
			t        transaction
			date     string
			isBuyInt int
		)
		if err := rows.Scan(&t.ID, &date, &t.TypeID, &t.Quantity, &t.UnitPrice, &isBuyInt, &t.LocationID); err != nil {
			return nil, fmt.Errorf("scanning wallet transaction: %w", err)
		}
		parsed, err := time.Parse(time.RFC3339Nano, date)
		if err != nil {
			return nil, fmt.Errorf("parsing wallet transaction %d date %q: %w", t.ID, date, err)
		}
		t.Date = parsed
		t.IsBuy = isBuyInt != 0
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading wallet transactions: %w", err)
	}
	return out, nil
}

// orderSnapshot is one (order_id, issued) character_order row.
type orderSnapshot struct {
	Issued       time.Time
	Price        float64
	VolumeRemain int
	State        string
}

// order is one order_id's re-list chain, with its estimated fee split into
// the part allocatable to filled units and the part that is sunk.
type order struct {
	ID          int64
	TypeID      int
	LocationID  int64
	IsBuy       bool
	VolumeTotal int
	Prices      map[float64]bool
	Issued      time.Time
	Closed      bool
	Snapshots   []orderSnapshot
	Fee         float64
	MatchedQty  int
	Allocated   float64
	SunkFee     float64
	Pending     float64
}

func loadOrders(ctx context.Context, db *sql.DB, rb, abr float64) ([]*order, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT order_id, issued, type_id, location_id, is_buy_order, price,
		       volume_remain, volume_total, state
		FROM character_order
		ORDER BY order_id, issued`)
	if err != nil {
		return nil, fmt.Errorf("querying character orders: %w", err)
	}
	defer rows.Close()

	byID := map[int64]*order{}
	var ids []int64
	for rows.Next() {
		var (
			orderID      int64
			issued       string
			typeID       int
			locationID   int64
			isBuyInt     int
			price        float64
			volumeRemain int
			volumeTotal  int
			state        string
		)
		if err := rows.Scan(&orderID, &issued, &typeID, &locationID, &isBuyInt, &price, &volumeRemain, &volumeTotal, &state); err != nil {
			return nil, fmt.Errorf("scanning character order: %w", err)
		}
		parsed, err := time.Parse(time.RFC3339Nano, issued)
		if err != nil {
			return nil, fmt.Errorf("parsing character order %d issued %q: %w", orderID, issued, err)
		}
		o := byID[orderID]
		if o == nil {
			o = &order{
				ID:          orderID,
				TypeID:      typeID,
				LocationID:  locationID,
				IsBuy:       isBuyInt != 0,
				VolumeTotal: volumeTotal,
				Prices:      map[float64]bool{},
				Issued:      parsed,
			}
			byID[orderID] = o
			ids = append(ids, orderID)
		}
		o.Prices[price] = true
		o.Snapshots = append(o.Snapshots, orderSnapshot{Issued: parsed, Price: price, VolumeRemain: volumeRemain, State: state})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading character orders: %w", err)
	}

	var out []*order
	for _, id := range ids {
		o := byID[id]
		o.estimateFee(rb, abr)
		out = append(out, o)
	}
	return out, nil
}

// estimateFee sums the observed placement and in-place modify fees for the
// chain. The first snapshot is the placement; every later snapshot is a
// modify (a new issued for the same order_id means an in-place reprice).
// volume_remain is the volume at each modify, per docs/spec/v2.md §4.4.
func (o *order) estimateFee(rb, abr float64) {
	var fee float64
	for i, snap := range o.Snapshots {
		if i == 0 {
			fee += fees.PlacementFee(snap.Price*float64(o.VolumeTotal), rb)
			continue
		}
		oldValue := o.Snapshots[i-1].Price * float64(snap.VolumeRemain)
		newValue := snap.Price * float64(snap.VolumeRemain)
		fee += fees.ModifyFee(oldValue, newValue, rb, abr)
	}
	o.Fee = fee

	last := o.Snapshots[len(o.Snapshots)-1]
	o.Closed = last.State != "" || last.VolumeRemain == 0
}

// matchTransactions attributes fills to re-list chains and returns each
// transaction's estimated broker fee: a share of its chain's fee, or a
// fresh placement-fee estimate when the chain is unknown (a fully-filled
// order vanishes from the order routes).
func matchTransactions(txs []transaction, orders []*order, rb float64) []float64 {
	attributed := make([]*order, len(txs))
	matched := map[*order]int{}
	for i := range txs {
		best := matchOrder(&txs[i], orders)
		attributed[i] = best
		if best != nil {
			matched[best] += txs[i].Quantity
		}
	}
	// Allocate each chain's fee to filled units, then split that across the
	// fills proportionally to quantity. An open chain's unfilled share is
	// pending; a closed chain's is sunk.
	for _, o := range orders {
		matchedQty := matched[o]
		if matchedQty > o.VolumeTotal {
			matchedQty = o.VolumeTotal
		}
		o.MatchedQty = matchedQty
		if o.VolumeTotal > 0 {
			o.Allocated = o.Fee * float64(matchedQty) / float64(o.VolumeTotal)
		}
		if o.Closed {
			o.SunkFee = o.Fee - o.Allocated
		} else {
			o.Pending = o.Fee - o.Allocated
		}
	}
	shares := make([]float64, len(txs))
	for i := range txs {
		o := attributed[i]
		if o == nil || o.VolumeTotal == 0 {
			// A fully-filled order vanishes from the order routes: estimate
			// a fresh placement fee for the fill.
			shares[i] = fees.PlacementFee(float64(txs[i].Quantity)*txs[i].UnitPrice, rb)
			continue
		}
		shares[i] = o.Fee * float64(txs[i].Quantity) / float64(o.VolumeTotal)
	}
	return shares
}

// matchOrder finds the re-list chain that most plausibly filled t: same
// (type, location, side), a price the chain ever rested at, issued at or
// before the fill, and -- among those -- the most recently issued.
func matchOrder(t *transaction, orders []*order) *order {
	var best *order
	for _, o := range orders {
		if o.TypeID != t.TypeID || o.LocationID != t.LocationID || o.IsBuy != t.IsBuy {
			continue
		}
		if !o.Prices[t.UnitPrice] || t.Date.Before(o.Issued) {
			continue
		}
		if best == nil || o.Issued.After(best.Issued) {
			best = o
		}
	}
	return best
}

// relistCount is the inferred buy/sell re-list count for one position.
type relistCount struct {
	buy  int
	sell int
}

// countRelists reconstructs re-list chains per (type, location, side) from
// the order snapshots: one re-list per extra order_id in the chain
// (cancel-and-recreate) plus one per extra issued snapshot on an order
// (in-place modify). Inferred, never definitive (docs/spec/v2.md §3).
func countRelists(orders []*order) map[positionKey]relistCount {
	type orderKey struct {
		typeID     int
		locationID int64
		isBuy      bool
	}
	groups := map[orderKey][]*order{}
	var keys []orderKey
	for _, o := range orders {
		k := orderKey{o.TypeID, o.LocationID, o.IsBuy}
		if _, ok := groups[k]; !ok {
			keys = append(keys, k)
		}
		groups[k] = append(groups[k], o)
	}
	out := map[positionKey]relistCount{}
	for _, k := range keys {
		group := groups[k]
		count := len(group) - 1
		for _, o := range group {
			count += len(o.Snapshots) - 1
		}
		pk := positionKey{typeID: k.typeID, locationID: k.locationID}
		rc := out[pk]
		if k.isBuy {
			rc.buy += count
		} else {
			rc.sell += count
		}
		out[pk] = rc
	}
	return out
}

func loadJournalFees(ctx context.Context, db *sql.DB) (float64, error) {
	var brokerSum, taxSum float64
	err := db.QueryRowContext(ctx, `
		SELECT COALESCE(SUM(CASE WHEN ref_type = 'brokers_fee' THEN amount ELSE 0 END), 0),
		       COALESCE(SUM(CASE WHEN ref_type = 'transaction_tax' THEN amount ELSE 0 END), 0)
		FROM wallet_journal`).Scan(&brokerSum, &taxSum)
	if err != nil {
		return 0, fmt.Errorf("summing journal fees: %w", err)
	}
	// ESI records expenses as negative amounts; the fee magnitudes are
	// their negation.
	return -(brokerSum + taxSum), nil
}

type marketPrices struct {
	bestBuy  float64
	bestSell float64
	hasSell  bool
}

func loadMarketPrices(ctx context.Context, db *sql.DB) (map[int]marketPrices, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT type_id,
		       MAX(CASE WHEN is_buy_order = 1 THEN price END),
		       MIN(CASE WHEN is_buy_order = 0 THEN price END)
		FROM market_order
		GROUP BY type_id`)
	if err != nil {
		return nil, fmt.Errorf("querying market prices: %w", err)
	}
	defer rows.Close()

	out := map[int]marketPrices{}
	for rows.Next() {
		var (
			typeID   int
			bestBuy  sql.NullFloat64
			bestSell sql.NullFloat64
		)
		if err := rows.Scan(&typeID, &bestBuy, &bestSell); err != nil {
			return nil, fmt.Errorf("scanning market price: %w", err)
		}
		out[typeID] = marketPrices{bestBuy: bestBuy.Float64, bestSell: bestSell.Float64, hasSell: bestSell.Valid}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading market prices: %w", err)
	}
	return out, nil
}

// loadOtherBestSells returns, per type, the lowest sell price in the Rens
// book among orders the character does not own. The re-list gain needs the
// price level the character is actually competing against: including the
// character's own best order would always make the current best sell equal
// to that order's price, leaving nothing to raise to.
func loadOtherBestSells(ctx context.Context, db *sql.DB, own map[int64]bool) (map[int]float64, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT type_id, order_id, price
		FROM market_order
		WHERE is_buy_order = 0`)
	if err != nil {
		return nil, fmt.Errorf("querying sell orders: %w", err)
	}
	defer rows.Close()

	out := map[int]float64{}
	for rows.Next() {
		var (
			typeID  int
			orderID int64
			price   float64
		)
		if err := rows.Scan(&typeID, &orderID, &price); err != nil {
			return nil, fmt.Errorf("scanning sell order: %w", err)
		}
		if own[orderID] {
			continue
		}
		if best, ok := out[typeID]; !ok || price < best {
			out[typeID] = price
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading sell orders: %w", err)
	}
	return out, nil
}

// relistGains computes the raise-only re-list gain for every open resting
// sell order priced below the market's best sell among other traders
// (docs/spec/v2.md §4.8):
//
//	net = (P_new - P_old) x volume_remain x (1 - R_t) - relistFee
//
// where relistFee is the in-place modify fee evaluating old and new order
// values at the order's remaining volume. Buy orders, orders undercut by
// someone else, orders already at/above best, and any move that does not
// net positive are dropped.
func relistGains(orders []*order, otherBestSells map[int]float64, names map[int]string, rb, rt, abr float64) []RelistGain {
	var out []RelistGain
	for _, o := range orders {
		if o.IsBuy || o.Closed {
			continue
		}
		last := o.Snapshots[len(o.Snapshots)-1]
		if last.VolumeRemain <= 0 {
			continue
		}
		newPrice, ok := otherBestSells[o.TypeID]
		if !ok || last.Price >= newPrice {
			continue
		}
		volume := float64(last.VolumeRemain)
		fee := fees.ModifyFee(last.Price*volume, newPrice*volume, rb, abr)
		net := (newPrice-last.Price)*volume*(1-rt) - fee
		if net <= 0 {
			continue
		}
		out = append(out, RelistGain{
			OrderID:      o.ID,
			TypeID:       o.TypeID,
			Name:         names[o.TypeID],
			LocationID:   o.LocationID,
			VolumeRemain: last.VolumeRemain,
			OldPrice:     last.Price,
			NewPrice:     newPrice,
			NetGain:      net,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TypeID != out[j].TypeID {
			return out[i].TypeID < out[j].TypeID
		}
		if out[i].LocationID != out[j].LocationID {
			return out[i].LocationID < out[j].LocationID
		}
		return out[i].OrderID < out[j].OrderID
	})
	return out
}

func loadNames(ctx context.Context, db *sql.DB) (map[int]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT type_id, name FROM item_type`)
	if err != nil {
		return nil, fmt.Errorf("querying item names: %w", err)
	}
	defer rows.Close()

	out := map[int]string{}
	for rows.Next() {
		var (
			typeID int
			name   string
		)
		if err := rows.Scan(&typeID, &name); err != nil {
			return nil, fmt.Errorf("scanning item name: %w", err)
		}
		out[typeID] = name
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading item names: %w", err)
	}
	return out, nil
}

type eventKind int

const (
	buyEvent eventKind = iota
	sellEvent
	transferEvent
)

type event struct {
	kind          eventKind
	date          time.Time
	typeID        int
	locationID    int64
	quantity      int
	gross         float64
	fee           float64
	tax           float64
	transferValue float64
	transferSrc   TransferSource
}

func buildEvents(txs []transaction, shares []float64, transfers []Transfer, rt float64) []event {
	var events []event
	for i := range txs {
		t := &txs[i]
		if t.IsBuy {
			events = append(events, event{
				kind: buyEvent, date: t.Date, typeID: t.TypeID, locationID: t.LocationID,
				quantity: t.Quantity, gross: float64(t.Quantity) * t.UnitPrice, fee: shares[i],
			})
			continue
		}
		gross := float64(t.Quantity) * t.UnitPrice
		events = append(events, event{
			kind: sellEvent, date: t.Date, typeID: t.TypeID, locationID: t.LocationID,
			quantity: t.Quantity, gross: gross, fee: shares[i], tax: gross * rt,
		})
	}
	for _, tr := range transfers {
		events = append(events, event{
			kind: transferEvent, date: tr.Date, typeID: tr.TypeID, locationID: tr.LocationID,
			quantity: tr.Quantity, transferValue: tr.Price, transferSrc: tr.Source,
		})
	}
	sort.SliceStable(events, func(i, j int) bool {
		if !events[i].date.Equal(events[j].date) {
			return events[i].date.Before(events[j].date)
		}
		if events[i].kind != events[j].kind {
			return events[i].kind < events[j].kind
		}
		return events[i].typeID < events[j].typeID
	})
	return events
}

type positionKey struct {
	typeID     int
	locationID int64
}

type positionState struct {
	qty       int
	cost      float64
	realized  float64
	estFees   float64
	transGain float64
	transQty  int
}

// replay walks the chronological events, applying weighted-average cost.
func replay(events []event) (map[positionKey]*positionState, Report) {
	state := map[positionKey]*positionState{}
	var report Report

	// resolveLocation finds where a location-less contract transfer should
	// land: anywhere the type is held, the largest remaining position. A
	// manual transfer keeps its user-entered location.
	resolveLocation := func(e event) (int64, bool) {
		if e.locationID != 0 {
			if p := state[positionKey{typeID: e.typeID, locationID: e.locationID}]; p != nil && p.qty > 0 {
				return e.locationID, true
			}
			if e.transferSrc == TransferManual {
				return e.locationID, true
			}
		}
		var bestKey positionKey
		bestQty := 0
		for k, p := range state {
			if k.typeID == e.typeID && p.qty > bestQty {
				bestKey, bestQty = k, p.qty
			}
		}
		return bestKey.locationID, bestQty > 0
	}

	for _, e := range events {
		switch e.kind {
		case buyEvent:
			p := ensure(state, e.typeID, e.locationID)
			p.cost += e.gross + e.fee
			p.qty += e.quantity
			p.estFees += e.fee
			report.EstimatedFees += e.fee
		case sellEvent:
			p := ensure(state, e.typeID, e.locationID)
			remove := e.quantity
			if remove > p.qty {
				remove = p.qty
			}
			avg := averageCost(p)
			costSold := avg * float64(remove)
			p.cost -= costSold
			p.qty -= remove
			gain := e.gross - e.fee - e.tax - costSold
			p.realized += gain
			p.estFees += e.fee + e.tax
			report.Realized += gain
			report.EstimatedFees += e.fee + e.tax
		case transferEvent:
			loc, ok := resolveLocation(e)
			if !ok {
				continue
			}
			p := ensure(state, e.typeID, loc)
			remove := e.quantity
			if remove > p.qty {
				remove = p.qty
			}
			if remove <= 0 {
				continue
			}
			avg := averageCost(p)
			costRemoved := avg * float64(remove)
			p.cost -= costRemoved
			p.qty -= remove
			value := costRemoved
			if e.transferValue > 0 && e.quantity > 0 {
				value = e.transferValue * float64(remove) / float64(e.quantity)
			}
			gain := value - costRemoved
			p.transGain += gain
			p.transQty += remove
			report.TransferGainLoss += gain
		}
	}
	return state, report
}

func ensure(state map[positionKey]*positionState, typeID int, locationID int64) *positionState {
	k := positionKey{typeID: typeID, locationID: locationID}
	p := state[k]
	if p == nil {
		p = &positionState{}
		state[k] = p
	}
	return p
}

func averageCost(p *positionState) float64 {
	if p.qty <= 0 {
		return 0
	}
	return p.cost / float64(p.qty)
}

// breakEvenPrice is the list price at which a position's net proceeds --
// gross of the sale less the estimated broker fee and sales tax -- cover
// its cost basis and allocated estimated fees, i.e. zero net margin
// (docs/spec/v2.md §4.7). The broker fee is the same max(100 ISK, value×R_b)
// the engine charges elsewhere (§4.4), so the floor cannot silently
// disappear from a small position's forecast.
func breakEvenPrice(cost float64, qty int, rb, rt float64) float64 {
	return targetPrice(cost, qty, rb, rt, 0)
}

// targetPrice generalizes breakEvenPrice to a target net margin m: the list
// price at which (net proceeds − cost) / gross equals m. With m = 0 it is
// the break-even price. Like the break-even it honors the max(100 ISK,
// value×R_b) broker-fee floor, and returns 0 when the margin is so high that
// no finite price could reach it.
func targetPrice(cost float64, qty int, rb, rt, m float64) float64 {
	if qty <= 0 {
		return 0
	}
	// First assume the percentage fee clears the 100 ISK floor.
	denom := float64(qty) * (1 - rb - rt - m)
	if denom <= 0 {
		return 0
	}
	price := cost / denom
	if price*float64(qty)*rb >= fees.MinFee {
		return price
	}
	// The floor dominates: net proceeds are price×qty − 100 − price×qty×R_t,
	// and net margin m requires those proceeds to exceed cost by m×gross.
	denomFloor := float64(qty) * (1 - rt - m)
	if denomFloor <= 0 {
		return 0
	}
	return (cost + fees.MinFee) / denomFloor
}

// statusFor picks the Portfolio group for a position. A position with no
// remaining quantity is a Transfer (it left off-market) or Closed (it was
// sold); a held position is No market without a Rens best sell, otherwise
// At target / Below target by whether the current market clears its cost.
func statusFor(p *positionState, pos Position) Status {
	switch {
	case p.qty == 0 && p.transQty > 0:
		return StatusTransfer
	case p.qty == 0:
		return StatusClosed
	case !pos.HasMarket:
		return StatusNoMarket
	case pos.Unrealized >= 0:
		return StatusAtTarget
	default:
		return StatusBelowTarget
	}
}

func finalizePositions(state map[positionKey]*positionState, prices map[int]marketPrices, names map[int]string, relists map[positionKey]relistCount, rt, rb, targetNetMargin, unattributed float64) []Position {
	// The high end of each forecast shares the unattributed-fee bucket in
	// proportion to the position's confidently-allocated estimated fees, so
	// the position most likely to have generated the unlinked re-list fees
	// carries the most of them (docs/spec/v2.md §4.7).
	var openFeeWeight float64
	for _, p := range state {
		if p.qty > 0 {
			openFeeWeight += p.estFees
		}
	}

	var out []Position
	for k, p := range state {
		if p.qty == 0 && p.realized == 0 && p.transGain == 0 && p.estFees == 0 && p.transQty == 0 {
			continue
		}
		rc := relists[k]
		pos := Position{
			TypeID:              k.typeID,
			Name:                names[k.typeID],
			LocationID:          k.locationID,
			Quantity:            p.qty,
			AverageCost:         averageCost(p),
			CostBasis:           p.cost,
			Realized:            p.realized,
			EstimatedFees:       p.estFees,
			TransferGainLoss:    p.transGain,
			TransferredQuantity: p.transQty,
			BuyRelists:          rc.buy,
			SellRelists:         rc.sell,
		}
		if p.qty > 0 {
			if mp, ok := prices[k.typeID]; ok {
				pos.HasMarket = mp.hasSell
				pos.MarketSell = mp.bestSell
				pos.LiquidationBuy = mp.bestBuy
				if mp.hasSell {
					// The market-implied margin and the status use the
					// confidently-allocated fees only (the low end), so they
					// report what the market actually yields rather than the
					// unattributed-fee guess (re-list-forecast addendum).
					grossValue := float64(p.qty) * mp.bestSell
					sellFee := fees.PlacementFee(grossValue, rb)
					tax := grossValue * rt
					pos.Unrealized = grossValue - sellFee - tax - p.cost
					if grossValue > 0 {
						pos.MarketNetMargin = pos.Unrealized / grossValue
					}
				}
			}
			share := 0.0
			if unattributed > 0 && openFeeWeight > 0 {
				share = unattributed * p.estFees / openFeeWeight
			}
			pos.BreakEvenLow = breakEvenPrice(p.cost, p.qty, rb, rt)
			pos.BreakEvenHigh = breakEvenPrice(p.cost+share, p.qty, rb, rt)
			pos.TargetLow = targetPrice(p.cost, p.qty, rb, rt, targetNetMargin)
			pos.TargetHigh = targetPrice(p.cost+share, p.qty, rb, rt, targetNetMargin)
		}
		pos.Status = statusFor(p, pos)
		out = append(out, pos)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TypeID != out[j].TypeID {
			return out[i].TypeID < out[j].TypeID
		}
		return out[i].LocationID < out[j].LocationID
	})
	return out
}
