package rules

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// TestRespawnVitalsKeepsTheBarOnlyWhenTheDeathDroppedNone pins the two terms
// the bar rides a respawn on: keep_inventory and a spectator keep it, any
// other death comes back empty, and the rest of the vitals is always fresh.
func TestRespawnVitalsKeepsTheBarOnlyWhenTheDeathDroppedNone(t *testing.T) {
	dying := player.Vitals{
		GameMode:           protocol.GameModeSurvival,
		Health:             3,
		Food:               2,
		ExperienceLevel:    2,
		ExperienceProgress: 0.5,
		ExperienceTotal:    16,
	}
	spectator := dying
	spectator.GameMode = protocol.GameModeSpectator

	for _, tc := range []struct {
		name    string
		old     player.Vitals
		keep    bool
		wantBar bool
	}{
		{"keep_inventory, survival", dying, true, true},
		{"a spectator", spectator, false, true},
		{"a plain death", dying, false, false},
	} {
		v, kept := RespawnVitalsKeeping(tc.old, tc.keep, tc.keep)
		if v.Health != MaxHealth || v.Food != MaxFood {
			t.Errorf("%s: vitals = %+v, want full health and food", tc.name, v)
		}
		gotBar := v.ExperienceLevel == tc.old.ExperienceLevel &&
			v.ExperienceProgress == tc.old.ExperienceProgress &&
			v.ExperienceTotal == tc.old.ExperienceTotal
		if gotBar != tc.wantBar {
			t.Errorf("%s: bar kept = %v, want %v (got %+v)", tc.name, gotBar, tc.wantBar, v)
		}
		if kept != gotBar {
			t.Errorf("%s: kept = %v but bar kept = %v, want the same decision", tc.name, kept, gotBar)
		}
	}
}

// TestExhaustionDrainsSaturationFirst and the food only when the saturation
// is gone: a counter past four loses four on the tick, and one point of the
// one or the other with it; at four or below it only piles up.
func TestExhaustionDrainsSaturationFirst(t *testing.T) {
	v := FreshVitals(0) // any mode: the drain is the same for everyone
	exhaust(&v, 4)
	drainExhaustion(&v, DifficultyNormal)
	if v.Exhaustion != 4 || v.Saturation != 5 {
		t.Errorf("four exhaustion points moved the bars, want only past four to: %+v", v)
	}

	// Past four converts into a point of saturation, the food untouched.
	exhaust(&v, 0.5)
	drainExhaustion(&v, DifficultyNormal)
	if v.Exhaustion != 0.5 || v.Saturation != 4 || v.Food != MaxFood {
		t.Errorf("4.5 exhaustion points did not drain saturation: %+v", v)
	}

	// The saturation runs out before the food is touched...
	for v.Saturation > 0 {
		v.Exhaustion = 5
		drainExhaustion(&v, DifficultyNormal)
	}
	if v.Food != MaxFood {
		t.Errorf("the drain took food while saturation was left: %+v", v)
	}

	// ...and then each drain takes one food instead.
	v.Exhaustion = 5
	drainExhaustion(&v, DifficultyNormal)
	if v.Food != MaxFood-1 || v.Exhaustion != 1 {
		t.Errorf("a drain did not take one food: %+v", v)
	}

	// An empty body has nothing left to drain, the counter still counting.
	v.Food = 0
	v.Exhaustion = 5
	drainExhaustion(&v, DifficultyNormal)
	if v.Food != 0 || v.Exhaustion != 1 {
		t.Errorf("the drain invented food or kept the counter: %+v", v)
	}
}

// TestExhaustionDrainsOnePointATick spreads a large cost over ticks, the way
// FoodData.tick converts at most four of the counter each time, and caps
// the counter at forty the way addExhaustion does.
func TestExhaustionDrainsOnePointATick(t *testing.T) {
	v := FreshVitals(0)
	v.Saturation = 0
	exhaust(&v, 100) // a whole lot of sprinting at once
	if v.Exhaustion != 40 {
		t.Fatalf("the counter holds %v, want the cap of forty", v.Exhaustion)
	}
	drainExhaustion(&v, DifficultyNormal)
	if v.Food != MaxFood-1 {
		t.Errorf("one tick drained to food %v, want one point gone", v.Food)
	}
	for range 20 {
		drainExhaustion(&v, DifficultyNormal)
	}
	// Forty drains while it is above four: nine of them, the last leaving
	// exactly four behind.
	if v.Food != MaxFood-9 || v.Exhaustion != 4 {
		t.Errorf("the drain left %+v, want nine food gone and four counted", v)
	}
}

// TestPeacefulExhaustionSparesTheFood drains saturation as anywhere else but
// never the food, the Difficulty.PEACEFUL check in FoodData.tick.
func TestPeacefulExhaustionSparesTheFood(t *testing.T) {
	v := FreshVitals(0)
	v.Saturation = 1
	v.Exhaustion = 10
	drainExhaustion(&v, DifficultyPeaceful)
	drainExhaustion(&v, DifficultyPeaceful)
	if v.Saturation != 0 || v.Food != MaxFood || v.Exhaustion != 2 {
		t.Errorf("peaceful drained to %+v, want the saturation gone and the food whole", v)
	}
}

// TestExhaustionNeverDropsBarsBelowZero pins the floors the drain stops at:
// saturation at zero, food at zero, and the exhaustion counter itself
// always finite.
func TestExhaustionNeverDropsBarsBelowZero(t *testing.T) {
	v := FreshVitals(0)
	v.Saturation = 0.5
	exhaust(&v, 5)
	drainExhaustion(&v, DifficultyNormal)
	if v.Saturation != 0 {
		t.Errorf("the drain ran saturation below zero: %v", v.Saturation)
	}
	if math.IsNaN(v.Exhaustion) || math.IsInf(v.Exhaustion, 0) {
		t.Errorf("the exhaustion counter is not finite: %v", v.Exhaustion)
	}
}
