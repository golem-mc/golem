package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkRepairFromXp(b *testing.B) {
	mending, ok := registry.EnchantmentID("minecraft:mending")
	if !ok {
		b.Fatal("missing Mending enchantment")
	}
	for _, tc := range []struct {
		name        string
		unbreakable bool
	}{
		{name: "breakable"},
		{name: "unbreakable", unbreakable: true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			var pieces [len(player.EquipmentSlots)]EnchPiece
			for i, slot := range player.EquipmentSlots {
				pieces[i].Slot = slot
			}
			pieces[0].Stack = protocol.ItemStack{
				ItemID:       int32(registry.ItemDiamondPickaxe),
				Count:        1,
				Damage:       100,
				Unbreakable:  tc.unbreakable,
				Enchantments: map[int32]int32{mending: 1},
			}
			b.ReportAllocs()
			for b.Loop() {
				RepairFromXp(pieces[:], 5, nil)
			}
		})
	}
}
