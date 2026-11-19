package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func itemsOf(name string, n int32) protocol.ItemStack {
	return protocol.ItemStack{ItemID: registry.MustItem(name), Count: n}
}

func TestAVillagerEatsUntilFullThenSpendsTwelve(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	m.barter.Inventory[0] = itemsOf("minecraft:carrot", 2)
	m.barter.Inventory[1] = itemsOf("minecraft:bread", 5)
	if !m.villagerCanBreed() {
		t.Fatal("22 food points cannot breed")
	}
	m.eatAndDigest()
	// 2 carrots and 3 bread reach 14; 12 is spent.
	if m.FoodLevel != 2 || !m.barter.Inventory[0].Empty() || m.barter.Inventory[1].Count != 2 {
		t.Errorf("food level %d, inventory %v; want 2 and two bread", m.FoodLevel, m.barter.Inventory[:2])
	}
	if m.villagerCanBreed() {
		t.Error("10 food points can breed")
	}
	m.FoodLevel = 12
	m.BabyAge = villagerParentAge
	if m.villagerCanBreed() {
		t.Error("a parent on its cooldown can breed")
	}
}

func TestAVillagerPicksUpOnlyWhatItWants(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	if !m.CanPickUpLoot {
		t.Fatal("a new villager cannot pick up loot")
	}
	s := stagedSlots{}
	if slot, n, ok := s.take(m, itemsOf("minecraft:bread", 5)); !ok || slot != "inventory" || n != 5 {
		t.Errorf("bread: %q %d %v", slot, n, ok)
	}
	if _, _, ok := s.take(m, itemsOf("minecraft:bone_meal", 1)); ok {
		t.Error("a jobless villager takes bone meal")
	}
	m.Villager.Profession = "minecraft:farmer"
	if _, _, ok := s.take(m, itemsOf("minecraft:bone_meal", 1)); !ok {
		t.Error("a farmer refuses bone meal")
	}
	for i := range m.barter.Inventory {
		m.barter.Inventory[i] = itemsOf("minecraft:stone", 64)
	}
	m.barter.Inventory[3] = itemsOf("minecraft:bread", 60)
	if _, n, ok := s.take(m, itemsOf("minecraft:bread", 10)); !ok || n != 4 {
		t.Errorf("into a nearly full inventory: %d %v, want 4", n, ok)
	}
}

func TestAVillagerThrowsHalfItsSurplus(t *testing.T) {
	cases := []struct{ have, thrown int32 }{{40, 20}, {30, 6}, {24, 0}}
	for _, c := range cases {
		m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
		o := NewMob(TypeVillager, 2, 2, 64, 0, 2)
		m.Villager.Profession = "minecraft:farmer"
		m.barter.Inventory[0] = itemsOf("minecraft:wheat", c.have)
		tv := &tickView{}
		m.throwHalfStack(tv, func(s protocol.ItemStack) bool { return s.ItemID == wheatItem }, o)
		var got int32
		for _, ev := range tv.events {
			if ev.Kind == MobThrewItem {
				got += ev.Stack.Count
				if ev.VX <= 0 {
					t.Errorf("%d wheat: thrown away from the target, vx %v", c.have, ev.VX)
				}
			}
		}
		if got != c.thrown || m.countItem(wheatItem) != c.have-c.thrown {
			t.Errorf("%d wheat: threw %d, kept %d; want %d thrown", c.have, got, m.countItem(wheatItem), c.thrown)
		}
	}
}

func TestAVillagerSharesSeedsWithAFarmer(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	farmer := NewMob(TypeVillager, 2, 2, 64, 0, 2)
	farmer.Villager.Profession = "minecraft:farmer"
	m.barter.Inventory[0] = itemsOf("minecraft:wheat_seeds", 40)
	tv := &tickView{}
	m.shareWith(tv, farmer)
	if len(tv.events) != 1 || tv.events[0].Stack.Count != 20 {
		t.Errorf("events %v, want 20 seeds thrown", tv.events)
	}
	tv = &tickView{}
	farmer.barter.Inventory[0] = itemsOf("minecraft:wheat_seeds", 40)
	farmer.shareWith(tv, m)
	if len(tv.events) != 0 {
		t.Errorf("a farmer threw its own seeds: %v", tv.events)
	}
}

