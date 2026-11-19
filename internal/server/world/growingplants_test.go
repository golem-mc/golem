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

// TestAGrowingVinePulsesTheObserverWatchingTheNewCell checks that the head's
// growth runs the shape pass an observer hears.
// vanilla: GrowingPlantHeadBlock.randomTick
func TestAGrowingVinePulsesTheObserverWatchingTheNewCell(t *testing.T) {
	w, p, clock := observerCircuit(t)
	// The watched cell is stone until cleared.
	setBlock(t, w, p, 13, 63, 9, rsState(t, "minecraft:air", nil))
	setBlock(t, w, p, 13, 62, 9, rsState(t, "minecraft:twisting_vines", map[string]string{"age": "10"}))
	for range 12 {
		*clock++
		p.Tick(*clock)
	}
	if props := observerProps(t, w, 14, 63, 9); props["powered"] == "true" {
		t.Fatalf("setup: the observer was pulsing before the dice: %v", props)
	}

	pulseTicks, lampOn := observerDicePulse(t, w, p, clock, 100, 11, func() bool {
		name, _ := rsNameAt(t, w, 13, 63, 9)
		return name == "minecraft:twisting_vines"
	})

	if pulseTicks != 2 {
		t.Errorf("the vine's growth pulsed the observer for %d ticks, want the two-tick pulse", pulseTicks)
	}
	if !lampOn {
		t.Error("the lamp behind the observer never lit on the vine's growth")
	}
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

// TestCaveVinesNewHeadRollsBerries checks the 0.11 chance of berries on the
// head a growth adds.
// vanilla: CaveVinesBlock.CHANCE_OF_BERRIES_ON_GROWTH
func TestCaveVinesNewHeadRollsBerries(t *testing.T) {
	w, p := newPhysicsWorld(t)
	p.rng = rand.New(rand.NewSource(5))
	const trials = 600
	withBerries := 0
	setBlock(t, w, p, 13, 71, 9, rsState(t, "minecraft:stone", nil))
	for range trials {
		w.SetBlock(13, 69, 9, rsState(t, "minecraft:air", nil))
		setBlock(t, w, p, 13, 70, 9, rsState(t, "minecraft:cave_vines", map[string]string{"age": "4", "berries": "false"}))
		_, head := growHead(t, w, p, 13, 70, 9, -1)
		if _, props := stateNameProps(t, head); props["berries"] == "true" {
			withBerries++
		}
	}
	// 0.11 of 600 is 66; the bounds sit about five standard deviations out.
	if withBerries < 30 || withBerries > 110 {
		t.Errorf("%d of %d new heads carry berries, want about 11%%", withBerries, trials)
	}
}
