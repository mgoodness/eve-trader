package cache_test

import (
	"testing"
	"time"

	"github.com/mgoodness/eve-trader/internal/cache"
)

func TestUntilDailyUTCReturnsTheDurationToTodaysOccurrenceWhenStillAhead(t *testing.T) {
	now := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)

	got := cache.UntilDailyUTC(now, 11, 5)

	want := 2*time.Hour + 5*time.Minute
	if got != want {
		t.Errorf("got %v, want %v (today's 11:05 UTC is 2h5m after 09:00)", got, want)
	}
}

func TestUntilDailyUTCRollsOverToTomorrowOnceTodaysOccurrenceHasPassed(t *testing.T) {
	now := time.Date(2026, 9, 30, 14, 0, 0, 0, time.UTC)

	got := cache.UntilDailyUTC(now, 11, 5)

	want := 21*time.Hour + 5*time.Minute
	if got != want {
		t.Errorf("got %v, want %v (tomorrow's 11:05 UTC is 21h5m after 14:00)", got, want)
	}
}

func TestUntilDailyUTCRollsOverExactlyAtTheOccurrence(t *testing.T) {
	now := time.Date(2026, 9, 30, 11, 5, 0, 0, time.UTC)

	got := cache.UntilDailyUTC(now, 11, 5)

	want := 24 * time.Hour
	if got != want {
		t.Errorf("got %v, want 24h (at the exact occurrence, the next one is a full day away, not zero)", got)
	}
}

func TestUntilDailyUTCConvertsAnInputTimeInAnotherZoneToUTCFirst(t *testing.T) {
	loc := time.FixedZone("UTC-5", -5*60*60)
	// 06:00 in UTC-5 is 11:00 UTC -- 5 minutes before today's 11:05 UTC.
	now := time.Date(2026, 9, 30, 6, 0, 0, 0, loc)

	got := cache.UntilDailyUTC(now, 11, 5)

	want := 5 * time.Minute
	if got != want {
		t.Errorf("got %v, want 5m (11:00 UTC is 5 minutes before 11:05 UTC)", got)
	}
}
