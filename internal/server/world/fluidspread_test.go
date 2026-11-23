package world

import (
	"testing"
)

// uncachedSlopeDistance keeps the individual fluid gates as an oracle for the
// per-cell classification and wall shortcuts.
func uncachedSlopeDistance(x, z int32, pass int, fromDX, fromDZ int32, b fluidBehaviour, c *spreadContext) int {
	lowest := 1000
	state := c.state(x, z)
	for _, d := range horizontal4 {
		if d[0] == fromDX && d[1] == fromDZ {
			continue
		}
		nx, nz := x+d[0], z+d[1]
		ns := c.state(nx, nz)
		axis, positive := faceOf(d[0], d[1])
		if !canMaybePassThrough(c.kind, state, axis, positive, ns, fluidHolding(ns)) || !canHoldSpecific(ns, c.flowingType) {
			continue
		}
		if c.isHole(nx, nz) {
			return pass
		}
		if pass < b.slopeFindDistance {
			if v := uncachedSlopeDistance(nx, nz, pass+1, -d[0], -d[1], b, c); v < lowest {
				lowest = v
			}
		}
	}
	return lowest
}

// TestLavaSpreadReachByDimension checks the fast-lava drop-off and slope
// distance: lava reaches 7 blocks where it runs fast, 3 elsewhere.
func TestLavaSpreadReachByDimension(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fast     bool
		reach    int32
		wantNone int32
	}{{"normal", false, 3, 4}, {"fast", true, 7, 8}} {
		t.Run(tc.name, func(t *testing.T) {
			w, p := newPhysicsWorld(t)
			p.fastLava = tc.fast
			var clock int64
			setBlock(t, w, p, 0, GroundLevel, 0, lavaByLevel[0])
			tickToQuiet(t, p, &clock, 800)
			if got := fluidHolding(w.BlockAt(tc.reach, GroundLevel, 0)); got.kind != fluidLava {
				t.Errorf("no lava at +%d", tc.reach)
			}
			assertAir(t, w, tc.wantNone, GroundLevel, 0, "lava's reach ends")
		})
	}
}

// TestWaterReplacingLavaFlowsAtItsOwnDelay checks that a lava visit still
// pending does not hold back the visit of the water that replaced the lava.
func TestWaterReplacingLavaFlowsAtItsOwnDelay(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	setBlock(t, w, p, 0, GroundLevel, 0, lavaByLevel[0])
	runTicks(p, &clock, 1)
	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	runTicks(p, &clock, 8)
	if got := fluidHolding(w.BlockAt(1, GroundLevel, 0)); got.kind != fluidWater {
		t.Errorf("water beside the source after 8 ticks = %v, want water", got.kind)
	}
}
