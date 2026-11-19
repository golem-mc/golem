package registry

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// BenchmarkDamageHolderHurt measures the resistance and shield membership
// queries made for each hurt, with components already decoded as on equipment.
func BenchmarkDamageHolderHurt(b *testing.B) {
	b.Run("DefaultResistance", func(b *testing.B) {
		stack := protocol.ItemStack{ItemID: int32(ItemNetheriteHelmet), Count: 1}
		b.ReportAllocs()
		for b.Loop() {
			if !StackResistsDamage(stack, DamageTypeLava) {
				b.Fatal("lost resistance")
			}
		}
	})
	b.Run("ComponentResistance", func(b *testing.B) {
		stack := protocol.ItemStack{ItemID: int32(ItemIronHelmet), Count: 1}
		stack.SetDamageResistant(protocol.DamageSetComponent{Present: true, Tag: "minecraft:is_fire"})
		b.ReportAllocs()
		for b.Loop() {
			if !StackResistsDamage(stack, DamageTypeLava) {
				b.Fatal("lost resistance")
			}
		}
	})
	b.Run("ShieldMembership", func(b *testing.B) {
		stack := protocol.ItemStack{ItemID: int32(ItemShield), Count: 1}
		stack.SetBlocksAttacks(protocol.BlocksAttacksComponent{BypassedBy: protocol.DamageSetComponent{Present: true, Tag: "minecraft:is_fire"}})
		b.ReportAllocs()
		for b.Loop() {
			props, ok := StackBlocksAttacks(stack)
			if !ok || !props.BypassedBy.Contains(DamageTypeLava) {
				b.Fatal("lost bypass")
			}
		}
	})
}
