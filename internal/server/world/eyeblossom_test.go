package world

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The eyeblossom's rules at the physics level: the die that lands and the
// booked visit alike carry the flower to the hour's openness, the sound the
// new type makes rides the change — the long one the random pass tells, the
// short one the visit — and every kindred flower in the box around the
// flipped one still carrying the state it left books its own visit out.
// vanilla: EyeblossomBlock.randomTick, tick, tryChangingState

// eyeblossomStateOf reads the one state the named eyeblossom block carries.
func eyeblossomStateOf(t *testing.T, name string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, nil)
	if !ok {
		t.Fatalf("the registry has no %s", name)
	}
	return state
}

// eyeblossomBed sets the soil a flower stands on, the flat world's stone
// carrying none of the supports_vegetation the bush's own shape check asks
// of its block below.
func eyeblossomBed(t *testing.T, w *World, p *Physics, x, y, z int32) {
	t.Helper()
	setBlock(t, w, p, x, y-1, z, registry.MustState("minecraft:dirt", nil))
}

// bookedEyeblossom finds the visit a position waits for and reports when it
// falls due, or reports none.
func bookedEyeblossom(p *Physics, x, y, z int32) (int64, bool) {
	for _, next := range p.queue {
		if next.pos.x == x && next.pos.y == y && next.pos.z == z {
			return next.due, true
		}
	}
	return 0, false
}

// TestAnEyeblossomRandomTickFlipsItByTheHour checks the die's own flip: a
// closed flower opens under a night clock and an open one closes under a day
// clock, each told aloud with the long sound of the state it takes, and the
// hour's own state is a die spent — no flip, no sound, no booking.
func TestAnEyeblossomRandomTickFlipsItByTheHour(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	closed := eyeblossomStateOf(t, "minecraft:closed_eyeblossom")
	open := eyeblossomStateOf(t, "minecraft:open_eyeblossom")

	// Night opens it.
	w.SetClockTime(OverworldClock, 14000)
	eyeblossomBed(t, w, p, 0, GroundLevel, 0)
	setBlock(t, w, p, 0, GroundLevel, 0, closed)
	p.now = clock
	var changes []BlockChange
	p.eyeblossomRandomTick(0, GroundLevel, 0, closed, &changes)
	if w.BlockAt(0, GroundLevel, 0) != open {
		t.Fatal("the closed flower stands closed after a night die, want it open")
	}
	if len(changes) != 1 || changes[0].State != open {
		t.Fatalf("the flip recorded %d changes, want the one open write", len(changes))
	}
	told := p.TakeRandomEffects()
	if len(told) != 1 {
		t.Fatalf("the flip told %d sounds, want the one", len(told))
	}
	if told[0].SoundID.Name() != "minecraft:block.eyeblossom.open_long" ||
		told[0].X != 0 || told[0].Y != GroundLevel || told[0].Z != 0 ||
		told[0].Volume != 1 || told[0].Pitch != 1 {
		t.Errorf("the open flip told %+v, want the long open sound at the flower", told[0])
	}
	if _, booked := bookedEyeblossom(p, 0, GroundLevel, 0); booked {
		t.Error("the flipped flower waits for a visit of its own, want none")
	}

	// Day closes it again.
	w.SetClockTime(OverworldClock, 2000)
	p.eyeblossomRandomTick(0, GroundLevel, 0, open, &changes)
	if w.BlockAt(0, GroundLevel, 0) != closed {
		t.Fatal("the open flower stands open after a day die, want it closed")
	}
	told = p.TakeRandomEffects()
	if len(told) != 1 || told[0].SoundID.Name() != "minecraft:block.eyeblossom.close_long" {
		t.Errorf("the close flip told %+v, want the long close sound", told)
	}

	// The hour's own state is a spent die.
	w.SetClockTime(OverworldClock, 2000)
	p.eyeblossomRandomTick(0, GroundLevel, 0, closed, &changes)
	if w.BlockAt(0, GroundLevel, 0) != closed || len(p.randomEffects) != 0 || len(p.queue) != 0 {
		t.Error("a day die on a closed flower moved something, want it spent")
	}
	w.SetClockTime(OverworldClock, 14000)
	p.eyeblossomRandomTick(0, GroundLevel, 0, closed, &changes)
	p.TakeRandomEffects()
	p.eyeblossomRandomTick(0, GroundLevel, 0, open, &changes)
	if w.BlockAt(0, GroundLevel, 0) != open || len(p.randomEffects) != 0 || len(p.queue) != 0 {
		t.Error("a night die on an open flower moved something, want it spent")
	}
}

// TestAnEyeblossomInTheNetherAndTheEndNeverFlips checks the dimension gate:
// the nether and the end run no timeline that carries the eyeblossom-open
// track, so their attribute stays at its default and a flower of either
// state keeps it, whatever hour the overworld's clock reads.
func TestAnEyeblossomInTheNetherAndTheEndNeverFlips(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		label  string
		nether bool
		gen    Generator
	}{
		{"nether", true, NewNetherGenerator(7)},
		{"end", false, NewEndGenerator(7)},
	} {
		w := NewDimensionWorld(t.TempDir(), tc.nether, tc.gen)
		pos := ChunkPos{X: 0, Z: 0}
		w.Acquire(pos)
		p := NewPhysics(w)
		w.SetBlock(0, 63, 0, registry.MustState("minecraft:grass_block", nil))
		for _, name := range []string{"minecraft:closed_eyeblossom", "minecraft:open_eyeblossom"} {
			state := eyeblossomStateOf(t, name)
			for _, hour := range []int64{14000, 2000} {
				w.SetBlock(0, 64, 0, state)
				w.SetClockTime(OverworldClock, hour)
				p.now = 100
				var changes []BlockChange
				p.eyeblossomRandomTick(0, 64, 0, state, &changes)
				if w.BlockAt(0, 64, 0) != state {
					t.Errorf("the %s's %s flipped on a die at %d, want it standing", tc.label, name, hour)
				}
				if len(p.randomEffects) != 0 || len(p.queue) != 0 {
					t.Errorf("the %s's %s told effects or booked visits, want none", tc.label, name)
				}
				p.schedule(0, 64, 0, p.now+1)
				p.Tick(p.now + 1)
				if w.BlockAt(0, 64, 0) != state {
					t.Errorf("the %s's %s flipped on its booked visit at %d, want it standing", tc.label, name, hour)
				}
			}
		}
		w.Release(pos)
		w.Close()
	}
}

// distOf reads the delay's span a cascade offset rolls in, the whole
// dist*5 to dist*10 ticks the distance's five to ten per block truncates
// to.
func distOf(d [3]int32) [2]int64 {
	dist := math.Sqrt(float64(d[0]*d[0] + d[1]*d[1] + d[2]*d[2]))
	return [2]int64{int64(dist * 5.0), int64(dist * 10.0)}
}
