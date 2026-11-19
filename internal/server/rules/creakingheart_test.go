package rules

import (
	"testing"
)

// TestCreakingHeartOutputScalesTheDistanceIn32Blocks: 15 beside the creaking,
// one less per 32/15 blocks, 0 from 32 blocks on.
// vanilla: CreakingHeartBlockEntity.computeAnalogOutputSignal
func TestCreakingHeartOutputScalesTheDistanceIn32Blocks(t *testing.T) {
	for _, tc := range []struct {
		distance float64
		want     int
	}{{0, 15}, {2.1, 15}, {2.2, 14}, {16, 8}, {31.9, 1}, {32, 0}, {80, 0}} {
		if got := CreakingHeartOutput(tc.distance); got != tc.want {
			t.Errorf("CreakingHeartOutput(%v) = %d, want %d", tc.distance, got, tc.want)
		}
	}
}

// TestCreakingProtectorKeptAsksNightDistanceAndEyes: the heart keeps its
// creaking only while all three allow it, a persistent one outliving the day.
// vanilla: CreakingHeartBlockEntity.serverTick
func TestCreakingProtectorKeptAsksNightDistanceAndEyes(t *testing.T) {
	for _, tc := range []struct {
		night, persistent bool
		distance          float64
		stuck, want       bool
	}{
		{true, false, 10, false, true},
		{false, false, 10, false, false},
		{false, true, 10, false, true},
		{true, false, 34, false, true},
		{true, false, 34.1, false, false},
		{true, false, 10, true, false},
	} {
		if got := CreakingProtectorKept(tc.night, tc.persistent, tc.distance, tc.stuck); got != tc.want {
			t.Errorf("CreakingProtectorKept(%+v) = %v, want %v", tc, got, tc.want)
		}
	}
}
