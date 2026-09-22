// Package skills loads the single trading character's fee- and tax-relevant
// skill levels from the character_skill table (docs/spec/v2.md §4.3, §5).
// ranking and ledger share it so the two never drift on how a missing row
// resolves.
package skills

import (
	"context"
	"database/sql"
	"fmt"
)

// Skills holds the fee/tax-relevant skill levels. A missing character_skill
// row or a missing skill resolves to level 0, mirroring ESI's convention.
type Skills struct {
	BrokerRelationsLevel         int
	AccountingLevel              int
	AdvancedBrokerRelationsLevel int
}

// Load reads the single character_skill row. A missing row resolves to
// level-0 skills rather than an error.
func Load(ctx context.Context, db *sql.DB) (Skills, error) {
	var s Skills
	err := db.QueryRowContext(ctx, `
		SELECT broker_relations_level, accounting_level, advanced_broker_relations_level
		FROM character_skill ORDER BY character_id LIMIT 1`).Scan(
		&s.BrokerRelationsLevel, &s.AccountingLevel, &s.AdvancedBrokerRelationsLevel)
	if err == sql.ErrNoRows {
		return Skills{}, nil
	}
	if err != nil {
		return Skills{}, fmt.Errorf("loading character skills: %w", err)
	}
	return s, nil
}
