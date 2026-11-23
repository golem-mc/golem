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
		// Keep the oracle's cell cache independent of the candidate's search cache.
		ctx := p.newSpreadContext(0, 64, 0, fluidWater)
		bounded := p.newSpreadContext(0, 64, 0, fluidWater)
		for start, d := range horizontal4 {
			ctx.state(d[0], d[1])
			bounded.state(d[0], d[1])
			i, _ := ctx.index(d[0], d[1])
			for from := range horizontal4 {
				ref := ctx.slopeDistanceRef(i, 1, from, reach)
				for step := 0; step <= reach; step++ {
					r := step
					if plane%2 != 0 {
						r = reach - step
					}
					want := ref
					if ref > max(r, 1) {
						want = 1000
					}
					got := bounded.slopeDistance(i, 1, from, r)
					if got != want {
						t.Fatalf("plane=%d start=%d from=%d bound=%d: got %d, want %d (unbounded %d)", plane, start, from, r, got, want, ref)
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

func TestFluidSlopeTagReload(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackTags(nil) })

	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{X: 0, Z: 0})

	water := registry.MustState("minecraft:water", nil)
	torch := registry.MustState("minecraft:torch", nil)
	w.SetBlock(8, 64, 8, water)
	// Two adjacent cells share the same (air, torch) top/bottom state pair.
	w.SetBlock(8, 63, 10, torch)
	w.SetBlock(9, 63, 10, torch)

	p := NewPhysics(w)
	reach := p.behaviourOf(fluidWater).slopeFindDistance
	searchSouth := func(c *spreadContext) int {
		c.state(8, 9)
		ti, _ := c.index(8, 9)
		return c.slopeDistance(ti, 1, 0, reach)
	}

	// Built-in tags include torch in washed_away_by_fluids.
	before := p.newSpreadContext(8, 64, 8, fluidWater)
	if !before.isHole(8, 10) || !before.isHole(9, 10) {
		t.Fatal("torch floor not classified as a hole under built-in tags")
	}
	if got := searchSouth(&before); got != 1 {
		t.Fatalf("slope distance before reload = %d, want 1", got)
	}

	// Removing torch from washed_away_by_fluids changes hole classification for
	// the next spread while the earlier spread keeps its tag snapshot.
	registry.ApplyPackTags(registry.PackTags{"minecraft:block": {
		"minecraft:washed_away_by_fluids": {Replace: true, Entries: []registry.PackTagEntry{
			{ID: "minecraft:oak_stairs", Required: true},
		}},
	}})
	after := p.newSpreadContext(8, 64, 8, fluidWater)
	if after.isHole(8, 10) || after.isHole(9, 10) {
		t.Fatal("torch floor still classified as a hole after removal from washed_away_by_fluids")
	}
	if got := searchSouth(&after); got != 1000 {
		t.Fatalf("slope distance after reload = %d, want 1000", got)
	}
	// Earlier spread context retains its pre-reload snapshot on an unvisited cell.
	w.SetBlock(3, 63, 3, torch)
	if !before.isHole(3, 3) {
		t.Fatal("earlier spread did not keep its tag snapshot")
	}

	// Restoring built-in tags makes a fresh spread see the hole again.
	registry.ApplyPackTags(nil)
	restored := p.newSpreadContext(8, 64, 8, fluidWater)
	if !restored.isHole(8, 10) || !restored.isHole(9, 10) {
		t.Fatal("torch floor not classified as a hole after restoring built-in tags")
	}
	if got := searchSouth(&restored); got != 1 {
		t.Fatalf("slope distance after restore = %d, want 1", got)
	}
}
