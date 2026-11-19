package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkEnchantmentDamageProtection measures the holder predicates on worn armour.
func BenchmarkEnchantmentDamageProtection(b *testing.B) {
	fire := registry.MustID("minecraft:enchantment", "minecraft:fire_protection")
	var pieces []EnchPiece
	for i, item := range []registry.Item{registry.ItemDiamondHelmet, registry.ItemDiamondChestplate, registry.ItemDiamondLeggings, registry.ItemDiamondBoots} {
		pieces = append(pieces, EnchPiece{
			Slot:  []string{"head", "chest", "legs", "feet"}[i],
			Stack: protocol.ItemStack{ItemID: int32(item), Count: 1, Enchantments: map[int32]int32{fire: 4}},
		})
	}
	for _, tc := range []struct {
		name   string
		damage registry.DamageType
		want   float64
	}{{"Lava", registry.DamageTypeLava, 32}, {"Generic", registry.DamageTypeGeneric, 0}} {
		b.Run(tc.name, func(b *testing.B) {
			ctx := &EnchContext{DamageType: tc.damage, Direct: true, Victim: protocol.EntityTypePlayer}
			b.ReportAllocs()
			for b.Loop() {
				if got := DamageProtection(pieces, ctx, nil); got != tc.want {
					b.Fatalf("protection = %v, want %v", got, tc.want)
				}
			}
		})
	}
}
