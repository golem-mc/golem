package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
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

// BenchmarkApplyLocatedModsActivating is a boots location_changed attribute
// entry becoming active on a mob's live map, then stopping. The pig lacks
// attack_speed, so the entry adds nothing; movement_speed is on every mob and
// attack_damage on a zombie.
func BenchmarkApplyLocatedModsActivating(b *testing.B) {
	for _, tc := range []struct {
		name, attr string
		kind       MobType
	}{{"pig_lacking", "minecraft:attack_speed", TypePig}, {"pig_having", "minecraft:movement_speed", TypePig}, {"zombie_having", "minecraft:attack_damage", TypeZombie}} {
		b.Run(tc.name, func(b *testing.B) {
			a := NewMob(tc.kind, 1, .5, 66, .5, 1).BuildAttributes(nbt.List{})
			act := rules.LocatedAction{Slot: "feet", Level: 1, Became: true, Entry: registry.EnchLocated{Attribute: &registry.EnchAttribute{
				Attribute: tc.attr, ID: "pack:slow", Operation: "add_value",
				Amount: &registry.EnchLevelValue{Kind: registry.EnchValueConstant, Constant: .1},
			}}}
			if _, ok := act.LocatedAttributeModifier(); !ok {
				b.Fatal("no modifier")
			}
			on := []rules.LocatedAction{act}
			b.ReportAllocs()
			for b.Loop() {
				ApplyLocatedMods(a, on, nil)
				ApplyLocatedMods(a, nil, on)
			}
		})
	}
}
