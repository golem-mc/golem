package server

import (
	"testing"
	"time"
)

// TestUserCacheExpiryClampsTheDay checks the month a fresh entry lasts:
// vanilla's Calendar.add(MONTH, 1) keeps the day where the target month has
// it and clamps to the month's last where it does not, so a late-month join
// lapses inside the next month rather than rolling past it.
func TestUserCacheExpiryClampsTheDay(t *testing.T) {
	t.Parallel()
	cases := []struct {
		now  time.Time
		want time.Time
	}{
		{time.Date(2021, 1, 15, 12, 0, 0, 0, time.UTC), time.Date(2021, 2, 15, 12, 0, 0, 0, time.UTC)},
		{time.Date(2021, 1, 31, 12, 0, 0, 0, time.UTC), time.Date(2021, 2, 28, 12, 0, 0, 0, time.UTC)},
		{time.Date(2020, 1, 31, 12, 0, 0, 0, time.UTC), time.Date(2020, 2, 29, 12, 0, 0, 0, time.UTC)},
		{time.Date(2021, 3, 31, 12, 0, 0, 0, time.UTC), time.Date(2021, 4, 30, 12, 0, 0, 0, time.UTC)},
		{time.Date(2021, 12, 31, 12, 0, 0, 0, time.UTC), time.Date(2022, 1, 31, 12, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		if got := userCacheExpiry(tc.now); !got.Equal(tc.want) {
			t.Errorf("userCacheExpiry(%s) = %s, want %s", tc.now, got, tc.want)
		}
	}
}
