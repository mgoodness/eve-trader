// Package engine is the pure recommendation engine: books, pricing, and the
// JSON result shape (spec §11). It performs no I/O — no network, no disk, no
// clock reads beyond what a caller hands it. Every adapter (CLI today; web,
// TUI, daemon, MCP later) is a thin shell around this package.
package engine

// Order mirrors the fields of an ESI region-market order record that the
// engine needs. Callers (the esi adapter) are responsible for fetching and
// decoding the wire format into this shape.
type Order struct {
	OrderID      int64
	TypeID       int32
	LocationID   int64
	SystemID     int32
	Price        float64
	VolumeRemain int64
	MinVolume    int64
	IsBuyOrder   bool
	// Range is the order's range enum: "station", "solarsystem", "region", or
	// a numeric jump count as a decimal string ("1".."40"). Only buy orders
	// carry a meaningful range; sell orders have none.
	Range string
}
