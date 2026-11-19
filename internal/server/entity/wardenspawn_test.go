package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

type wardenSpawnTerrain struct{ testWorld }

func (w wardenSpawnTerrain) BoxCollides(x0, y0, z0, x1, y1, z1 float64) bool {
	for x := int32(math.Floor(x0)); x <= int32(math.Floor(x1)); x++ {
		for y := int32(math.Floor(y0)); y <= int32(math.Floor(y1)); y++ {
			for z := int32(math.Floor(z0)); z <= int32(math.Floor(z1)); z++ {
				if world.StateCollides(w.BlockAt(x, y, z), x, y, z, x0, y0, z0, x1, y1, z1) {
					return true
				}
			}
		}
	}
	return false
}

func TestWardenSpawnSearch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		floorY int32
		block  string
		want   bool
	}{
		{"bottom", 57, "stone", true}, {"top", 69, "stone", true},
		{"too_low", 56, "stone", false}, {"too_high", 70, "stone", false},
		{"leaves", 63, "oak_leaves", true}, {"slab", 63, "stone_slab", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := wardenSpawnTerrain{newTestWorld()}
			w.set(0, tc.floorY, 0, defaultState(t, "minecraft:"+tc.block))
			_, y, _, ok := FindWardenSpawnSpot(w, 0, 64, 0, func(n int) int { return n / 2 }, nil)
			if ok != tc.want || ok && y != tc.floorY+1 {
				t.Fatalf("y=%d ok=%v, want y=%d ok=%v", y, ok, tc.floorY+1, tc.want)
			}
		})
	}
}
