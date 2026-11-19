package world

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestFluidSlopeDistanceBound(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	for cx := int32(-1); cx <= 0; cx++ {
		for cz := int32(-1); cz <= 0; cz++ {
			w.Acquire(ChunkPos{X: cx, Z: cz})
		}
	}
	p := NewPhysics(w)
	rng := rand.New(rand.NewSource(3651))
	states := []uint16{
		stoneState, airState,
		registry.MustState("minecraft:water", map[string]string{"level": "3"}),
		registry.BlockOakSlab.DefaultState(),
		registry.MustState("minecraft:oak_slab", map[string]string{"type": "top"}),
		registry.BlockOakStairs.DefaultState(),
		registry.MustState("minecraft:oak_stairs", map[string]string{"shape": "inner_left"}),
		registry.BlockOakTrapdoor.DefaultState(),
		registry.MustState("minecraft:oak_trapdoor", map[string]string{"open": "true"}),
	}
	reach := p.behaviourOf(fluidWater).slopeFindDistance
	for plane := range 500 {
		for x := int32(-spreadReach); x <= spreadReach; x++ {
			for z := int32(-spreadReach); z <= spreadReach; z++ {
				w.SetBlock(x, 64, z, states[rng.Intn(len(states))])
				w.SetBlock(x, 63, z, states[rng.Intn(2)])
			}
		}
		ctx := p.newSpreadContext(0, 64, 0, fluidWater)
		for start, d := range horizontal4 {
			ctx.state(d[0], d[1])
			i, _ := ctx.index(d[0], d[1])
			for from := range horizontal4 {
				ref := ctx.slopeDistanceRef(i, 1, from, reach)
				for r := 0; r <= reach; r++ {
					// Use an independent cache so the reference cannot pre-load the bounded walk.
					bounded := p.newSpreadContext(0, 64, 0, fluidWater)
					bounded.state(d[0], d[1])
					got := bounded.slopeDistance(i, 1, from, r)
					if (ref <= r && got != ref) || (ref > r && got <= r) {
						t.Fatalf("plane=%d start=%d from=%d bound=%d: got %d, unbounded %d", plane, start, from, r, got, ref)
					}
				}
			}
		}
	}
}

// slopeDistanceRef preserves the exhaustive walk as a differential oracle.
func (c *spreadContext) slopeDistanceRef(i, pass, from, reach int) int {
	lowest := 1000
	flags := c.flags[i]
	for d, step := range spreadStep {
		if d == from {
			continue
		}
		n := i + step
		nf := c.flags[n]
		if nf&cellKnown == 0 {
			c.load(n)
			nf = c.flags[n]
		}
		if nf&cellEnterable == 0 || (flags|nf)&cellFullShape != 0 {
			continue
		}
		if flags&nf&cellEmptyShape == 0 {
			axis, positive := faceOf(horizontal4[d][0], horizontal4[d][1])
			if mergedFaceOccludes(collisionOf(c.states[i]).boxes, collisionOf(c.states[n]).boxes, axis, positive) {
				continue
			}
		}
		if nf&cellHoleKnown == 0 {
			c.holeAt(n)
			nf = c.flags[n]
		}
		if nf&cellHole != 0 {
			return pass
		}
		if pass >= reach {
			continue
		}
		if v := c.slopeDistanceRef(n, pass+1, d^2, reach); v < lowest {
			lowest = v
		}
	}
	return lowest
}
