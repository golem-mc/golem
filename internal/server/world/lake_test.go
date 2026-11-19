package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestLakePredicatesControlRimAndReplacement(t *testing.T) {
	for _, rejectRim := range []bool{false, true} {
		world := newGoldenTerrain()
		r := newDecorRegion(world, newChunk(0, 0), worldMinY, worldHeight)
		r.rng.SetSeed(1)
		lava := registry.BlockLava.DefaultState()
		sulfur := registry.BlockSulfur.DefaultState()
		fluidCalls, barrierCalls := 0, 0
		cfg := lakeConfig{
			fluid: func(_ *decorRegion, p [3]int32) uint16 {
				fluidCalls++
				if p != [3]int32{0, 26, 0} {
					t.Errorf("fluid sampled at %v", p)
				}
				return lava
			},
			barrier: func(_ *decorRegion, p [3]int32) uint16 {
				barrierCalls++
				if p != [3]int32{0, 26, 0} {
					t.Errorf("barrier sampled at %v", p)
				}
				return sulfur
			},
			canPlace:       func(*decorRegion, [3]int32) bool { return !rejectRim },
			replaceFluid:   func(_ *decorRegion, p [3]int32) bool { return p[0]%2 == 0 },
			replaceBarrier: func(*decorRegion, [3]int32) bool { return false },
		}
		if placed := r.placeLake(cfg, [3]int32{8, 30, 8}); placed == rejectRim {
			t.Fatalf("rim rejection %v: placed %v", rejectRim, placed)
		}
		changed := 0
		for x := range int32(16) {
			for z := range int32(16) {
				for y := int32(26); y < 34; y++ {
					got := r.get(x, y, z)
					if got == world.at(x, y, z) {
						continue
					}
					changed++
					if rejectRim || x%2 != 0 || got == sulfur {
						t.Fatalf("replacement predicates bypassed at %d %d %d", x, y, z)
					}
				}
			}
		}
		if fluidCalls != 1 || rejectRim && (barrierCalls != 0 || changed != 0) || !rejectRim && (barrierCalls != 1 || changed == 0) {
			t.Fatalf("rim rejection %v: fluid calls %d, barrier calls %d, changes %d", rejectRim, fluidCalls, barrierCalls, changed)
		}
	}
}
