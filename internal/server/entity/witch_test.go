package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// The witch's behaviour as unit tests against the plain test world.

func witchFloor(w testWorld, t testing.TB) {
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-30); x <= 30; x++ {
		for z := int32(-30); z <= 30; z++ {
			w.set(x, 63, z, stone)
		}
	}
}

func newWitch() *Mob { return NewMob(TypeWitch, 1, 0.5, 64, 0.5, 2) }

func witchPrey(dist float64) []Player {
	return []Player{{X: 0.5 + dist, Y: 64, Z: 0.5, EntityID: 7, Huntable: true, Health: 20}}
}

func potionNameOf(t *testing.T, m *Mob) string {
	t.Helper()
	contents, ok := m.MainHand.PotionContentsOf()
	if !ok {
		t.Fatal("the held stack carries no potion contents")
	}
	name, _ := registry.PotionName(contents.Potion)
	return name
}

func TestAWitchThrowsAPotionEverySixtyTicks(t *testing.T) {
	w := newTestWorld()
	witchFloor(w, t)
	m := newWitch()
	players := witchPrey(8)
	var throws []int
	for tick := range 400 {
		for _, ev := range m.Tick(w, players) {
			if ev.Kind == MobThrewPotion {
				throws = append(throws, tick)
			}
		}
	}
	if len(throws) < 4 {
		t.Fatalf("a witch within range threw %d potions in 400 ticks, want at least 4", len(throws))
	}
	for i := 1; i < len(throws); i++ {
		if gap := throws[i] - throws[i-1]; gap != 60 {
			t.Errorf("throw %d came %d ticks after the one before, want 60", i, gap)
		}
	}
}

func TestAWitchPicksItsPotionByDistanceHealthAndEffects(t *testing.T) {
	healthy := Player{Health: 20}
	hurt := Player{Health: 7.5}
	for _, tc := range []struct {
		name string
		p    Player
		dist float64
		roll float32
		want string
	}{
		{"far and unslowed", healthy, 8, 0.9, "minecraft:slowness"},
		{"far, slowed, healthy", Player{Health: 20, Slowed: true}, 9, 0.9, "minecraft:poison"},
		{"near and healthy", healthy, 5, 0.9, "minecraft:poison"},
		{"near, poisoned, lucky", Player{Health: 20, Poisoned: true}, 3, 0.2, "minecraft:weakness"},
		{"near, poisoned, unlucky", Player{Health: 20, Poisoned: true}, 3, 0.3, "minecraft:harming"},
		{"near, weakened, lucky", Player{Health: 20, Poisoned: true, Weakened: true}, 3, 0.2, "minecraft:harming"},
		{"far, poisoned and slowed", Player{Health: 20, Poisoned: true, Slowed: true}, 12, 0.2, "minecraft:harming"},
		{"hurt and mid-range", hurt, 5, 0.2, "minecraft:harming"},
		{"hurt but far", hurt, 8, 0.9, "minecraft:slowness"},
		{"just under eight", hurt, 7.9, 0.9, "minecraft:harming"},
		{"exactly eight health", Player{Health: 8}, 5, 0.9, "minecraft:poison"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := witchPotionFor(&tc.p, tc.dist, tc.roll); got != tc.want {
				t.Errorf("potion = %s, want %s", got, tc.want)
			}
		})
	}
}

// drinkUntil ticks the witch until it holds a potion and reports the
// tick the drink began on, or -1.
func drinkUntil(m *Mob, w testWorld, players []Player, ticks int) int {
	for i := range ticks {
		m.Tick(w, players)
		if m.DrinkingPotion() {
			return i
		}
	}
	return -1
}

func TestABurningWitchDrinksFireResistance(t *testing.T) {
	w := newTestWorld()
	witchFloor(w, t)
	m := newWitch()
	m.FireTicks = 300
	m.NoAI = true
	if drinkUntil(m, w, nil, 400) < 0 {
		t.Fatal("a burning witch never drank")
	}
	if got := potionNameOf(t, m); got != "minecraft:fire_resistance" {
		t.Fatalf("drank %s, want fire resistance", got)
	}
	if pace := m.pace(); pace != 0 {
		t.Errorf("a drinking witch walks at %v, want the drinking modifier to stop it", pace)
	}
	for range witchDrinkTicks + 2 {
		m.Tick(w, nil)
	}
	if m.DrinkingPotion() || !m.MainHand.Empty() {
		t.Errorf("after the drink: drinking = %v, hand empty = %v, want the glass put away", m.DrinkingPotion(), m.MainHand.Empty())
	}
	if !m.hasEffect(mobEffectFireResistance) {
		t.Error("the witch has no fire resistance after drinking it")
	}
	if pace := m.pace(); pace != 0.25 {
		t.Errorf("the pace after drinking = %v, want 0.25 again", pace)
	}
}

func TestAHurtWitchDrinksHealing(t *testing.T) {
	w := newTestWorld()
	witchFloor(w, t)
	m := newWitch()
	m.NoAI = true
	m.Health = 10
	if drinkUntil(m, w, nil, 3000) < 0 {
		t.Fatal("a hurt witch never drank")
	}
	if got := potionNameOf(t, m); got != "minecraft:healing" {
		t.Fatalf("drank %s, want healing", got)
	}
	for range witchDrinkTicks + 4 {
		m.Tick(w, nil)
	}
	if m.Health <= 10 {
		t.Errorf("health = %v after the healing potion, want it mended", m.Health)
	}
}

// witchThrowsAmong ticks the witch among others until its first throw and
// returns the potion and the tick, or "" after limit ticks.
func witchThrowsAmong(m *Mob, w testWorld, players []Player, others []*Mob, limit int) (string, int) {
	for tick := range limit {
		for _, ev := range m.tick(w, players, others) {
			if ev.Kind == MobThrewPotion {
				return ev.Potion, tick
			}
		}
	}
	return "", limit
}

func TestARaidingWitchHealsAWoundedRaider(t *testing.T) {
	for _, tc := range []struct {
		health float64
		want   string
	}{{3, "minecraft:healing"}, {4, "minecraft:healing"}, {10, "minecraft:regeneration"}} {
		w := newTestWorld()
		witchFloor(w, t)
		m := newWitch()
		m.InRaid = true
		pillager := NewMob(TypePillager, 2, 6.5, 64, 0.5, 1)
		pillager.Health = tc.health
		potion, _ := witchThrowsAmong(m, w, nil, []*Mob{m, pillager}, 400)
		if potion != tc.want {
			t.Errorf("a raiding witch threw %q at a pillager on %v health, want %q", potion, tc.health, tc.want)
		}
		if m.TargetID != 0 {
			t.Errorf("the witch kept the pillager it healed as its target")
		}
	}
}
