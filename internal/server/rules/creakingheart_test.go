package rules

import (
	"math/rand"
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

// TestCreakingTrailRunsFromTheCreakingToTheHeartAndBack: the trail starts in
// one box and ends in the other, swapped when it runs toward the creaking.
// vanilla: CreakingHeartBlockEntity.emitParticles
func TestCreakingTrailRunsFromTheCreakingToTheHeartAndBack(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	boxMin, boxMax, heart := [3]float64{10, 0, 0}, [3]float64{11, 3, 1}, [3]float64{0, 0, 0}
	for range 50 {
		p := CreakingTrail(rng, boxMin, boxMax, heart, false)
		if p.X < 10 || p.X > 11 || p.ToX < 0 || p.ToX > 1 || p.Ticks < 10 || p.Ticks >= 50 {
			t.Fatalf("a trail toward the heart = %+v, want it from the creaking's box to the heart's", p)
		}
		p = CreakingTrail(rng, boxMin, boxMax, heart, true)
		if p.ToX < 10 || p.ToX > 11 || p.X < 0 || p.X > 1 {
			t.Fatalf("a trail toward the creaking = %+v, want it from the heart's box to the creaking's", p)
		}
	}
}

// TestCreakingHurtSoundDrawsNearerTheHeartAsTheTrailAges: the sound starts a
// fifth of the way from the creaking and louder while more of the trail is left.
// vanilla: CreakingHeartBlockEntity.serverTick
func TestCreakingHurtSoundDrawsNearerTheHeartAsTheTrailAges(t *testing.T) {
	target, heart := [3]float64{10.5, 0.5, 0.5}, [3]float64{0.5, 0.5, 0.5}
	x, _, _, loud := CreakingHurtSound(100, target, heart)
	if x != 8 {
		t.Errorf("the first sound is at x=%d, want 8 (a fifth of the way)", x)
	}
	x2, _, _, quiet := CreakingHurtSound(10, target, heart)
	if x2 >= x || quiet >= loud {
		t.Errorf("the last sound at x=%d volume %v, want nearer and quieter than x=%d volume %v", x2, quiet, x, loud)
	}
}
