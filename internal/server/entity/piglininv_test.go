package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestAFullPiglinInventoryThrowsTheOverflow: stow keeps what fits and throws
// the rest.
func TestAFullPiglinInventoryThrowsTheOverflow(t *testing.T) {
	m := NewMob(TypePiglin, 1, 0, 64, 0, 1)
	nugget := int32(registry.ItemGoldNugget)
	for range piglinInventorySize {
		m.addToInventory(protocol.ItemStack{ItemID: nugget, Count: 64})
	}
	tv := &tickView{w: testWorld{}}
	m.stow(tv, protocol.ItemStack{ItemID: nugget, Count: 3})
	if len(tv.events) != 1 || tv.events[0].Kind != MobThrewItem || tv.events[0].Stack.Count != 3 {
		t.Fatalf("events = %+v, want one thrown stack of 3 nuggets", tv.events)
	}
	if got := len(m.CarriedInventory()); got != piglinInventorySize {
		t.Errorf("inventory holds %d stacks, want %d", got, piglinInventorySize)
	}
}

// TestAPiglinInventorySurvivesSaveAndLoad round-trips "Inventory" through NBT.
func TestAPiglinInventorySurvivesSaveAndLoad(t *testing.T) {
	m := NewMob(TypePiglin, 1, 0, 64, 0, 1)
	nugget := int32(registry.ItemGoldNugget)
	bell, _ := registry.ItemID("minecraft:bell")
	m.addToInventory(protocol.ItemStack{ItemID: nugget, Count: 7})
	m.addToInventory(protocol.ItemStack{ItemID: bell, Count: 1})
	tag := nbt.Compound{}
	m.putInventory(tag)
	again := NewMob(TypePiglin, 2, 0, 64, 0, 2)
	again.loadInventory(tag)
	got := again.CarriedInventory()
	if len(got) != 2 || got[0].ItemID != nugget || got[0].Count != 7 || got[1].ItemID != bell || got[1].Count != 1 {
		t.Fatalf("loaded inventory = %+v, want 7 nuggets then a bell", got)
	}
}
