package server

import (
	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"log/slog"
	"testing"
)

func newBlockBenchServer(b *testing.B, chunks int32) (*Server, *world.World) {
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
	s.SetSpawning(false)
	w := s.world
	var holds []world.ChunkPos
	for x := int32(-1); x <= chunks; x++ {
		for z := int32(-1); z <= chunks; z++ {
			pos := world.ChunkPos{X: x, Z: z}
			w.Acquire(pos)
			holds = append(holds, pos)
		}
	}
	b.Cleanup(func() {
		s.mu.Lock()
		clear(s.clients)
		s.mu.Unlock()
		for _, pos := range holds {
			w.Release(pos)
		}
		s.Shutdown()
	})
	return s, w
}

// BenchmarkIdleHoppers is one block entity tick over 16384 hoppers with
// nothing to move.
func BenchmarkIdleHoppers(b *testing.B) {
	const side = 128
	cobble, _ := registry.ItemID("minecraft:cobblestone")
	y := int32(world.GroundLevel + 3)
	build := func(b *testing.B, facing string, fill func(be *world.BlockEntity), above uint16, aboveKind world.BlockEntityKind) (*Server, *world.World, []*world.BlockEntity) {
		s, w := newBlockBenchServer(b, side/16)
		state := registry.MustState("minecraft:hopper", map[string]string{"facing": facing, "enabled": "true"})
		var bes []*world.BlockEntity
		for x := range int32(side) {
			for z := range int32(side) {
				w.SetBlock(x, y, z, state)
				be := w.EnsureBlockEntity(x, y, z, world.KindHopper)
				if fill != nil {
					fill(be)
				}
				bes = append(bes, be)
				if above != 0 {
					w.SetBlock(x, y+1, z, above)
					if aboveKind != world.KindNone {
						w.EnsureBlockEntity(x, y+1, z, aboveKind)
					}
				}
			}
		}
		w.FlushLighting()
		return s, w, bes
	}
	run := func(b *testing.B, s *Server) {
		// Settle cooldowns left by the first moves.
		for range 10 {
			s.tickBlockEntities()
		}
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			s.tickBlockEntities()
		}
		b.StopTimer()
		b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/(side*side), "ns/hopper")
	}
	full := func(be *world.BlockEntity) {
		for i := range 5 {
			be.Put(i, protocol.ItemStack{ItemID: cobble, Count: 64})
		}
	}
	b.Run("empty", func(b *testing.B) {
		s, _, _ := build(b, "down", nil, 0, world.KindNone)
		run(b, s)
	})
	b.Run("empty_200_chest_carts", func(b *testing.B) {
		s, w, _ := build(b, "down", nil, 0, world.KindNone)
		for i := range 200 {
			s.spawnMinecart(w, "minecraft:chest_minecart", float64(-20-i%10), float64(y), float64(-20-i/10))
		}
		run(b, s)
	})
	b.Run("empty_200_chest_boats", func(b *testing.B) {
		s, w, _ := build(b, "down", nil, 0, world.KindNone)
		for i := range 200 {
			s.spawnBoat(w, "minecraft:oak_chest_boat", float64(-20-i%10), float64(y), float64(-20-i/10), 0, customName{})
		}
		run(b, s)
	})
	b.Run("cooling", func(b *testing.B) {
		s, _, bes := build(b, "down", nil, 0, world.KindNone)
		for _, be := range bes {
			be.Cooldown = 1 << 30
		}
		run(b, s)
	})
	b.Run("disabled", func(b *testing.B) {
		s, w, _ := build(b, "down", nil, 0, world.KindNone)
		off := registry.MustState("minecraft:hopper", map[string]string{"facing": "down", "enabled": "false"})
		for x := range int32(side) {
			for z := range int32(side) {
				w.SetBlock(x, y, z, off)
			}
		}
		run(b, s)
	})
	b.Run("empty_1000items", func(b *testing.B) {
		s, w, _ := build(b, "down", nil, 0, world.KindNone)
		for i := range 1000 {
			s.spawnItem(w, protocol.ItemStack{ItemID: cobble, Count: 1}, float64(-10-i%8), float64(y), float64(-10-i/8%8), 0, 0, 0, 0)
		}
		run(b, s)
	})
	b.Run("empty_under_stone", func(b *testing.B) {
		s, _, _ := build(b, "down", nil, registry.MustState("minecraft:stone", nil), world.KindNone)
		run(b, s)
	})
	b.Run("empty_under_empty_chest", func(b *testing.B) {
		s, _, _ := build(b, "down", nil, registry.MustState("minecraft:barrel", nil), world.KindBarrel)
		run(b, s)
	})
	b.Run("full_into_air", func(b *testing.B) {
		s, _, _ := build(b, "down", full, 0, world.KindNone)
		run(b, s)
	})
	b.Run("full_into_full_hopper", func(b *testing.B) {
		// Rows facing east; the last column faces a wall of nothing.
		s, _, _ := build(b, "east", full, 0, world.KindNone)
		run(b, s)
	})
}

// BenchmarkVehicleColumnDispatch is one block entity tick over chest boats,
// with no hopper, one hopper clear of them, one hopper and no boats, and one
// hopper with 8 boats in the box above it.
func BenchmarkVehicleColumnDispatch(b *testing.B) {
	for _, bc := range []struct {
		name          string
		spaced, above int
		hopper        bool
	}{
		{"no_hopper", 200, 0, false},
		{"one_hopper", 200, 0, true},
		{"empty_index", 0, 0, true},
		{"overlapping_boats", 200, 8, true},
	} {
		b.Run(bc.name, func(b *testing.B) {
			s, w := newBlockBenchServer(b, 0)
			for i := range bc.spaced {
				s.spawnBoat(w, "minecraft:oak_chest_boat", float64((i%20)*4), 64, float64(10+(i/20)*4), 0, customName{})
			}
			// The hopper at (0, 64, 0) pulls from the unit box centred on (0.5, 65.5, 0.5).
			for range bc.above {
				s.spawnBoat(w, "minecraft:oak_chest_boat", 0.5, 65, 0.5, 0, customName{})
			}
			if bc.hopper {
				w.SetBlock(0, 64, 0, registry.MustState("minecraft:hopper", map[string]string{"facing": "down", "enabled": "true"}))
				w.EnsureBlockEntity(0, 64, 0, world.KindHopper)
				// Without the hold the block entity pass skips the hopper.
				b.Cleanup(w.HoldEntityTicking(world.ChunkPos{}))
			}
			s.tickBlockEntities()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tickBlockEntities()
			}
		})
	}
}
