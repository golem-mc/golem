package server

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// spawnBenchItems lays trackerBenchBodies stacks at rest on the ground, each
// of its own item so none merge, that never despawn and are never picked up.
func spawnBenchItems(b *testing.B, s *Server, w *world.World) {
	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		b.Fatal("no stone")
	}
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		it := s.spawnItem(w, protocol.ItemStack{ItemID: stone + int32(i), Count: 1},
			x, float64(world.GroundLevel), z, 0, 0, 0, -1)
		it.item.Age = -1 << 30
	}
}

// spawnBenchStands stands trackerBenchBodies armor stands on the ground.
func spawnBenchStands(s *Server, w *world.World) {
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		s.registerArmorStand(s.newArmorStandEntity(w, x, float64(world.GroundLevel), z))
	}
}

// BenchmarkTickItemsTracked runs the whole item tick over stacks at rest,
// every one watched by every player.
func BenchmarkTickItemsTracked(b *testing.B) {
	benchTickItemsTracked(b, false)
}

// BenchmarkTickItemsTrackedTicketed includes gate construction and eligibility
// checks with eight viewers and two hundred active items.
func BenchmarkTickItemsTrackedTicketed(b *testing.B) {
	benchTickItemsTracked(b, true)
}

func benchTickItemsTracked(b *testing.B, ticketed bool) {
	s, w := newTrackerBenchServer(b)
	if ticketed {
		w.AddTicket(world.ChunkPos{}, 2)
		b.Cleanup(func() { w.RemoveTicket(world.ChunkPos{}, 2) })
	}
	spawnBenchItems(b, s, w)
	for range 40 {
		s.tickItems()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickItems()
	}
	b.StopTimer()
	if n := len(s.items); n != trackerBenchBodies {
		b.Fatalf("%d items after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkTickItemsDestroyed runs the item tick over trackerBenchBodies
// shulker boxes of three stacks each, standing in lava with a last point of
// health, so every one is destroyed and spills its contents.
func BenchmarkTickItemsDestroyed(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	lava := registry.MustState("minecraft:lava", nil)
	box, _ := registry.ItemID("minecraft:shulker_box")
	ingot, _ := registry.ItemID("minecraft:netherite_ingot")
	contents := []protocol.ItemStack{{ItemID: ingot, Count: 1}, {ItemID: ingot + 1, Count: 1}, {ItemID: ingot + 2, Count: 1}}
	for i := range trackerBenchBodies {
		x, z := trackerBenchSpot(i)
		if !w.SetBlock(int32(math.Floor(x)), world.GroundLevel, int32(math.Floor(z)), lava) {
			b.Fatal("the world did not take the lava")
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		s.items = s.items[:0]
		for i := range trackerBenchBodies {
			x, z := trackerBenchSpot(i)
			it := s.spawnItem(w, protocol.ItemStack{ItemID: box, Count: 1, Container: contents},
				x, float64(world.GroundLevel)+0.5, z, 0, 0, 0, -1)
			it.item.Health = 1
		}
		b.StartTimer()
		s.tickItems()
		b.StopTimer()
		if n := len(s.items); n != trackerBenchBodies*len(contents) {
			b.Fatalf("%d items after the tick, want %d spilled stacks", n, trackerBenchBodies*len(contents))
		}
		b.StartTimer()
	}
}

// BenchmarkTickArmorStandsTracked runs the whole armor stand tick over
// stands at rest, every one watched by every player.
func BenchmarkTickArmorStandsTracked(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	spawnBenchStands(s, w)
	for range 40 {
		s.tickArmorStands()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickArmorStands()
	}
	b.StopTimer()
	if n := len(s.armorStands); n != trackerBenchBodies {
		b.Fatalf("%d armor stands after the run, want %d", n, trackerBenchBodies)
	}
}

// BenchmarkFrozenTrackingItems runs the frozen world's item tracking pass,
// with every player in range of every stack.
func BenchmarkFrozenTrackingItems(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	spawnBenchItems(b, s, w)
	watchers := s.trackingWatchers()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		frozenItemsPass(s, watchers)
	}
}

// BenchmarkFrozenTrackingArmorStands runs the frozen world's armor stand
// tracking pass, with every player in range of every stand.
func BenchmarkFrozenTrackingArmorStands(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	spawnBenchStands(s, w)
	watchers := s.trackingWatchers()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		frozenStandsPass(s, watchers)
	}
}

// frozenItemsPass and frozenStandsPass are what tick.go runs for each kind
// on a frozen world.
func frozenItemsPass(s *Server, watchers []trackingWatcher) { itemTracker.frozenPass(s, watchers) }

func frozenStandsPass(s *Server, watchers []trackingWatcher) { s.tickArmorStandTracking(watchers) }

// BenchmarkItemPairing builds what shows each of trackerBenchBodies stacks
// at rest to a watcher entering view: the spawn and the metadata.
func BenchmarkItemPairing(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	spawnBenchItems(b, s, w)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		for _, it := range s.items {
			itemPairing(it)
		}
	}
}

// BenchmarkTickItemsMoving runs the whole item tick over 2000 stacks of 50
// item IDs two blocks apart on the ground, none close enough to merge, each put back 0.1
// short of a block edge and pushed 0.3 along X before every tick, so each one
// crosses a block every tick and its merge scan runs every second tick.
func BenchmarkTickItemsMoving(b *testing.B) {
	const n, row = 2000, 45
	s, w := newTrackerBenchServer(b)
	spot := func(i int) (x, z float64) { return float64(i%row)*2 - row + 0.9, float64(i/row)*2 - row + 0.5 }
	for i := range n {
		x, z := spot(i)
		it := s.spawnItem(w, protocol.ItemStack{ItemID: int32(registry.ItemStone) + int32(i%50), Count: 1},
			x, float64(world.GroundLevel)+entity.ItemHalfSize, z, 0, 0, 0, -1)
		it.item.Age = -1 << 30
	}
	push := func() {
		for i, it := range s.items {
			it.item.X, _ = spot(i)
			it.item.VX = 0.3
		}
	}
	for range 4 {
		push()
		s.tickItems()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		push()
		s.tickItems()
	}
	b.StopTimer()
	if got := len(s.items); got != n {
		b.Fatalf("%d items after the run, want %d", got, n)
	}
}

// BenchmarkTickMobsBesideItems times the mob tick of a few resting cows in a
// world holding trackerBenchBodies item stacks, which it has no business with.
func BenchmarkTickMobsBesideItems(b *testing.B) {
	s, w := newTrackerBenchServer(b)
	spawnBenchItems(b, s, w)
	dim := s.allDims()[0]
	for i := range 10 {
		id := s.spawnMobInDim(dim, entity.TypeCow, float64(4+i*2), float64(world.GroundLevel), 0, int64(i), nil)
		s.mobsMu.Lock()
		ent := s.mobByEntityIDLocked(w, id)
		ent.mob.NoAI = true
		ent.mob.Persistence = true
		s.mobsMu.Unlock()
	}
	for range 5 {
		s.tickMobs()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickMobs()
	}
}
