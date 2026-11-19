package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestSnowLayerMeltsUnderBlockLight pins the light gate of the snow layer's
// random tick: block light 12 removes it, posting block_destroy, 11 leaves it.
// vanilla: SnowLayerBlock.randomTick
func TestSnowLayerMeltsUnderBlockLight(t *testing.T) {
	w, p := newPhysicsWorld(t)
	const x, y, z = int32(0), GroundLevel + 1, int32(0)
	snow, ok := registry.FindState("minecraft:snow", map[string]string{"layers": "3"})
	if !ok {
		t.Fatal("the registry has no snow layer")
	}
	if !randomlyTicking(snow) {
		t.Fatal("a snow layer does not count as randomly ticking")
	}
	for _, tc := range []struct {
		light uint8
		melts bool
	}{{11, false}, {12, true}, {15, true}} {
		w.SetBlock(x, y, z, snow)
		setLightAt(t, w, x, y, z, 0, tc.light)
		var changes []BlockChange
		p.randomVisit(x, y, z, snow, &changes, nil)
		got := w.BlockAt(x, y, z)
		effects := p.TakeRandomEffects()
		if posted := len(effects) == 1 && effects[0].GameEvent == gameEventBlockDestroy; posted != tc.melts {
			t.Errorf("light %d: random effects %+v, want block_destroy only on a melt", tc.light, effects)
		}
		if tc.melts && got != airState {
			t.Errorf("light %d: snow stayed (state %d), want air", tc.light, got)
		}
		if !tc.melts && got != snow {
			t.Errorf("light %d: snow changed to state %d", tc.light, got)
		}
	}
}
