package server

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkHopperRing measures a tick of 64 closed, 16-hopper rings.
func BenchmarkHopperRing(b *testing.B) {
	logger := slog.Default()
	slog.SetDefault(slog.New(slog.DiscardHandler))
	b.Cleanup(func() { slog.SetDefault(logger) })
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = b.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		b.Fatal(err)
	}
	w := s.world
	var holds []world.ChunkPos
	for x := range int32(2) {
		for z := range int32(2) {
			pos := world.ChunkPos{X: x, Z: z}
			w.Acquire(pos)
			holds = append(holds, pos)
		}
	}
	b.Cleanup(func() {
		// The idle observer has no connection to close during shutdown.
		delete(s.clients, uuid.Nil)
		for _, pos := range holds {
			w.Release(pos)
		}
		s.Shutdown()
	})
	// Exercise notification discovery with a player present and no window open.
	s.clients[uuid.Nil] = &client{}
	// A closed path visiting all 16 cells of a 4x4 square, folding inward
	// because its outer perimeter alone has only 12 cells.
	ring := [16][2]int32{
		{0, 0}, {1, 0}, {2, 0}, {3, 0},
		{3, 1}, {2, 1}, {1, 1}, {1, 2},
		{2, 2}, {3, 2}, {3, 3}, {2, 3},
		{1, 3}, {0, 3}, {0, 2}, {0, 1},
	}
	cobble, ok := registry.ItemID("minecraft:cobblestone")
	if !ok {
		b.Fatal("missing cobblestone")
	}
	var hoppers [64 * 16]*world.BlockEntity
	for r := range 64 {
		for i, pos := range ring {
			next := ring[(i+1)%len(ring)]
			facing := "north"
			switch {
			case next[0] > pos[0]:
				facing = "east"
			case next[0] < pos[0]:
				facing = "west"
			case next[1] > pos[1]:
				facing = "south"
			}
			x, z := int32(r%8)*4+pos[0], int32(r/8)*4+pos[1]
			w.SetBlock(x, 64, z, registry.MustState("minecraft:hopper", map[string]string{"facing": facing, "enabled": "true"}))
			be := w.EnsureBlockEntity(x, 64, z, world.KindHopper)
			be.Put(0, protocol.ItemStack{ItemID: cobble, Count: 1})
			hoppers[r*len(ring)+i] = be
		}
	}
	w.FlushLighting()
	b.ReportAllocs()
	b.ResetTimer()
	for age := range b.N {
		for _, be := range hoppers {
			s.tickHopper(w, be, int64(age))
		}
		s.dimensionOfWorld(w).physics.Tick(int64(age))
	}
	b.StopTimer()
}

