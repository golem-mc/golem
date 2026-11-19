package world

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// darkMushroomPatch lays a roof over x, z in [-6, 6], so
// the cells between are dark enough for a mushroom to survive in.
func darkMushroomPatch(t *testing.T, w *World, p *Physics) {
	t.Helper()
	stone := registry.MustState("minecraft:stone", nil)
	for x := int32(-6); x <= 6; x++ {
		for z := int32(-6); z <= 6; z++ {
			setBlock(t, w, p, x, GroundLevel+2, z, stone)
		}
	}
	w.Relight(0, 0)
}

func countBlock(w *World, state uint16) int {
	n := 0
	for x := int32(-6); x <= 6; x++ {
		for z := int32(-6); z <= 6; z++ {
			for y := int32(GroundLevel); y <= GroundLevel+1; y++ {
				if w.BlockAt(x, y, z) == state {
					n++
				}
			}
		}
	}
	return n
}

// TestAMushroomSpreadsOnRandomTicksInTheDark checks the planting: dice
// landing on a lone mushroom in the dark seed more of its kind, and
// the pass stops once five stand within its box.
// vanilla: MushroomBlock.randomTick
func TestAMushroomSpreadsOnRandomTicksInTheDark(t *testing.T) {
	for _, name := range []string{"minecraft:brown_mushroom", "minecraft:red_mushroom"} {
		t.Run(name, func(t *testing.T) {
			w, p := newPhysicsWorld(t)
			darkMushroomPatch(t, w, p)
			state := registry.MustState(name, nil)
			setBlock(t, w, p, 0, GroundLevel, 0, state)
			p.rng = rand.New(rand.NewSource(7))
			var clock int64
			for range 20000 {
				clock++
				p.RandomTick(clock, []ChunkPos{{0, 0}}, 500, 1)
				if countBlock(w, state) >= 5 {
					break
				}
			}
			if got := countBlock(w, state); got != 5 {
				t.Fatalf("%d mushrooms after the dice, want it to stop at %d", got, 5)
			}
			// Crowded: more dice plant nothing.
			for range 5000 {
				clock++
				p.RandomTick(clock, []ChunkPos{{0, 0}}, 500, 1)
			}
			if got := countBlock(w, state); got != 5 {
				t.Fatalf("%d mushrooms after crowded dice, want %d", got, 5)
			}
		})
	}
}
