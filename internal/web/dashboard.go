package web

import (
	"context"
	"fmt"
	"time"

	"github.com/mgoodness/eve-trader/internal/esi"
)

// Station IDs for the two Hubs this tool trades at (see CONTEXT.md).
const (
	jitaStationID = 60003760
	rensStationID = 60004588
)

// orderRow is one row of the dashboard's Orders sidebar.
type orderRow struct {
	Item          string
	Hub           string
	Side          string
	Price         string
	VolumeRemain  int32
	TimeRemaining string
}

// dashboardView is the data passed to the dashboard template.
type dashboardView struct {
	Authenticated bool
	LoginURL      string
	Orders        []orderRow
}

// buildOrderRows fetches the character's open orders and resolves their
// item names into dashboard-ready rows.
func buildOrderRows(ctx context.Context, client esi.Client, characterID int32) ([]orderRow, error) {
	orders, err := client.CharacterOrders(ctx, characterID)
	if err != nil {
		return nil, fmt.Errorf("fetch character orders: %w", err)
	}

	typeIDs := make([]int32, 0, len(orders))
	seen := make(map[int32]bool, len(orders))
	for _, o := range orders {
		if !seen[o.TypeID] {
			seen[o.TypeID] = true
			typeIDs = append(typeIDs, o.TypeID)
		}
	}

	names, err := client.ResolveNames(ctx, typeIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve item names: %w", err)
	}

	now := time.Now()
	rows := make([]orderRow, len(orders))
	for i, o := range orders {
		name, ok := names[o.TypeID]
		if !ok {
			name = fmt.Sprintf("Item #%d", o.TypeID)
		}

		expiresAt := o.Issued.AddDate(0, 0, int(o.Duration))
		rows[i] = orderRow{
			Item:          name,
			Hub:           hubName(o.LocationID),
			Side:          sideName(o.IsBuyOrder),
			Price:         fmt.Sprintf("%.2f", o.Price),
			VolumeRemain:  o.VolumeRemain,
			TimeRemaining: formatTimeRemaining(expiresAt.Sub(now)),
		}
	}
	return rows, nil
}

func hubName(locationID int64) string {
	switch locationID {
	case jitaStationID:
		return "Jita"
	case rensStationID:
		return "Rens"
	default:
		return fmt.Sprintf("Station %d", locationID)
	}
}

func sideName(isBuyOrder bool) string {
	if isBuyOrder {
		return "Buy"
	}
	return "Sell"
}

// formatTimeRemaining renders a duration the way the chosen dashboard
// prototype does: "6d 4h", "18h", or "45m", falling back to "expired" for
// a non-positive duration.
func formatTimeRemaining(d time.Duration) string {
	if d <= 0 {
		return "expired"
	}

	days := int(d / (24 * time.Hour))
	hours := int(d/time.Hour) % 24
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh", hours)
	}
	minutes := int(d/time.Minute) % 60
	return fmt.Sprintf("%dm", minutes)
}
