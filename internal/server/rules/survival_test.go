package rules

import (
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
