package world

import (
	"testing"
)

// uncachedSlopeDistance keeps the individual fluid gates as an oracle for the
// per-cell classification and wall shortcuts.
func uncachedSlopeDistance(x, z int32, pass int, fromDX, fromDZ int32, b fluidBehaviour, c *spreadContext) int {
	lowest := 1000
	state := c.state(x, z)
	for _, d := range horizontal4 {
		if d[0] == fromDX && d[1] == fromDZ {
			continue
		}
		nx, nz := x+d[0], z+d[1]
		ns := c.state(nx, nz)
		axis, positive := faceOf(d[0], d[1])
		if !canMaybePassThrough(c.kind, state, axis, positive, ns, fluidHolding(ns)) || !canHoldSpecific(ns, c.flowingType) {
			continue
		}
		if c.isHole(nx, nz) {
			return pass
		}
		if pass < b.slopeFindDistance {
			if v := uncachedSlopeDistance(nx, nz, pass+1, -d[0], -d[1], b, c); v < lowest {
				lowest = v
			}
		}
	}
	return lowest
}

// TestLavaSpreadReachByDimension checks the fast-lava drop-off and slope
// distance: lava reaches 7 blocks where it runs fast, 3 elsewhere.
func TestLavaSpreadReachByDimension(t *testing.T) {
	for _, tc := range []struct {
		name     string
		fast     bool
		reach    int32
		wantNone int32
	}{{"normal", false, 3, 4}, {"fast", true, 7, 8}} {
		t.Run(tc.name, func(t *testing.T) {
			w, p := newPhysicsWorld(t)
			p.fastLava = tc.fast
			var clock int64
			setBlock(t, w, p, 0, GroundLevel, 0, lavaByLevel[0])
			tickToQuiet(t, p, &clock, 800)
			if got := fluidHolding(w.BlockAt(tc.reach, GroundLevel, 0)); got.kind != fluidLava {
				t.Errorf("no lava at +%d", tc.reach)
			}
			assertAir(t, w, tc.wantNone, GroundLevel, 0, "lava's reach ends")
		})
	}
}

// newFluidBookingWorld is a flat world with the one chunk holding (0, 0).
func newFluidBookingWorld(t *testing.T) (*World, *Physics) {
	t.Helper()
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	t.Cleanup(func() { w.Close() })
	w.Acquire(ChunkPos{})
	return w, NewPhysics(w)
}

// TestClearTicksInForgetsFluidBookings checks that a cleared fluid visit leaves
// no booking behind to block the next one.
func TestClearTicksInForgetsFluidBookings(t *testing.T) {
	w, p := newFluidBookingWorld(t)
	w.SetBlock(0, GroundLevel, 0, waterByLevel[0])
	p.scheduleFluid(0, GroundLevel, 0)
	if len(p.fluidBooked) != 1 {
		t.Fatalf("%d fluid bookings before the clear, want 1", len(p.fluidBooked))
	}
	p.ClearTicksIn(0, GroundLevel, 0, 0, GroundLevel, 0)
	if len(p.fluidBooked) != 0 {
		t.Errorf("%d fluid bookings after the clear, want none", len(p.fluidBooked))
	}
}

// TestSavedFluidVisitsReloadWithTheirType saves a lava and a water visit at one
// cell, reloads them while the cell holds water, and expects both bookings back.
// vanilla: LevelChunkTicks.<init>, ServerLevel.tickFluid
func TestSavedFluidVisitsReloadWithTheirType(t *testing.T) {
	w, p := newFluidBookingWorld(t)
	setBlock(t, w, p, 0, GroundLevel, 0, lavaByLevel[0])
	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	saved := p.chunkTicks(ChunkPos{}).fluid
	if len(saved) != 2 {
		t.Fatalf("%d fluid visits saved, want lava and water", len(saved))
	}
	p.ClearTicksIn(0, GroundLevel, 0, 0, GroundLevel, 0)
	if len(p.fluidBooked) != 0 {
		t.Fatalf("%d bookings after the clear", len(p.fluidBooked))
	}
	for _, st := range saved {
		w.pendingLoaded = append(w.pendingLoaded, loadedTick{savedTick: st, fluid: true})
	}
	p.mu.Lock()
	p.adoptLoadedTicks()
	p.mu.Unlock()
	if len(p.fluidBooked) != 2 {
		t.Errorf("%d fluid bookings after the reload, want 2", len(p.fluidBooked))
	}
}

