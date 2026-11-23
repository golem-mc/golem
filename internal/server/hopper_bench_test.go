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

// BenchmarkChainedHoppersWithChests measures blocked push chains and empty
// pull chains beside full and empty chests, including the block-entity dispatch.
func BenchmarkChainedHoppersWithChests(b *testing.B) {
	const side = int32(64)
	s, w := newBlockBenchServer(b, side/16)
	hopper := registry.MustState("minecraft:hopper", map[string]string{"facing": "east", "enabled": "true"})
	chest := registry.BlockChest.DefaultState()
	fill := func(be *world.BlockEntity) {
		for i := range be.Len() {
			be.Put(i, protocol.ItemStack{ItemID: int32(registry.ItemCobblestone), Count: 64})
		}
	}
	for z := range side {
		for x := range side {
			w.SetBlock(x, 64, z, hopper)
			be := w.EnsureBlockEntity(x, 64, z, world.KindHopper)
			if z%2 == 0 {
				fill(be)
			} else {
				w.SetBlock(x, 65, z, chest)
				w.EnsureBlockEntity(x, 65, z, world.KindChest)
			}
		}
		w.SetBlock(side, 64, z, chest)
		be := w.EnsureBlockEntity(side, 64, z, world.KindChest)
		if z%2 == 0 {
			fill(be)
		}
	}
	w.FlushLighting()
	for range 10 {
		s.tickBlockEntities()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.tickBlockEntities()
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
