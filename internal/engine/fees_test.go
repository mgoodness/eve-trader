package engine_test

import (
	"testing"

	"github.com/mgoodness/eve-trader/internal/engine"
)

// The pilot's real skills and standings, from spec §4: character 932683762,
// Trade 4, Broker Relations 4, Accounting 3, zero faction/corp standing →
// 21 active orders, broker 1.8%, sales tax 5.025%. These are independent,
// worked-example literals from the spec, not values recomputed the way the
// code computes them.
func pilotSkills() map[int32]int {
	return map[int32]int{
		engine.TradeSkillID:           4,
		engine.BrokerRelationsSkillID: 4,
		engine.AccountingSkillID:      3,
	}
}

func TestDeriveFeesAtThePilotsRealSkillsAndZeroStandings(t *testing.T) {
	fees := engine.DeriveFees(pilotSkills(), 0, 0)

	if fees.Broker != 0.018 {
		t.Errorf("got broker fee %v, want 0.018", fees.Broker)
	}
	if fees.SalesTax != 0.05025 {
		t.Errorf("got sales tax %v, want 0.05025", fees.SalesTax)
	}
}

func TestDeriveFeesWithNoSkillsAtAllDefaultsToBaseRatesNotMaxSkills(t *testing.T) {
	fees := engine.DeriveFees(map[int32]int{}, 0, 0)

	if fees.Broker != 0.03 {
		t.Errorf("got broker fee %v, want 0.03 (base rate, no Broker Relations trained)", fees.Broker)
	}
	if fees.SalesTax != 0.075 {
		t.Errorf("got sales tax %v, want 0.075 (base rate, no Accounting trained)", fees.SalesTax)
	}
}

func TestDeriveFeesStandingsReduceTheBrokerFee(t *testing.T) {
	fees := engine.DeriveFees(map[int32]int{}, 10, 10)

	// 3% - 0.03%*10 - 0.02%*10 = 3% - 0.3% - 0.2% = 2.5%
	if fees.Broker != 0.025 {
		t.Errorf("got broker fee %v, want 0.025", fees.Broker)
	}
}

func TestDeriveFeesFloorsTheBrokerFeeAtOnePercent(t *testing.T) {
	fees := engine.DeriveFees(map[int32]int{engine.BrokerRelationsSkillID: 5}, 10, 10)

	// 3% - 0.3%*5 - 0.03%*10 - 0.02%*10 = 3% - 1.5% - 0.3% - 0.2% = 1.0%, the floor.
	if fees.Broker != 0.01 {
		t.Errorf("got broker fee %v, want 0.01 (the 1%% floor)", fees.Broker)
	}
}

func TestOrderLimitAtThePilotsRealSkills(t *testing.T) {
	if got := engine.OrderLimit(pilotSkills()); got != 21 {
		t.Errorf("got order limit %d, want 21", got)
	}
}

func TestOrderLimitWithNoOrderSkillsAtAllDefaultsToTheBaseLimitNotMaxSkills(t *testing.T) {
	if got := engine.OrderLimit(map[int32]int{}); got != 5 {
		t.Errorf("got order limit %d, want 5 (base, no order skills trained)", got)
	}
}
