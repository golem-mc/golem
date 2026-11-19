package rules

import (
	"testing"
	"time"
)

func TestIsHalloweenIsOctober31Only(t *testing.T) {
	for _, c := range []struct {
		d    time.Time
		want bool
	}{
		{time.Date(2026, time.October, 31, 0, 0, 0, 0, time.UTC), true},
		{time.Date(2027, time.October, 31, 23, 59, 0, 0, time.UTC), true},
		{time.Date(2026, time.October, 30, 12, 0, 0, 0, time.UTC), false},
		{time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC), false},
		{time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC), false},
	} {
		if got := IsHalloween(c.d); got != c.want {
			t.Errorf("IsHalloween(%v) = %v, want %v", c.d, got, c.want)
		}
	}
}