var lavaFluidType = fluidTypeOf(fluidHolding(lavaByLevel[0]))

// TestFluidBookingKeepsSavedVisit books lava at a cell whose saved lava visit
// has not been adopted yet; the saved visit's due tick stands.
// vanilla: LevelChunkTicks.<init>, LevelChunkTicks.schedule
func TestFluidBookingKeepsSavedVisit(t *testing.T) {
	w, p := newFluidBookingWorld(t)
	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	w.pendingLoaded = append(w.pendingLoaded, loadedTick{
		id: lavaFluidType.name(), y: GroundLevel, delay: 18, fluid: true})
	p.mu.Lock()
	p.bookFluid(0, GroundLevel, 0, fluidHolding(lavaByLevel[0]))
	p.mu.Unlock()
	var due []int64
	for _, q := range p.queue {
		if q.fluid == lavaFluidType {
			due = append(due, q.due)
		}
	}
	if len(due) != 1 || due[0] != p.now+18 {
		t.Errorf("lava visits due %v, want one at %d", due, p.now+18)
	}
}

// TestSavedFluidVisitIsNotAPistonLanding loads a fluid visit saved at the
// piston landing's priority; it is no landing, and a clear removes it.
// vanilla: TickPriority.VERY_LOW, SavedTick.pack
func TestSavedFluidVisitIsNotAPistonLanding(t *testing.T) {
	w, p := newFluidBookingWorld(t)
	setBlock(t, w, p, 0, GroundLevel, 0, waterByLevel[0])
	p.ClearTicksIn(0, GroundLevel, 0, 0, GroundLevel, 0)
	w.pendingLoaded = append(w.pendingLoaded, loadedTick{
		id: waterFluidTypeName(), y: GroundLevel, delay: 3, priority: tickPriorityFinalize, fluid: true})
	p.mu.Lock()
	p.adoptLoadedTicks()
	p.mu.Unlock()
	if len(p.finalizeSeq) != 0 {
		t.Errorf("%d piston landings registered", len(p.finalizeSeq))
	}
	p.ClearTicksIn(0, GroundLevel, 0, 0, GroundLevel, 0)
	if len(p.queue) != 0 || len(p.fluidBooked) != 0 {
		t.Errorf("%d visits and %d bookings survive the clear", len(p.queue), len(p.fluidBooked))
	}
}

func waterFluidTypeName() string { return fluidTypeOf(fluidHolding(waterByLevel[0])).name() }

// TestStaleFluidVisitsSpendTheBudget queues a full budget of stale lava visits
// ahead of one water visit, which then waits for the next tick.
// vanilla: LevelTicks.collectTicks
func TestStaleFluidVisitsSpendTheBudget(t *testing.T) {
	w, p := newFluidBookingWorld(t)
	const top = 300
	setBlock(t, w, p, 0, top, 0, waterByLevel[0])
	p.ClearTicksIn(0, top, 0, 0, top, 0)
	p.mu.Lock()
	for i := range maxFluidTicksPerTick {
		p.seq++
		p.queue.push(scheduledTick{pos: blockKey{int32(i & 15), int32(i>>8) - 64, int32(i >> 4 & 15)},
			due: p.now + 1, seq: p.seq, fluid: lavaFluidType})
	}
	p.mu.Unlock()
	p.mu.Lock()
	p.bookTick(0, top, 0, fluidTypeOf(fluidHolding(waterByLevel[0])), p.now+1, tickPriorityNormal)
	p.mu.Unlock()
	p.Tick(p.now + 1)
	assertAir(t, w, 0, top-1, 0, "the water below waits for the next tick")
}
