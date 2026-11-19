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
