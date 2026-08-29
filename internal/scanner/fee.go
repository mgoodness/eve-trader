package scanner

import (
	"github.com/mgoodness/eve-trader/internal/esi"
	"github.com/mgoodness/eve-trader/internal/hub"
)

// Skill type IDs for the two skills that affect Fees (see
// docs/adr/0006-fee-computation-formulas.md).
const (
	AccountingSkillID      int32 = 16622
	BrokerRelationsSkillID int32 = 3446
)

const (
	baseSalesTaxPct                  = 7.5
	accountingReductionPerLevel      = 0.11 // multiplicative, per docs/adr/0006
	baseBrokerFeePct                 = 3.0
	brokerRelationsReductionPerLevel = 0.3
	factionStandingReductionPerPoint = 0.03
	corpStandingReductionPerPoint    = 0.02
)

// skillLevel returns the character's trained level in skillID, or 0 if
// they haven't trained it at all.
func skillLevel(skills esi.Skills, skillID int32) int32 {
	for _, s := range skills.Skills {
		if s.SkillID == skillID {
			return s.ActiveSkillLevel
		}
	}
	return 0
}

// standingWith returns the character's standing with fromID, or 0
// (neutral) if no standing entry exists for it.
func standingWith(standings []esi.Standing, fromID int32) float64 {
	for _, s := range standings {
		if s.FromID == fromID {
			return s.Standing
		}
	}
	return 0
}

// SalesTaxPct is the character's current sales-tax rate (a percentage,
// e.g. 7.5 means 7.5%), from their Accounting skill level. Sales tax
// isn't Hub-specific -- it doesn't depend on standings (see
// docs/adr/0006-fee-computation-formulas.md).
func SalesTaxPct(skills esi.Skills) float64 {
	level := skillLevel(skills, AccountingSkillID)
	pct := baseSalesTaxPct * (1 - accountingReductionPerLevel*float64(level))
	return max(pct, 0)
}

// BrokerFeePct is the character's current broker-fee rate at h (a
// percentage), from their Broker Relations skill level and their
// standings with h's station owner specifically -- good standing at one
// Hub doesn't discount the fee at the other (see
// docs/adr/0006-fee-computation-formulas.md).
func BrokerFeePct(skills esi.Skills, standings []esi.Standing, h hub.Hub) float64 {
	level := skillLevel(skills, BrokerRelationsSkillID)
	factionStanding := standingWith(standings, h.OwnerFactionID)
	corpStanding := standingWith(standings, h.OwnerCorpID)

	pct := baseBrokerFeePct -
		brokerRelationsReductionPerLevel*float64(level) -
		factionStandingReductionPerPoint*factionStanding -
		corpStandingReductionPerPoint*corpStanding
	return max(pct, 0)
}

// Margin is the net profit of a station trade at one Hub for one item:
// the spread between bestSell and bestBuy, minus Fees -- the broker fee
// (paid twice, on both the buy and sell order's value) and sales tax
// (paid once, on the sale). brokerFeePct and salesTaxPct are percentages
// (e.g. 3.0 means 3%), as returned by BrokerFeePct/SalesTaxPct.
func Margin(bestBuy, bestSell, brokerFeePct, salesTaxPct float64) float64 {
	spread := bestSell - bestBuy
	brokerFees := (brokerFeePct / 100) * (bestBuy + bestSell)
	salesTax := (salesTaxPct / 100) * bestSell
	return spread - brokerFees - salesTax
}

// Markup is an Opportunity's Margin expressed as a fraction of its best
// buy order price (see CONTEXT.md's Markup definition): a
// return-on-capital view, distinct from the absolute-ISK Margin. A
// fraction, not a percentage -- e.g. 0.15 for "15%".
func Markup(margin, bestBuy float64) float64 {
	return margin / bestBuy
}
