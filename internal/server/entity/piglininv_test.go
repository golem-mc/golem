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

// TestABabyPiglinHoldsAnAdmiredItemInItsMainHand: a baby that cannot wear the
// item throws an unloved main-hand item and holds the admired one.
func TestABabyPiglinHoldsAnAdmiredItemInItsMainHand(t *testing.T) {
	m := NewMob(TypePiglin, 1, 0, 64, 0, 1)
	m.SetBaby(true)
	bell, _ := registry.ItemID("minecraft:bell")
	m.MainHand = protocol.ItemStack{ItemID: int32(registry.ItemStoneSword), Count: 1}
	m.OffHand = protocol.ItemStack{ItemID: bell, Count: 1}
	tv := &tickView{w: testWorld{}}
	m.finishAdmiring(tv, true)
	if m.MainHand.ItemID != bell || !m.OffHand.Empty() {
		t.Errorf("main hand %+v, off hand %+v; want the bell in the main hand", m.MainHand, m.OffHand)
	}
	if len(tv.events) != 1 || tv.events[0].Stack.ItemID != int32(registry.ItemStoneSword) {
		t.Errorf("events = %+v, want the sword thrown", tv.events)
	}
	if len(m.CarriedInventory()) != 0 {
		t.Error("a baby put the admired item in its inventory")
	}
}

// TestAPiglinSwingsItsOffHandWhenThrowingAMainHandItem: a non-loved main-hand
// item is thrown after one off-hand swing.
func TestAPiglinSwingsItsOffHandWhenThrowingAMainHandItem(t *testing.T) {
	m := NewMob(TypePiglin, 1, 0, 64, 0, 1)
	stick, _ := registry.ItemID("minecraft:stick")
	m.MainHand = protocol.ItemStack{ItemID: stick, Count: 1}
	tv := &tickView{w: testWorld{}}
	m.releaseMainHand(tv)
	if len(tv.events) != 2 || tv.events[0].Kind != MobSwungOffHand || tv.events[1].Kind != MobThrewItem {
		t.Fatalf("events = %+v, want a swing then a throw", tv.events)
	}
}

// TestAPiglinEatsOneFoodItemThenWaitsOutTheCooldown: a taken porkchop sets
// ATE_RECENTLY, which refuses the next one until it runs out.
func TestAPiglinEatsOneFoodItemThenWaitsOutTheCooldown(t *testing.T) {
	m := NewMob(TypePiglin, 1, 0, 64, 0, 1)
	pork, _ := registry.ItemID("minecraft:porkchop")
	stack := protocol.ItemStack{ItemID: pork, Count: 3}
	s := stagedSlots{}
	slot, count, ok := s.takePiglin(m, stack)
	if !ok || slot != "eat" || count != 1 {
		t.Fatalf("takePiglin = %q, %d, %v, want eat, 1, true", slot, count, ok)
	}
	m.TookLoot(LootTake{Slot: slot, Count: count}, stack)
	if _, _, ok := s.takePiglin(m, stack); ok {
		t.Error("piglin took food while it had eaten recently")
	}
	m.barter.ate = 0
	if _, _, ok := s.takePiglin(m, stack); !ok {
		t.Error("piglin refused food after the cooldown")
	}
}

// TestAPiglinWearsAShieldWithoutAdmiringIt: a shield goes to the off hand as
// equipment, not as a loved item, so it starts no admire.
func TestAPiglinWearsAShieldWithoutAdmiringIt(t *testing.T) {
	m := NewMob(TypePiglin, 1, 0, 64, 0, 1)
	shield := protocol.ItemStack{ItemID: int32(registry.ItemShield), Count: 1}
	s := stagedSlots{main: m.MainHand, off: m.OffHand, worn: m.Worn}
	slot, count, ok := s.takePiglin(m, shield)
	if !ok || slot != "offhand" || count != 1 {
		t.Fatalf("takePiglin = %q, %d, %v, want offhand, 1, true", slot, count, ok)
	}
	m.TookLoot(LootTake{Slot: slot, Count: count}, shield)
	if registry.Item(m.OffHand.ItemID) != registry.ItemShield {
		t.Errorf("off hand = %v, want the shield", m.OffHand)
	}
	if m.barter.admiring != 0 {
		t.Errorf("admiring = %d, want 0 for a shield", m.barter.admiring)
	}
}

// TestAPiglinHoldingAShieldStillTakesAGoldIngot: only a loved item in the off
// hand refuses the bartering currency; the ingot displaces the shield.
func TestAPiglinHoldingAShieldStillTakesAGoldIngot(t *testing.T) {
	m := NewMob(TypePiglin, 1, 0, 64, 0, 1)
	shield := protocol.ItemStack{ItemID: int32(registry.ItemShield), Count: 1}
	m.TookLoot(LootTake{Slot: "offhand", Count: 1}, shield)
	ingot := protocol.ItemStack{ItemID: int32(registry.ItemGoldIngot), Count: 4}
	s := stagedSlots{main: m.MainHand, off: m.OffHand, worn: m.Worn}
	slot, count, ok := s.takePiglin(m, ingot)
	if !ok || slot != "offhand" || count != 1 {
		t.Fatalf("takePiglin = %q, %d, %v, want offhand, 1, true", slot, count, ok)
	}
	dropped := m.TookLoot(LootTake{Slot: slot, Count: count}, ingot)
	if registry.Item(dropped.ItemID) != registry.ItemShield {
		t.Errorf("dropped = %v, want the shield", dropped)
	}
	if registry.Item(m.OffHand.ItemID) != registry.ItemGoldIngot || m.barter.admiring != piglinAdmireTicks {
		t.Errorf("off hand = %v, admiring = %d, want a gold ingot admired for %d", m.OffHand, m.barter.admiring, piglinAdmireTicks)
	}
	s = stagedSlots{main: m.MainHand, off: m.OffHand, worn: m.Worn}
	if _, _, ok := s.takePiglin(m, ingot); ok {
		t.Error("piglin took a second ingot while admiring the first")
	}
}
