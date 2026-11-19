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

// TestAnEyeblossomFlipBooksItsKindDistTicksOut checks the cascade: every
// kindred flower in the box around the flipped one still carrying the state
// it left books its own visit, the delay the distance's five to ten ticks
// per block, and an open kindred, a flower past the box and a bystander
// none.
func TestAnEyeblossomFlipBooksItsKindDistTicksOut(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	closed := eyeblossomStateOf(t, "minecraft:closed_eyeblossom")
	open := eyeblossomStateOf(t, "minecraft:open_eyeblossom")
	w.SetClockTime(OverworldClock, 14000)

	// The kindred around the flower about to flip, at each of the box's
	// three depths, the open kindred, the one past the box and the
	// bystander beside them. None stands a block above the flower about
	// to flip: a flower's support is the soil under it, and the one
	// there is the flower itself.
	kindred := [][3]int32{{1, 0, 0}, {3, 0, 0}, {0, 1, 1}, {0, -2, 0}}
	for _, d := range kindred {
		eyeblossomBed(t, w, p, d[0], GroundLevel+d[1], d[2])
		setBlock(t, w, p, d[0], GroundLevel+d[1], d[2], closed)
	}
	eyeblossomBed(t, w, p, 0, GroundLevel+2, 0)
	setBlock(t, w, p, 0, GroundLevel+2, 0, open)
	// Seven blocks out: past every flower's box, since each visit runs a
	// box of its own, and so only distance from them all keeps it still.
	eyeblossomBed(t, w, p, 7, GroundLevel, 0)
	setBlock(t, w, p, 7, GroundLevel, 0, closed)
	setBlock(t, w, p, 2, GroundLevel, 0, registry.MustState("minecraft:dirt", nil))
	eyeblossomBed(t, w, p, 0, GroundLevel, 0)
	setBlock(t, w, p, 0, GroundLevel, 0, closed)
	p.now = clock
	var changes []BlockChange
	p.eyeblossomRandomTick(0, GroundLevel, 0, closed, &changes)

	for _, d := range kindred {
		due, booked := bookedEyeblossom(p, d[0], GroundLevel+d[1], d[2])
		if !booked {
			t.Fatalf("the kindred at (%d,%d,%d) waits for no visit", d[0], d[1], d[2])
		}
		dist := distOf(d)
		if due-p.now < dist[0] || due-p.now > dist[1] {
			t.Errorf("the kindred at (%d,%d,%d) falls due %d ticks out, want %d..%d",
				d[0], d[1], d[2], due-p.now, dist[0], dist[1])
		}
	}
	for _, gone := range [][3]int32{{0, 2, 0}, {7, 0, 0}, {2, 0, 0}} {
		if _, booked := bookedEyeblossom(p, gone[0], GroundLevel+gone[1], gone[2]); booked {
			t.Errorf("the block at (%d,%d,%d) waits for a visit, want none", gone[0], gone[1], gone[2])
		}
	}

	// The visits answer themselves: the kindred open by the hour's word,
	// the short sound of the state each takes riding the wake.
	var told []Effect
	until := p.now + 40
	for clock <= until {
		clock++
		_, _, _, fx := p.Tick(clock)
		told = append(told, fx...)
	}
	for _, d := range kindred {
		if w.BlockAt(d[0], GroundLevel+d[1], d[2]) != open {
			t.Errorf("the kindred at (%d,%d,%d) stands closed after its visit, want it open",
				d[0], d[1], d[2])
		}
	}
	if w.BlockAt(7, GroundLevel, 0) != closed || w.BlockAt(0, GroundLevel+2, 0) != open {
		t.Error("a flower the cascade does not reach moved, want it standing as it was")
	}
	if len(told) != len(kindred) {
		t.Fatalf("the visits told %d sounds, want %d", len(told), len(kindred))
	}
	for _, effect := range told {
		if effect.SoundID.Name() != "minecraft:block.eyeblossom.open" || effect.Volume != 1 || effect.Pitch != 1 {
			t.Fatalf("a visit told %+v, want the short open sound", effect)
		}
	}
}

