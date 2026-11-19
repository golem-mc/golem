package entity

import "testing"

// vanilla: GiveGiftToHero.getLootTableToThrow
func TestAVillagerPicksTheGiftTableOfItsProfession(t *testing.T) {
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	for prof, want := range map[string]string{
		"minecraft:librarian": "librarian_gift",
		"minecraft:none":      "unemployed_gift",
		"minecraft:nitwit":    "unemployed_gift",
	} {
		m.Villager.Profession = prof
		if got := m.giftTable(); got != "minecraft:gameplay/hero_of_the_village/"+want {
			t.Errorf("%s gives %s", prof, got)
		}
	}
}

// vanilla: GiveGiftToHero
func TestAVillagerThrowsAGiftToAHeroAfterTheHeadTurns(t *testing.T) {
	w := flatWorld(t)
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	m.Villager.Profession = "minecraft:farmer"
	m.vb.activity = actIdle
	players := []Player{{EntityID: 7, X: 2, Y: 64, Z: 0, Hero: true}}
	threw := -1
	for tick := 0; tick < 1400 && threw < 0; tick++ {
		m.GameTime = int64(1000 + tick)
		m.X, m.Z = 0, 0 // a stroll must not take it out of sight
		for _, e := range m.tick(w, players, nil) {
			if e.Kind == MobThrewItem && threw < 0 {
				threw = tick
			}
		}
	}
	if threw < giftFirstDelay+giftTurnTicks {
		t.Errorf("threw at tick %d, want after the first %d-tick wait and the turn", threw, giftFirstDelay)
	}
	players[0].Hero = false
	m2 := NewMob(TypeVillager, 2, 0, 64, 0, 2)
	m2.vb.activity = actIdle
	for tick := range 1400 {
		m2.GameTime = int64(1000 + tick)
		for _, e := range m2.tick(w, players, nil) {
			if e.Kind == MobThrewItem {
				t.Fatal("a villager gave a gift to a player without the effect")
			}
		}
	}
}

// The closest visible player must be the hero, even with a hero farther away.
// vanilla: GiveGiftToHero.isHero
func TestVillagerGiftRequiresTheNearestVisiblePlayerToBeAHero(t *testing.T) {
	w := flatWorld(t)
	m := NewMob(TypeVillager, 1, 0, 64, 0, 1)
	g := &giveGiftGoal{m: m}
	players := []Player{{EntityID: 2, X: 2, Y: 64}, {EntityID: 3, X: 3, Y: 64, Hero: true}}
	if g.hero(&tickView{w: w, players: players}) != nil {
		t.Fatal("chose a hero behind a closer visible non-hero")
	}
	players[0].Hero = true
	if p := g.hero(&tickView{w: w, players: players}); p == nil || p.EntityID != 2 {
		t.Fatal("did not choose the nearest visible hero")
	}
	// No world is needed when there is no hero: visibility must not be queried.
	players[0].Hero, players[1].Hero = false, false
	if g.hero(&tickView{players: players}) != nil {
		t.Fatal("chose a non-hero")
	}
}
