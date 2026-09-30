package cache

import "time"

// UntilDailyUTC returns the duration from now until the next occurrence of
// hour:minute UTC, rolling over to tomorrow if now is already at or past
// today's occurrence. It exists for ESI routes that expire once a day at a
// fixed UTC time rather than after a fixed TTL window — market history is
// the one documented case (research eve-market-mechanics-and-esi.md §6.2:
// "This route expires daily at 11:05") — so a caller can pass the result
// straight to Set/SetWithETag as the entry's ttl.
func UntilDailyUTC(now time.Time, hour, minute int) time.Duration {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
	if !next.After(now) {
		next = next.Add(24 * time.Hour)
	}
	return next.Sub(now)
}