// TestAnEyeblossomScheduledTickFlipsItByTheHour checks the booked visit's
// own flip: the same hour's word the die obeys, the short sound of the state
// it takes, and nothing told to the random pass's own sink.
func TestAnEyeblossomScheduledTickFlipsItByTheHour(t *testing.T) {
	w, p := newPhysicsWorld(t)
	var clock int64
	closed := eyeblossomStateOf(t, "minecraft:closed_eyeblossom")
	open := eyeblossomStateOf(t, "minecraft:open_eyeblossom")

	w.SetClockTime(OverworldClock, 14000)
	eyeblossomBed(t, w, p, 0, GroundLevel, 0)
	setBlock(t, w, p, 0, GroundLevel, 0, closed)
	p.now = clock
	p.schedule(0, GroundLevel, 0, p.now+1)
	_, _, _, told := p.Tick(p.now + 1)
	if w.BlockAt(0, GroundLevel, 0) != open {
		t.Fatal("the visit left the closed flower closed at night, want it open")
	}
	if len(told) != 1 || told[0].SoundID.Name() != "minecraft:block.eyeblossom.open" {
		t.Errorf("the visit told %+v, want the short open sound", told)
	}
	if len(p.randomEffects) != 0 {
		t.Errorf("the visit told %d sounds to the random pass, want none", len(p.randomEffects))
	}

	w.SetClockTime(OverworldClock, 2000)
	p.schedule(0, GroundLevel, 0, p.now+1)
	_, _, _, told = p.Tick(p.now + 1)
	if w.BlockAt(0, GroundLevel, 0) != closed {
		t.Fatal("the visit left the open flower open by day, want it closed")
	}
	if len(told) != 1 || told[0].SoundID.Name() != "minecraft:block.eyeblossom.close" {
		t.Errorf("the visit told %+v, want the short close sound", told)
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

// TestAnEyeblossomFlipDrawsATrailParticle checks the particle both paths
// give: one trail from the block's centre, in the colour of the type the
// flower takes, flying up and out for as many ticks as its lifetime rolled.
func TestAnEyeblossomFlipDrawsATrailParticle(t *testing.T) {
	w, p := newPhysicsWorld(t)
	closed := eyeblossomStateOf(t, "minecraft:closed_eyeblossom")
	eyeblossomBed(t, w, p, 0, GroundLevel, 0)
	setBlock(t, w, p, 0, GroundLevel, 0, closed)
	var changes []BlockChange

	check := func(label string, fx []Effect, color int32) {
		t.Helper()
		if len(fx) != 1 || fx[0].Trail == nil {
			t.Fatalf("%s: the flip told %+v, want one effect with a trail", label, fx)
		}
		tr := fx[0].Trail
		if tr.X != 0.5 || tr.Y != float64(GroundLevel)+0.5 || tr.Z != 0.5 {
			t.Errorf("%s: the trail starts at (%v,%v,%v), want the block's centre", label, tr.X, tr.Y, tr.Z)
		}
		if tr.Color != color {
			t.Errorf("%s: the trail's colour is %#x, want %#x", label, tr.Color, color)
		}
		// lifetime in [0.5, 1.5): the rise is (rand+1)*lifetime, the
		// sideways drift (rand-0.5)*lifetime.
		if rise := tr.TargetY - tr.Y; rise < 0.5 || rise >= 3.0 {
			t.Errorf("%s: the trail rises %v, want [0.5, 3)", label, rise)
		}
		if dx := tr.TargetX - tr.X; dx < -0.75 || dx >= 0.75 {
			t.Errorf("%s: the trail drifts %v sideways, want within 0.75", label, dx)
		}
		if tr.Duration < 10 || tr.Duration >= 30 {
			t.Errorf("%s: the trail lasts %d ticks, want [10, 30)", label, tr.Duration)
		}
	}

	w.SetClockTime(OverworldClock, 14000)
	p.eyeblossomRandomTick(0, GroundLevel, 0, closed, &changes)
	check("random tick", p.TakeRandomEffects(), 16545810)

	w.SetClockTime(OverworldClock, 2000)
	p.now = 10
	p.schedule(0, GroundLevel, 0, 11)
	_, _, _, fx := p.Tick(11)
	check("scheduled tick", fx, 0x5F5F5F)
	if w.BlockAt(0, GroundLevel, 0) != closed {
		t.Fatal("the visit left the flower open by day")
	}
}

// TestAPottedEyeblossomFlipsByTheHour checks the pot's random tick: the
// potted flower opens at night and closes by day with the long sound and the
// trail, books no neighbour, and keeps its state in the nether.
func TestAPottedEyeblossomFlipsByTheHour(t *testing.T) {
	w, p := newPhysicsWorld(t)
	closed := registry.MustState("minecraft:potted_closed_eyeblossom", nil)
	open := registry.MustState("minecraft:potted_open_eyeblossom", nil)
	setBlock(t, w, p, 0, GroundLevel, 0, closed)
	setBlock(t, w, p, 1, GroundLevel, 0, closed)
	var changes []BlockChange

	w.SetClockTime(OverworldClock, 14000)
	p.pottedEyeblossomRandomTick(0, GroundLevel, 0, closed, &changes)
	if w.BlockAt(0, GroundLevel, 0) != open {
		t.Fatal("the potted flower stands closed after a night die, want it open")
	}
	told := p.TakeRandomEffects()
	if len(told) != 1 || told[0].SoundID.Name() != "minecraft:block.eyeblossom.open_long" ||
		told[0].Trail == nil || told[0].Trail.Color != 16545810 {
		t.Errorf("the potted flip told %+v, want the long open sound and an open trail", told)
	}
	if len(p.queue) != 0 || w.BlockAt(1, GroundLevel, 0) != closed {
		t.Error("the potted flip woke its neighbour, want no spread")
	}

	p.pottedEyeblossomRandomTick(0, GroundLevel, 0, open, &changes)
	if w.BlockAt(0, GroundLevel, 0) != open || len(p.randomEffects) != 0 {
		t.Error("a night die on an open potted flower moved something, want it spent")
	}
	w.SetClockTime(OverworldClock, 2000)
	p.pottedEyeblossomRandomTick(0, GroundLevel, 0, open, &changes)
	if w.BlockAt(0, GroundLevel, 0) != closed {
		t.Error("the potted flower stands open after a day die, want it closed")
	}
}

// distOf reads the delay's span a cascade offset rolls in, the whole
// dist*5 to dist*10 ticks the distance's five to ten per block truncates
// to.
func distOf(d [3]int32) [2]int64 {
	dist := math.Sqrt(float64(d[0]*d[0] + d[1]*d[1] + d[2]*d[2]))
	return [2]int64{int64(dist * 5.0), int64(dist * 10.0)}
}
