package world

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// growHead calls plantHeadTick on the head at (x, y, z) until it grows, and
// returns the states of the head cell and of the cell it grew into.
func growHead(t *testing.T, w *World, p *Physics, x, y, z, dy int32) (body, head uint16) {
	t.Helper()
	for range 1000 {
		var changes []BlockChange
		p.plantHeadTick(x, y, z, w.BlockAt(x, y, z), &changes)
		if len(changes) > 0 {
			return w.BlockAt(x, y, z), w.BlockAt(x, y+dy, z)
		}
	}
	t.Fatal("the head never grew")
	return 0, 0
}

func stateNameProps(t *testing.T, state uint16) (string, map[string]string) {
	t.Helper()
	name, props, ok := registry.BlockStateName(state)
	if !ok {
		t.Fatalf("state %d is not a named block", state)
	}
	return name, registry.ParseStateProps(props)
}

// TestCaveVinesGrowthCarriesBerriesToTheBody checks that the old head's
// berries pass to the plant block it becomes.
// vanilla: CaveVinesBlock.updateBodyAfterConvertedFromHead
func TestCaveVinesGrowthCarriesBerriesToTheBody(t *testing.T) {
	for _, berries := range []string{"true", "false"} {
		t.Run(berries, func(t *testing.T) {
			w, p := newPhysicsWorld(t)
			p.rng = rand.New(rand.NewSource(3))
			setBlock(t, w, p, 13, 71, 9, rsState(t, "minecraft:stone", nil))
			setBlock(t, w, p, 13, 70, 9, rsState(t, "minecraft:cave_vines", map[string]string{"age": "4", "berries": berries}))

			body, _ := growHead(t, w, p, 13, 70, 9, -1)
			name, props := stateNameProps(t, body)
			if name != "minecraft:cave_vines_plant" || props["berries"] != berries {
				t.Errorf("body is %s %v, want cave_vines_plant with berries=%s", name, props, berries)
			}
		})
	}
}
