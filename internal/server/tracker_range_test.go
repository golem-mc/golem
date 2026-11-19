package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
)

// TestInTrackingRangeScalesByBroadcastPercentage checks that
// entity-broadcast-range-percentage scales a kind's range before the view
// distance caps it.
func TestInTrackingRangeScalesByBroadcastPercentage(t *testing.T) {
	cow := entity.MobTrackingRange(entity.TypeCow)
	if cow != 10 {
		t.Fatalf("cow tracking range = %d chunks, want 10", cow)
	}
	const viewDistance = 10
	for _, tc := range []struct {
		pct  int
		want bool
	}{
		{50, false},
		{100, true},
		// 1000% is still capped by the view distance's 160 blocks.
		{1000, true},
	} {
		if got := inTrackingRange(0.5, 0.5, 100.5, 0.5, cow, tc.pct, viewDistance); got != tc.want {
			t.Errorf("cow 100 blocks away at %d%%: in range = %v, want %v", tc.pct, got, tc.want)
		}
	}
	if inTrackingRange(0.5, 0.5, 170.5, 0.5, cow, 1000, viewDistance) {
		t.Error("cow 170 blocks away at 1000% is in range, want the view distance's 160 blocks to cap it")
	}
}
