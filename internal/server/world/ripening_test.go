package world

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The ripening dice, pinned: a growth test only asks whether a planting's
// state moves, not how often, so a wrong roll rate — one in five where
// vanilla rolls one in ten — would go unnoticed. These tests drive the tick
// functions straight with reseeded dice and count the hits over many rolls.
const ripeningRolls = 4000

// ripeningSeed reseeds the engine's dice for every case, so the hit counts
// are deterministic.
const ripeningSeed = int64(1)

type ripeningTick func(x, y, z int32, state uint16, changes *[]BlockChange)

// ripeningHits rolls tick over one cell, resetting it to state before each
// roll so the rolls stand alone, and returns how many aged it.
func ripeningHits(w *World, p *Physics, x, y, z int32, state uint16, tick ripeningTick) int {
	hits := 0
	changes := make([]BlockChange, 0, 1)
	for range ripeningRolls {
		w.SetBlock(x, y, z, state)
		changes = changes[:0]
		tick(x, y, z, state, &changes)
		if len(changes) > 0 {
			hits++
		}
	}
	return hits
}

// TestTheRipeningDiceLandAtVanillasRates pins the three rolls: the nether
// wart ages on one roll in ten, the cocoa pod and the berry bush on one in
// five. A rate doubled or halved lands outside the bounds the many seeded
// rolls draw around the expected count.
func TestTheRipeningDiceLandAtVanillasRates(t *testing.T) {
	w, p := newPhysicsWorld(t)
	const x, y, z = int32(0), GroundLevel, int32(0)

	cases := []struct {
		name   string
		props  map[string]string
		tick   ripeningTick
		per    int
		wantLo int
		wantHi int
	}{
		{"minecraft:nether_wart", map[string]string{"age": "0"}, p.netherWartTick, 10, 320, 480},
		{"minecraft:cocoa", map[string]string{"age": "0", "facing": "east"}, p.cocoaTick, 5, 700, 900},
		{"minecraft:sweet_berry_bush", map[string]string{"age": "0"}, p.berryBushTick, 5, 700, 900},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state, ok := registry.FindState(tc.name, tc.props)
			if !ok {
				t.Fatalf("the registry has no %s %v", tc.name, tc.props)
			}
			p.rng = rand.New(rand.NewSource(ripeningSeed))
			hits := ripeningHits(w, p, x, y, z, state, tc.tick)
			if hits < tc.wantLo || hits > tc.wantHi {
				t.Errorf("%s ripened on %d of %d rolls, want about one in %d (%d..%d)",
					tc.name, hits, ripeningRolls, tc.per, tc.wantLo, tc.wantHi)
			}
		})
	}
}

// TestABerryBushRipensOnBlockLightAlone pins the light gate's read of both
// light layers: a bush under no sky at all still ripens from block light of
// nine above it, and stops one short of it.
func TestABerryBushRipensOnBlockLightAlone(t *testing.T) {
	w, p := newPhysicsWorld(t)
	const x, y, z = int32(0), GroundLevel, int32(0)
	bush, ok := registry.FindState("minecraft:sweet_berry_bush", map[string]string{"age": "0"})
	if !ok {
		t.Fatal("the registry has no sweet berry bush")
	}

	ripenings := func(blockLight uint8) int {
		p.rng = rand.New(rand.NewSource(ripeningSeed))
		setLightAt(t, w, x, y+1, z, 0, blockLight)
		return ripeningHits(w, p, x, y, z, bush, p.berryBushTick)
	}

	if got := ripenings(9); got == 0 {
		t.Error("the bush under block light nine did not ripen")
	}
	if got := ripenings(8); got != 0 {
		t.Errorf("the bush under block light eight ripened on %d rolls, want none", got)
	}
}

// TestARipeningWartPulsesTheObserverWatchingIt checks the ripen write: a
// nether wart on soul sand whose age the random pass bumps pulses the
// observer watching it for the two game ticks vanilla's pulse lasts.
// vanilla: NetherWartBlock.randomTick
func TestARipeningWartPulsesTheObserverWatchingIt(t *testing.T) {
	w, p, clock := observerCircuit(t)
	setBlock(t, w, p, 13, 62, 9, rsState(t, "minecraft:soul_sand", nil))
	setBlock(t, w, p, 13, 63, 9, rsState(t, "minecraft:nether_wart", map[string]string{"age": "0"}))
	for range 12 {
		*clock++
		p.Tick(*clock)
	}
	if props := observerProps(t, w, 14, 63, 9); props["powered"] == "true" {
		t.Fatalf("setup: the observer was pulsing before the dice: %v", props)
	}

	pulseTicks, lampOn := observerDicePulse(t, w, p, clock, 300, ripeningSeed, func() bool {
		_, props := rsNameAt(t, w, 13, 63, 9)
		return props["age"] != "0"
	})

	if pulseTicks != 2 {
		t.Errorf("the wart's ripening pulsed the observer for %d ticks, want the two-tick pulse", pulseTicks)
	}
	if !lampOn {
		t.Error("the lamp behind the observer never lit on the wart's ripening")
	}
}

// setLightAt writes the stored sky and block light nibbles of a cell, which
// the engine would otherwise only set through its own light computation.
func setLightAt(t *testing.T, w *World, x, y, z int32, sky, block uint8) {
	t.Helper()
	pos := ChunkPos{X: ChunkOf(float64(x)), Z: ChunkOf(float64(z))}
	w.mu.Lock()
	defer w.mu.Unlock()
	loaded, ok := w.chunks[pos]
	if !ok {
		t.Fatalf("the chunk at (%d, %d) is not loaded", x, z)
	}
	loaded.chunk.lightSet(skyLayer, x&15, y, z&15, sky)
	loaded.chunk.lightSet(blockLayer, x&15, y, z&15, block)
}
