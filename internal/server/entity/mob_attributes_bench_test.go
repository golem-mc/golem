package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkMobBuildAttributes measures the throwaway pairing map a mob
// without an installed map gets for each spawn packet.
func BenchmarkMobBuildAttributes(b *testing.B) {
	for _, tc := range []struct {
		name  string
		kind  MobType
		type_ int32
	}{{"rabbit", TypeRabbit, 0}, {"killer bunny", TypeRabbit, 99}, {"pig", TypePig, 0}, {"zombie", TypeZombie, 0}} {
		b.Run(tc.name, func(b *testing.B) {
			m := NewMob(tc.kind, 1, .5, 66, .5, 1)
			m.RabbitType = tc.type_
			b.ReportAllocs()
			for b.Loop() {
				m.BuildAttributes(nbt.List{})
			}
		})
	}
}

func equippedBenchMob(kind MobType) *Mob {
	m := NewMob(kind, 1, .5, 66, .5, 1)
	m.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemIronSword), Count: 1}
	m.Worn[0] = protocol.ItemStack{ItemID: int32(registry.ItemIronBoots), Count: 1}
	m.Worn[3] = protocol.ItemStack{ItemID: int32(registry.ItemIronHelmet), Count: 1}
	return m
}

// BenchmarkMobBuildAttributesEquipped is the pairing map of a mob holding
// equipment with attribute modifiers.
func BenchmarkMobBuildAttributesEquipped(b *testing.B) {
	for _, tc := range []struct {
		name string
		kind MobType
	}{{"zombie", TypeZombie}, {"pig", TypePig}} {
		b.Run(tc.name, func(b *testing.B) {
			m := equippedBenchMob(tc.kind)
			b.ReportAllocs()
			for b.Loop() {
				m.BuildAttributes(nbt.List{})
			}
		})
	}
}

// BenchmarkMobSyncEquipmentChange is the tick sync after the main hand
// changes between two stacks with modifiers.
func BenchmarkMobSyncEquipmentChange(b *testing.B) {
	for _, tc := range []struct {
		name string
		kind MobType
	}{{"zombie", TypeZombie}, {"pig", TypePig}} {
		b.Run(tc.name, func(b *testing.B) {
			m := equippedBenchMob(tc.kind)
			a := m.BuildAttributes(nbt.List{})
			stacks := [2]protocol.ItemStack{
				{ItemID: int32(registry.ItemIronSword), Count: 1},
				{ItemID: int32(registry.ItemDiamondSword), Count: 1},
			}
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				m.MainHand = stacks[i&1]
				m.equipmentRevision++
				m.SyncEquipmentModifiers(a)
			}
		})
	}
}
