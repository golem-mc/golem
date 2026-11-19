package server

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The fire-spread benchmark: a 32x32 oak-plank floor with oak log pillars
// and oak-leaf canopies standing on it, one fire lit in each of the
// sixteen canopies, and the engine's ticks driven by hand from the first
// sparks until the blaze has run its course. The window holds the ticks
// and the light work they schedule: the light pool is drained inside it,
// so a tick pays for the relight it queues rather than handing it to
// whoever runs after.

// The burn site's box, in world cells: a 32x32 floor at GroundLevel, logs
// and leaves up to burnTop above it.
const (
	burnMinX  = 0
	burnMaxX  = 31
	burnMinZ  = 0
	burnMaxZ  = 31
	burnFloor = world.GroundLevel
	burnTop   = world.GroundLevel + 6
)

// burnTicks is how many engine ticks one burn runs; fireSpreadSeed pins
// the fire's dice, so every burn of the same site plays out the same.
const (
	burnTicks      = 200
	fireSpreadSeed = int64(20260226)
)

// burnStates are the blocks the site is built of, resolved once.
var (
	planksState    = registry.BlockStates["minecraft:oak_planks"]
	logState       = registry.BlockStates["minecraft:oak_log"]
	leavesState, _ = registry.FindState("minecraft:oak_leaves", map[string]string{
		"distance": "7", "persistent": "true", "waterlogged": "false",
	})
	fireSpreadState = registry.MustState("minecraft:fire", nil)
)

// buildBurnSite writes the site's blocks into the world: the plank floor,
// a log pillar with a leaf canopy on it at every cell whose x and z are
// both 4 mod 8, and air everywhere above the floor the builders left.
func buildBurnSite(w *world.World) {
	for y := int32(burnFloor); y <= burnTop; y++ {
		for x := int32(burnMinX); x <= burnMaxX; x++ {
			for z := int32(burnMinZ); z <= burnMaxZ; z++ {
				switch {
				case y == burnFloor:
					w.SetBlock(x, y, z, planksState)
				case y > burnFloor+1 && x%8 == 4 && z%8 == 4:
					w.SetBlock(x, y, z, logState)
				case y > burnFloor+2 && x%8 >= 2 && x%8 <= 6 && z%8 >= 2 && z%8 <= 6 &&
					(x%8 >= 3 && x%8 <= 5 || z%8 >= 3 && z%8 <= 5):
					// The canopy: the 5x5 square around each pillar top
					// minus its four corners, from two above the floor up.
					w.SetBlock(x, y, z, leavesState)
				default:
					w.SetBlock(x, y, z, 0)
				}
			}
		}
	}
}

// newBurnServer starts a server on the flat stone the black-box tests build
// on, holding every chunk the burn and its relights touch, with the site
// built and the held chunks' first light waited out. The tick loop is never
// started: the ticks the benchmark measures are the ones it drives itself.
func newBurnServer(b *testing.B) (*Server, *world.World) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatalf("create server: %v", err)
	}
	s.SetSpawning(false)
	w := s.world

	const holdPad = 16 // the burn's relight boxes reach 15 cells past its own
	var holds []world.ChunkPos
	for cx := world.ChunkOf(float64(burnMinX - holdPad)); cx <= world.ChunkOf(float64(burnMaxX+holdPad)); cx++ {
		for cz := world.ChunkOf(float64(burnMinZ - holdPad)); cz <= world.ChunkOf(float64(burnMaxZ+holdPad)); cz++ {
			holds = append(holds, world.ChunkPos{X: cx, Z: cz})
		}
	}
	for _, pos := range holds {
		w.Acquire(pos)
	}
	b.Cleanup(func() {
		for _, pos := range holds {
			w.Release(pos)
		}
		s.Shutdown()
	})

	buildBurnSite(w)
	// The acquired chunks' first light is still on the worker pool while
	// the site is built; drain it, or the window measures leftover set-up
	// work instead of the burn's own.
	w.FlushLighting()
	return s, w
}

// snapshotBurn reads the site's box back, for the rebuild between burns.
func snapshotBurn(s *Server, w *world.World) []world.BlockChange {
	var snap []world.BlockChange
	for y := int32(burnFloor); y <= burnTop; y++ {
		for x := int32(burnMinX); x <= burnMaxX; x++ {
			for z := int32(burnMinZ); z <= burnMaxZ; z++ {
				snap = append(snap, world.BlockChange{
					X: x, Y: y, Z: z, State: s.BlockAt(x, y, z),
				})
			}
		}
	}
	return snap
}

