package scanner

import (
	"testing"

	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
)

func TestSalesTaxPct(t *testing.T) {
	cases := []struct {
		name            string
		accountingLevel int32
		want            float64
	}{
		{"no accounting skill", 0, 7.5},
		{"level 1", 1, 7.5 * (1 - 0.11)},
		{"level 5", 5, 7.5 * (1 - 0.55)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			skills := esi.Skills{Skills: []esi.Skill{
				{SkillID: AccountingSkillID, ActiveSkillLevel: c.accountingLevel},
			}}
			if got := SalesTaxPct(skills); !almostEqual(got, c.want) {
				t.Errorf("SalesTaxPct() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSalesTaxPctWithoutTheSkillTrainedIsBaseRate(t *testing.T) {
	if got := SalesTaxPct(esi.Skills{}); !almostEqual(got, 7.5) {
		t.Errorf("SalesTaxPct(no skills) = %v, want 7.5 (base rate, untrained = level 0)", got)
	}
}

func TestBrokerFeePct(t *testing.T) {
	cases := []struct {
		name               string
		brokerRelationsLvl int32
		factionStanding    float64
		corpStanding       float64
		want               float64
	}{
		{"no skill, no standings", 0, 0, 0, 3.0},
		{"max broker relations only", 5, 0, 0, 3.0 - 0.3*5},
		{"max everything", 5, 10, 10, 3.0 - 0.3*5 - 0.03*10 - 0.02*10}, // = 1.0, the documented minimum
		{"partial", 2, 4, 6, 3.0 - 0.3*2 - 0.03*4 - 0.02*6},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			skills := esi.Skills{Skills: []esi.Skill{
				{SkillID: BrokerRelationsSkillID, ActiveSkillLevel: c.brokerRelationsLvl},
			}}
			standings := []esi.Standing{
				{FromID: hub.Jita.OwnerFactionID, FromType: "faction", Standing: c.factionStanding},
				{FromID: hub.Jita.OwnerCorpID, FromType: "npc_corp", Standing: c.corpStanding},
			}
			if got := BrokerFeePct(skills, standings, hub.Jita); !almostEqual(got, c.want) {
				t.Errorf("BrokerFeePct() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestBrokerFeePctIsPerHubStanding(t *testing.T) {
	// Good standing with Jita's owner (Caldari Navy/State) shouldn't
	// discount the fee at Rens, which is owned by a different
	// corp/faction entirely.
	skills := esi.Skills{}
	standings := []esi.Standing{
		{FromID: hub.Jita.OwnerFactionID, FromType: "faction", Standing: 10},
		{FromID: hub.Jita.OwnerCorpID, FromType: "npc_corp", Standing: 10},
	}

	gotJita := BrokerFeePct(skills, standings, hub.Jita)
	gotRens := BrokerFeePct(skills, standings, hub.Rens)

	if !almostEqual(gotJita, 3.0-0.03*10-0.02*10) {
		t.Errorf("BrokerFeePct(Jita) = %v, want the discounted rate", gotJita)
	}
	if !almostEqual(gotRens, 3.0) {
		t.Errorf("BrokerFeePct(Rens) = %v, want the undiscounted base rate (no standing with Rens's owner)", gotRens)
	}
}

func TestBrokerFeePctFloorsAtZero(t *testing.T) {
	// Not reachable through legitimate skill/standing caps, but the
	// computation shouldn't produce a nonsensical negative fee if it
	// ever were.
	skills := esi.Skills{Skills: []esi.Skill{{SkillID: BrokerRelationsSkillID, ActiveSkillLevel: 100}}}
	if got := BrokerFeePct(skills, nil, hub.Jita); got < 0 {
		t.Errorf("BrokerFeePct() = %v, want floored at 0", got)
	}
}

func TestMargin(t *testing.T) {
	// bestSell=110, bestBuy=100, brokerFee=1%, salesTax=3.3%:
	// spread = 10
	// broker fee paid twice: 1% * (100 + 110) = 2.1
	// sales tax paid once on the sale: 3.3% * 110 = 3.63
	// margin = 10 - 2.1 - 3.63 = 4.27
	got := Margin(100, 110, 1.0, 3.3)
	want := 10 - (0.01 * (100 + 110)) - (0.033 * 110)
	if !almostEqual(got, want) {
		t.Errorf("Margin() = %v, want %v", got, want)
	}
}

func TestMarginHigherFeesReduceMargin(t *testing.T) {
	low := Margin(100, 110, 1.0, 3.3)
	high := Margin(100, 110, 3.0, 7.5)
	if high >= low {
		t.Errorf("Margin(high fees) = %v, want less than Margin(low fees) = %v", high, low)
	}
}

func almostEqual(a, b float64) bool {
	const epsilon = 1e-9
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < epsilon
}
