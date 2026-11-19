package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkLootSlotOf(b *testing.B) {
	stacks := [...]protocol.ItemStack{
		{ItemID: int32(registry.ItemIronHelmet), Count: 1},
		{ItemID: int32(registry.ItemStone), Count: 1},
		{ItemID: int32(registry.ItemDiamondChestplate), Count: 1},
		{ItemID: int32(registry.ItemShield), Count: 1},
	}
	for b.Loop() {
		for _, stack := range stacks {
			lootSlotOf(stack)
		}
	}
}
