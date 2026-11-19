package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkTargetVisibility measures the per-player visibility check in target scans.
func BenchmarkTargetVisibility(b *testing.B) {
	for _, tc := range []struct {
		name string
		kind MobType
		worn [4]protocol.ItemStack
	}{
		{name: "bare", kind: TypeZombie},
		{name: "piglin-head", kind: TypePiglinBrute, worn: [4]protocol.ItemStack{
			wornHead: {ItemID: int32(registry.ItemPiglinHead), Count: 1},
		}},
		{name: "armour", kind: TypeZombie, worn: [4]protocol.ItemStack{
			wornFeet:  {ItemID: int32(registry.ItemIronBoots), Count: 1},
			wornLegs:  {ItemID: int32(registry.ItemIronLeggings), Count: 1},
			wornChest: {ItemID: int32(registry.ItemIronChestplate), Count: 1},
			wornHead:  {ItemID: int32(registry.ItemIronHelmet), Count: 1},
		}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			m := NewMob(tc.kind, 1, 0, 64, 0, 1)
			p := Player{Worn: tc.worn}
			b.ReportAllocs()
			for b.Loop() {
				visibilityPercent(m, &p)
			}
		})
	}
}
