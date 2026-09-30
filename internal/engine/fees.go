package engine

// Skill type IDs needed to derive fee rates and the order limit (spec §4;
// docs/research/eve-market-mechanics-and-esi.md §4, §7.4). Only the skills
// that affect the values this ticket derives are named here.
const (
	TradeSkillID              int32 = 3443
	RetailSkillID             int32 = 3444
	BrokerRelationsSkillID    int32 = 3446
	WholesaleSkillID          int32 = 16596
	AdvancedBrokerRelationsID int32 = 16597
	AccountingSkillID         int32 = 16622
	TycoonSkillID             int32 = 18580
)

// DeriveFees computes the pilot's NPC-station broker fee and sales tax
// (spec §4) from active skill levels and unmodified standings toward the
// trade station's owning corporation and faction. A skill absent from
// skills is treated as level 0 — the base, un-trained rate — never as
// max skill; the same holds for standings of 0.
//
//	broker fee = max(1%, 3% − 0.3%·BrokerRelations − 0.03%·factionStanding − 0.02%·corpStanding)
//	sales tax  = 7.5% × (1 − 0.11·Accounting)
func DeriveFees(skills map[int32]int, factionStanding, corpStanding float64) Fees {
	brokerRelations := float64(skills[BrokerRelationsSkillID])
	accounting := float64(skills[AccountingSkillID])

	broker := 0.03 - 0.003*brokerRelations - 0.0003*factionStanding - 0.0002*corpStanding
	if broker < 0.01 {
		broker = 0.01
	}

	salesTax := 0.075 * (1 - 0.11*accounting)

	return Fees{Broker: broker, SalesTax: salesTax}
}

// OrderLimit computes the pilot's active-order limit (spec §4) from active
// skill levels. A skill absent from skills is treated as level 0, so an
// untrained pilot gets the base limit of 5, never a max-skill limit.
//
//	order limit = 5 + 4·Trade + 8·Retail + 16·Wholesale + 32·Tycoon
func OrderLimit(skills map[int32]int) int {
	return 5 + 4*skills[TradeSkillID] + 8*skills[RetailSkillID] + 16*skills[WholesaleSkillID] + 32*skills[TycoonSkillID]
}