func TestVillagersBreedOnlyWithAFreeBedForTheChild(t *testing.T) {
	w := newTestWorld()
	floor(w, -6, -6, 6, 6, 63, defaultState(t, "minecraft:stone"))
	w.set(4, 64, 0, bedHead(t))
	view := fakeVillage{w: w, pois: []Waypoint{{4, 64, 0}}}
	a := NewMob(TypeVillager, 1, 0.5, 64, 0.5, 1)
	b := NewMob(TypeVillager, 2, 1.5, 64, 0.5, 2)
	c := NewMob(TypeVillager, 3, 0.5, 64, 1.5, 3)
	d := NewMob(TypeVillager, 4, 1.5, 64, 1.5, 4)
	for _, m := range []*Mob{a, b, c, d} {
		m.Village = view
	}
	tv := &tickView{w: w, others: []*Mob{b, c, d}}
	a.giveBirth(tv, b)
	if len(tv.events) != 1 || tv.events[0].Kind != MobBred || a.BabyAge != villagerParentAge || b.BabyAge != villagerParentAge {
		t.Fatalf("events %v, ages %d %d; want a birth", tv.events, a.BabyAge, b.BabyAge)
	}
	// The bed is the unborn child's until it spawns.
	tv = &tickView{w: w, others: []*Mob{a, b, d}}
	c.giveBirth(tv, d)
	if len(tv.events) != 2 || tv.events[0].Status != protocol.EntityStatusVillagerAngry || c.BabyAge != 0 {
		t.Errorf("events %v, age %d; want both angry and no birth", tv.events, c.BabyAge)
	}
	baby := NewMob(TypeVillager, 5, 0.5, 64, 0.5, 5)
	a.BreedVillager(baby, b, testBiome("minecraft:desert"))
	if baby.vb.home == nil || baby.vb.home.at() != (Waypoint{4, 64, 0}) || a.vb.childHome != nil {
		t.Errorf("child's home %v, parent's pending %v", baby.vb.home, a.vb.childHome)
	}
	if baby.Villager.Profession != "minecraft:none" || !baby.Villager.Finalized {
		t.Errorf("child data %+v", baby.Villager)
	}
}

func TestAVillagersFoodRoundTrips(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	m.FoodLevel = 7
	m.barter.Inventory[0] = itemsOf("minecraft:potato", 9)
	tag := nbt.Compound{}
	m.SaveTo(tag, func() int64 { return 0 })
	if v, _ := nbt.ByteField(tag, "FoodLevel"); v != 7 {
		t.Errorf("FoodLevel saved as %d", v)
	}
	l := NewMob(TypeVillager, 2, 0, 64, 0, 2)
	l.LoadFrom(tag, newTestWorld(), func() int64 { return 0 })
	if l.FoodLevel != 7 || l.countItem(registry.MustItem("minecraft:potato")) != 9 {
		t.Errorf("loaded food level %d, potatoes %d", l.FoodLevel, l.countItem(registry.MustItem("minecraft:potato")))
	}
	empty := NewMob(TypeVillager, 3, 0, 64, 0, 3)
	tag = nbt.Compound{}
	empty.SaveTo(tag, func() int64 { return 0 })
	if list, ok := nbt.Get[nbt.List](tag, "Inventory"); !ok || len(list.Items) != 0 {
		t.Errorf("an empty inventory saved as %v, want an empty list", tag["Inventory"])
	}
	if drops := l.inventoryDrops(); drops != nil {
		t.Errorf("a villager's inventory drops %v", drops)
	}
}