// rebuildBurn puts the site back as the snapshot found it, so the next burn
// runs against the world the last one started from.
func rebuildBurn(w *world.World, snap []world.BlockChange) {
	for _, ch := range snap {
		if w.BlockAt(ch.X, ch.Y, ch.Z) != ch.State {
			w.SetBlock(ch.X, ch.Y, ch.Z, ch.State)
		}
	}
	// SetBlock marks light cells without queueing a relight, and past 512
	// marks a chunk is in overflow, which sends every later relight down the
	// whole-box path; the install of this relight clears the marks.
	<-w.RelightAreaAsync(burnMinX, burnMinZ, burnMaxX, burnMaxZ)
}

// runBurn lights the fire in the site's middle and drives the engine's ticks
// by hand for burnTicks of them, the physics pass exactly as the tick loop
// runs it, with the site's own light settled before the window (rebuildBurn),
// draining the light pool each tick so the window measures the
// light work the burn itself books against each tick, not a backlog racing
// the ticks after it. It returns the ticks' wall time — light included —
// and the block changes and distinct columns they changed, the columns
// being what each tick hands the light engine on the per-column path, one
// relight each.
func (s *Server) runBurn(w *world.World) (ticks time.Duration, changes, columns int) {
	s.SetPhysicsSeed(fireSpreadSeed)
	// The fires go in the way the server's own writes go: written, then
	// woken, which is what books each fire's first scheduled visit. One
	// canopy leaf beside each trunk is replaced, sixteen lightning-struck
	// trees, so the window measures the blaze at its busiest: a fire
	// spreading over a whole forest floor, not one campfire waking up.
	for _, p := range [][2]int32{{4, 4}, {12, 4}, {20, 4}, {28, 4},
		{4, 12}, {12, 12}, {20, 12}, {28, 12},
		{4, 20}, {12, 20}, {20, 20}, {28, 20},
		{4, 28}, {12, 28}, {20, 28}, {28, 28}} {
		x, y, z := p[0]-1, int32(burnFloor+4), p[1]
		old := w.BlockAt(x, y, z)
		w.SetBlock(x, y, z, fireSpreadState)
		s.blockChanged(w, x, y, z, old)
	}

	seen := make(map[[2]int32]struct{}, 64)
	for range burnTicks {
		start := time.Now()
		s.world.TickTime()
		base, _ := s.world.Time()
		for _, dim := range s.allDims() {
			tickChanges, falls, primes, effects := dim.physics.Tick(base)
			for _, e := range effects {
				s.tellEffect(dim.w, e)
			}
			s.physicsResults(dim, tickChanges, falls, primes, effects)
			for _, d := range dim.physics.TakeDispenses() {
				s.runDispense(dim.w, d)
			}
			changes += len(tickChanges)
			clear(seen)
			for _, ch := range tickChanges {
				seen[[2]int32{ch.X, ch.Z}] = struct{}{}
			}
			columns += len(seen)
		}
		w.FlushLighting() // safe here: nothing but the bench submits to the pool
		ticks += time.Since(start)
	}
	return ticks, changes, columns
}

// BenchmarkFireSpread is the cost of a wooden site burning: 200 engine
// ticks from sixteen lightning-struck trees on a 32x32 plank floor with
// logs and leaves above it, with each tick's light work waited on inside
// the window. The site's light is settled before the window, so it measures
// the burn's incremental relights. Reported per burn: the tick wall time with its light
// included (tick-ms), the block changes per tick the clients are told
// (changes/tick) and the changed columns per tick the light engine is
// handed on the per-column path (columns/tick).
func BenchmarkFireSpread(b *testing.B) {
	s, w := newBurnServer(b)
	snap := snapshotBurn(s, w)
	b.Cleanup(func() {
		// The held chunks were dirtied by burns; flush the queued saves
		// before the cleanup releases them and the temp dir goes.
		w.FlushSaves()
	})
	b.ReportAllocs()
	var (
		ticks   int
		changes int
		columns int
		tickNs  time.Duration
	)
	for range b.N {
		b.StopTimer()
		rebuildBurn(w, snap)
		b.StartTimer()

		tickDur, c, cols := s.runBurn(w)
		ticks += burnTicks
		changes += c
		columns += cols
		tickNs += tickDur
	}
	b.ReportMetric(float64(tickNs.Milliseconds())/float64(ticks), "tick-ms")
	b.ReportMetric(float64(changes)/float64(ticks), "changes/tick")
	b.ReportMetric(float64(columns)/float64(ticks), "columns/tick")
}
