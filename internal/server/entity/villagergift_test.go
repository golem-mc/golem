package entity

import "testing"

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