// BenchmarkChainedHoppersWithChests measures blocked, empty and transferring
// chains, including the block-entity dispatch and container wake drain.
func BenchmarkChainedHoppersWithChests(b *testing.B) {
	const side = int32(64)
	s, w := newBlockBenchServer(b, side/16)
	for x := range side/16 + 1 {
		for z := range side / 16 {
			b.Cleanup(w.HoldEntityTicking(world.ChunkPos{X: x, Z: z}))
		}
	}
	hopper := registry.MustState("minecraft:hopper", map[string]string{"facing": "east", "enabled": "true"})
	chest := registry.BlockChest.DefaultState()
	cobble := protocol.ItemStack{ItemID: int32(registry.ItemCobblestone), Count: 64}
	fill := func(be *world.BlockEntity) {
		for i := range be.Len() {
			be.Put(i, cobble)
		}
	}
	var sources, sinks []*world.BlockEntity
	for z := range side {
		for x := range side {
			w.SetBlock(x, 64, z, hopper)
			be := w.EnsureBlockEntity(x, 64, z, world.KindHopper)
			switch {
			case z%4 == 0:
				fill(be)
			case z%4 == 1:
				w.SetBlock(x, 65, z, chest)
				w.EnsureBlockEntity(x, 65, z, world.KindChest)
			default:
				// Prime each hopper so the chain transfers before the first
				// item from the source reaches its far end.
				be.Put(0, protocol.ItemStack{ItemID: cobble.ItemID, Count: 1})
				if x == 0 {
					w.SetBlock(x, 65, z, chest)
					source := w.EnsureBlockEntity(x, 65, z, world.KindChest)
					fill(source)
					sources = append(sources, source)
				}
			}
		}
		w.SetBlock(side, 64, z, chest)
		be := w.EnsureBlockEntity(side, 64, z, world.KindChest)
		if z%4 == 0 {
			fill(be)
		} else if z%4 >= 2 {
			sinks = append(sinks, be)
		}
	}
	w.FlushLighting()
	physics := s.dimensionOfWorld(w).physics
	tick := func(age int64) {
		w.TickTime()
		s.tickBlockEntities()
		physics.Tick(age)
	}
	const warmup = 10
	for i := range warmup {
		tick(int64(i + 1))
	}
	for _, sink := range sinks {
		if sink.FilledSlots() == 0 {
			b.Fatal("the chain did not feed its sink during warmup")
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		// Renew the stock before the source or sink runs out of space.
		if i > 0 && i%(512*int(world.HopperMoveInterval)) == 0 {
			b.StopTimer()
			for j, source := range sources {
				fill(source)
				for slot := range sinks[j].Len() {
					sinks[j].Put(slot, protocol.ItemStack{})
				}
			}
			b.StartTimer()
		}
		tick(int64(i + warmup + 1))
	}
}

// BenchmarkHopperCachedFailedPull measures a tick of an enabled, empty hopper whose pull
// from an unchanged empty container above already failed once.
func BenchmarkHopperCachedFailedPull(b *testing.B) {
	hopper := registry.MustState("minecraft:hopper", map[string]string{"facing": "down", "enabled": "true"})
	chest := func(typ string) uint16 {
		return registry.MustState("minecraft:chest", map[string]string{"facing": "north", "type": typ})
	}
	cases := []struct {
		name  string
		build func(w *world.World)
		pair  bool
	}{
		{"single_chest", func(w *world.World) {
			w.SetBlock(1, 65, 1, chest("single"))
			w.EnsureBlockEntity(1, 65, 1, world.KindChest)
		}, false},
		{"double_chest", func(w *world.World) {
			w.SetBlock(1, 65, 1, chest("left"))
			w.SetBlock(2, 65, 1, chest("right"))
			w.EnsureBlockEntity(1, 65, 1, world.KindChest)
			w.EnsureBlockEntity(2, 65, 1, world.KindChest)
		}, true},
		{"composter", func(w *world.World) {
			w.SetBlock(1, 65, 1, registry.MustState("minecraft:composter", map[string]string{"level": "0"}))
		}, false},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			s, w := newBlockBenchServer(b, 1)
			w.SetBlock(1, 64, 1, hopper)
			be := w.EnsureBlockEntity(1, 64, 1, world.KindHopper)
			c.build(w)
			w.FlushLighting()
			age := int64(0)
			s.tickHopper(w, be, age)
			if c.pair && be.HopperTargets.Pull.Entities[1] == nil {
				b.Fatal("double chest did not resolve")
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				age++
				s.tickHopper(w, be, age)
			}
		})
	}
}

// BenchmarkHopperItemBox measures hopper item-box queries with N unrelated
// items in distant chunks.
func BenchmarkHopperItemBox(b *testing.B) {
	stone := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
	for _, n := range []int{0, 1024, 8192} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			s, w := newBlockBenchServer(b, 1)
			s.spawnItem(w, stone, 2, 65, 2, 0, 0, 0, 0)
			for i := range n {
				s.spawnItem(w, stone, float64(100+i%64), 65, 2, 0, 0, 0, 0)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				got := s.itemEntitiesIn(w, 1.5, 64.5, 1.5, 2.5, 66, 2.5)
				if len(got) != 1 {
					b.Fatalf("itemEntitiesIn returned %d items, want 1", len(got))
				}
			}
			b.StopTimer()
		})
	}
}

// BenchmarkHopperItemBoxMany measures a hopper item-box query that finds N
// items straddling a chunk border, so the section ordering sorts them.
func BenchmarkHopperItemBoxMany(b *testing.B) {
	stone := protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 1}
	for _, n := range []int{2, 16, 64} {
		b.Run(fmt.Sprintf("N=%d", n), func(b *testing.B) {
			s, w := newBlockBenchServer(b, 1)
			for i := range n {
				// Alternate sides of the x=0 chunk border inside the box.
				x := 0.2 + 0.5*float64(i%2)
				if i%2 == 0 {
					x = -x
				}
				s.spawnItem(w, stone, x, 65, 0.5, 0, 0, 0, 0)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				got := s.itemEntitiesIn(w, -1, 64.5, 0, 1, 66, 1)
				if len(got) != n {
					b.Fatalf("itemEntitiesIn returned %d items, want %d", len(got), n)
				}
			}
			b.StopTimer()
		})
	}
}
