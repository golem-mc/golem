package entity

import (
	"runtime"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// pathTickFixture makes a cow take each kind of ground search while following wheat.
func pathTickFixture(t testing.TB) (Mobs, World, []Player) {
	t.Helper()
	w := newTestWorld()
	floor(w, -2, -2, 10, 22, 63, defaultState(t, "minecraft:stone"))
	mobs := Mobs{NewMob(TypeCow, 1, 0.5, 64, 0.5, 1), NewMob(TypeCow, 2, 0.5, 64, 20.5, 2)}
	wheat := protocol.ItemStack{ItemID: int32(registry.ItemWheat), Count: 1}
	players := []Player{
		{EntityID: 3, X: 3.5, Y: 64, Z: 0.5, Huntable: true, HeldItem: wheat},
		{EntityID: 4, X: 8.5, Y: 64, Z: 20.5, Huntable: true, HeldItem: wheat},
	}
	return mobs, w, players
}

func pathTick(mobs *Mobs, w World, players []Player, events []MobEvent, paths *MobPathScratch) []MobEvent {
	for i, m := range *mobs {
		m.X, m.Y, m.Z = 0.5, 64, players[i].Z
		m.VX, m.VY, m.VZ = 0, 0, 0
		m.stopSeeking()
	}
	// TemptGoal runs on the full goal pass, once every two ticks.
	for range 2 {
		events = mobs.TickInto(events, w, players, nil, nil, nil, paths)
	}
	return events
}

func TestMobTickReusesPathMemoryAcrossGC(t *testing.T) {
	mobs, w, players := pathTickFixture(t)
	var paths MobPathScratch
	events := pathTick(&mobs, w, players, nil, &paths)
	for i := range 3 {
		// Two collections evict scratches that are reachable only from a sync.Pool.
		runtime.GC()
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		events = pathTick(&mobs, w, players, events, &paths)
		runtime.ReadMemStats(&after)
		for j, m := range mobs {
			p := players[j]
			if len(m.nav.path) == 0 || m.nav.end != cellAt(p.X, p.Y, p.Z) {
				t.Fatalf("iteration %d: cow %d did not plan a path to its wheat holder", i, j)
			}
		}
		// One new 33-cubed cell array alone costs about 2 MiB.
		if allocated := after.TotalAlloc - before.TotalAlloc; allocated >= 1<<20 {
			t.Fatalf("iteration %d: warm mob ticks allocated %d bytes after GC, want under 1 MiB", i, allocated)
		}
	}
}

// BenchmarkMobsTickPathScratchGC runs sixteen herd ticks between double GCs.
func BenchmarkMobsTickPathScratchGC(b *testing.B) {
	mobs, w, players := pathTickFixture(b)
	var paths MobPathScratch
	events := pathTick(&mobs, w, players, nil, &paths)
	runtime.GC()
	runtime.GC()
	before := pathPrepareAllocs()
	b.ReportAllocs()
	for b.Loop() {
		runtime.GC()
		runtime.GC()
		for range 8 {
			events = pathTick(&mobs, w, players, events, &paths)
		}
	}
	runtime.GC()
	runtime.GC()
	b.ReportMetric(float64(pathPrepareAllocs()-before)/float64(b.N), "sampled-prepare-allocs/op")
}

func pathPrepareAllocs() int64 {
	n, _ := runtime.MemProfile(nil, true)
	for {
		records := make([]runtime.MemProfileRecord, n)
		var ok bool
		n, ok = runtime.MemProfile(records, true)
		if !ok {
			continue
		}
		var allocs int64
		for _, r := range records[:n] {
			frames := runtime.CallersFrames(r.Stack())
			for {
				frame, more := frames.Next()
				if frame.Function == "github.com/golem-mc/golem/internal/server/entity.(*pathScratch).prepare" {
					allocs += r.AllocObjects
					break
				}
				if !more {
					break
				}
			}
		}
		return allocs
	}
}
