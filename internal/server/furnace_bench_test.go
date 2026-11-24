package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkFurnaceCooking measures progress and recipe completion in a lit furnace.
func BenchmarkFurnaceCooking(b *testing.B) {
	_, w := newBlockBenchServer(b, 0)
	w.SetBlock(0, 64, 0, registry.BlockFurnace.DefaultState())
	be := w.EnsureBlockEntity(0, 64, 0, world.KindFurnace)
	be.Put(0, protocol.ItemStack{ItemID: int32(registry.ItemIronOre), Count: 64})
	b.ReportAllocs()
	for b.Loop() {
		be.LitRemaining = 1600
		be.Slots[0].Count = 64
		be.Slots[2] = protocol.ItemStack{}
		step := furnaceBurn{be: be}
		step.advance(be)
	}
}

// BenchmarkActiveCookers measures the block-entity pass over 500 lit cookers.
func BenchmarkActiveCookers(b *testing.B) {
	for _, c := range []struct {
		name  string
		block registry.Block
		kind  world.BlockEntityKind
		input registry.Item
	}{
		{"furnace", registry.BlockFurnace, world.KindFurnace, registry.ItemIronOre},
		{"blast_furnace", registry.BlockBlastFurnace, world.KindBlastFurnace, registry.ItemIronOre},
		{"smoker", registry.BlockSmoker, world.KindSmoker, registry.ItemBeef},
	} {
		b.Run(c.name, func(b *testing.B) {
			s, w := newBlockBenchServer(b, 1)
			for i := range int32(500) {
				x, y, z := i%16, world.GroundLevel+1+i/256, i/16%16
				w.SetBlock(x, y, z, c.block.DefaultState())
				be := w.EnsureBlockEntity(x, y, z, c.kind)
				be.Put(0, protocol.ItemStack{ItemID: int32(c.input), Count: 64})
				be.Put(1, protocol.ItemStack{ItemID: int32(registry.ItemCoalBlock), Count: 64})
				w.Touch(x, y, z)
			}
			w.FlushLighting()
			w.AddTicket(world.ChunkPos{}, 2)
			for range 5 {
				s.tickBlockEntities()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tickBlockEntities()
			}
			b.StopTimer()
		})
	}
}
