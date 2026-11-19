package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkDispenserBodyEquipment(b *testing.B) {
	for _, tc := range []struct {
		name string
		kind MobType
		item registry.Item
		tame bool
	}{
		{"tamed_horse", TypeHorse, registry.ItemIronHorseArmor, true},
		{"wild_horse", TypeHorse, registry.ItemIronHorseArmor, false},
		{"tamed_llama", TypeLlama, registry.ItemWhiteCarpet, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			mob := NewMob(tc.kind, 1, 4.5, 64, 1.5, 1)
			mob.Horse.Tame = tc.tame
			stack := protocol.ItemStack{ItemID: int32(tc.item), Count: 1}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				mob.Body = protocol.ItemStack{}
				mob.DispenserEquip(stack)
			}
		})
	}
}
