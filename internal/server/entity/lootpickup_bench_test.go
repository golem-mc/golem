package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkPillagerLootPickup(b *testing.B) {
	m := NewMob(TypePillager, 1, 0.5, 64, 0.5, 1)
	m.CanPickUpLoot = true
	mobs := Mobs{m}
	items := []LootItem{
		{X: 0.5, Y: 64, Z: 0.5, Stack: protocol.ItemStack{ItemID: int32(registry.ItemIronHelmet), Count: 1}},
		{X: 0.5, Y: 64, Z: 0.5, Stack: protocol.ItemStack{ItemID: int32(registry.ItemWhiteBanner), Count: 1}},
	}
	b.ReportAllocs()
	for b.Loop() {
		mobs.TakeLoot(items)
	}
}
