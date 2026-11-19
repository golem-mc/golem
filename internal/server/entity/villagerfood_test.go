package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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
