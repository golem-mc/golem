package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkRandomTickGather is the server's random-tick pass over 1156 loaded
// flat chunks (no section holds a ticking block) with 20 players: the cost of
// gathering, filtering and sorting the chunk list each tick.
func BenchmarkRandomTickGather(b *testing.B) { benchRandomTickGather(b, false) }

// BenchmarkRandomTickGatherTicketed is the same pass with a plugin ticket held
// in the loaded area, so the chunks no player covers each check the tickets.
func BenchmarkRandomTickGatherTicketed(b *testing.B) { benchRandomTickGather(b, true) }

func benchRandomTickGather(b *testing.B, ticketed bool) {
	s, w := newBlockBenchServer(b, 32)
	if ticketed {
		w.AddTicket(world.ChunkPos{X: 20, Z: 20}, 2)
	}
	w.FlushLighting()
	dim := s.dimensionOfWorld(w)
	for i := range 20 {
		p := &player.Player{UUID: uuid.New(), EntityID: s.nextEntity()}
		p.SetPosition(player.Position{X: float64(i%5) * 100, Y: float64(world.GroundLevel), Z: float64(i/5) * 100})
		c := &client{srv: s, player: p}
		c.dim.Store(dim)
		s.mu.Lock()
		s.assignPlayerSlot(c)
		s.clients[p.UUID] = c
		s.mu.Unlock()
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		s.tickRandomTicks(dim, int64(i))
	}
}

// BenchmarkPlateChecksBesideHerd is 50 pressure plate checks on distinct
// cells beside 2000 zombies, with the dimension's touch window open (one
// snapshot per op) and closed (one snapshot per check).
func BenchmarkPlateChecksBesideHerd(b *testing.B) {
	s, _ := newTrackerBenchServer(b)
	dim := s.allDims()[0]
	for i := range 2000 {
		s.spawnMobInDim(dim, entity.TypeZombie, float64(i%45)*2-45, float64(world.GroundLevel), float64(i/45)*2-45, int64(i), nil)
	}
	t := redstoneToucher{s: s, dim: dim}
	checks := func() {
		for i := range int32(50) {
			t.Touching(i, world.GroundLevel, 0)
		}
	}
	b.Run("windowed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			dim.touch.begin()
			checks()
			dim.touch.end()
		}
	})
	b.Run("unwindowed", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			checks()
		}
	})
}

// BenchmarkRollBreakDropShulkerBox measures the drops of a filled shulker box
// a piston destroyed, whose slots come from the effect's captured compound.
func BenchmarkRollBreakDropShulkerBox(b *testing.B) {
	s, w := newCommandBenchServer(b, 0)
	var items []nbt.Tag
	for slot := range 27 {
		items = append(items, nbt.Compound{
			"Slot":  nbt.Byte(slot),
			"id":    nbt.String("minecraft:diamond"),
			"count": nbt.Int(1),
		})
	}
	tag := nbt.Compound{
		"id":    nbt.String("minecraft:shulker_box"),
		"Items": nbt.List{Elem: nbt.TypeCompound, Items: items},
	}
	e := world.Effect{BreakDrop: registry.BlockShulkerBox.DefaultState(), BreakTag: tag, X: 3, Y: 64, Z: 0}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		s.rollBreakDrop(w, e)
	}
}
