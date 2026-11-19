package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/rules"
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

func newWitch() *Mob { return NewMob(TypeWitch, 1, 0.5, 64, 0.5, 5) }

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

func TestAWitchThrowsAtThePowerOfItsRange(t *testing.T) {
	w := newTestWorld()
	witchFloor(w, t)
	m := newWitch()
	tv := &tickView{w: w}
	p := Player{X: 0.5 + 12, Y: 64, Z: 0.5, Health: 20}
	m.witchThrow(tv, &p)
	if len(tv.events) != 1 {
		t.Fatalf("a throw gave %d events, want 1", len(tv.events))
	}
	ev := tv.events[0]
	if ev.Potion != "minecraft:slowness" {
		t.Errorf("potion = %s, want slowness for a target twelve blocks off", ev.Potion)
	}
	if ev.VZ < -0.2 || ev.VZ > 0.2 {
		t.Errorf("VZ = %v, want a throw along x with only its inaccuracy off it", ev.VZ)
	}
	if speed := math.Sqrt(ev.VX*ev.VX + ev.VY*ev.VY + ev.VZ*ev.VZ); speed < 0.5 || speed > 1.0 {
		t.Errorf("speed = %v, want about the 0.75 of a far throw", speed)
	}
	if want := 64 + 1.62 - ThrownSpawnBelow; math.Abs(ev.Y-want) > 1e-9 {
		t.Errorf("born at y=%v, want %v", ev.Y, want)
	}
	near := &tickView{w: w}
	m.witchThrow(near, &Player{X: 0.5 + 1.5, Y: 64, Z: 0.5, Health: 20})
	ne := near.events[0]
	if speed := math.Sqrt(ne.VX*ne.VX + ne.VY*ne.VY + ne.VZ*ne.VZ); speed > 0.6 {
		t.Errorf("a throw at 1.5 blocks has speed %v, want about the 0.45 of a near throw", speed)
	}
}

func TestADrinkingWitchThrowsNothing(t *testing.T) {
	w := newTestWorld()
	m := newWitch()
	m.witch.drinking = true
	tv := &tickView{w: w}
	m.witchThrow(tv, &Player{X: 8, Y: 64, Z: 0.5, Health: 20})
	if len(tv.events) != 0 {
		t.Errorf("a drinking witch threw %d potions, want none", len(tv.events))
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

func TestADrinkLastsTheThirtyTwoTicksOfAPotionPlusOne(t *testing.T) {
	w := newTestWorld()
	witchFloor(w, t)
	m := newWitch()
	m.NoAI = true
	m.FireTicks = 300
	start := drinkUntil(m, w, nil, 400)
	if start < 0 {
		t.Fatal("never drank")
	}
	held := 0
	for m.DrinkingPotion() {
		m.Tick(w, nil)
		held++
		if held > 100 {
			t.Fatal("the drink never ended")
		}
	}
	if held != 33 {
		t.Errorf("the drink ended %d ticks after it began, want 33", held)
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

func TestAWitchWithFullHealthNeverDrinksHealing(t *testing.T) {
	w := newTestWorld()
	witchFloor(w, t)
	m := newWitch()
	m.NoAI = true
	if drinkUntil(m, w, nil, 3000) >= 0 {
		t.Errorf("an unhurt, dry, unburnt witch with no target drank %s", potionNameOf(t, m))
	}
}

func TestAWitchDrinksSwiftnessOnlyForAFarTarget(t *testing.T) {
	w := newTestWorld()
	witchFloor(w, t)
	far := newWitch()
	far.NoAI = true
	far.TargetID = 7
	if drinkUntil(far, w, witchPrey(14), 600) < 0 {
		t.Fatal("a witch with a target fourteen blocks away never drank")
	}
	if got := potionNameOf(t, far); got != "minecraft:swiftness" {
		t.Errorf("drank %s, want swiftness", got)
	}
	near := newWitch()
	near.NoAI = true
	near.TargetID = 7
	if drinkUntil(near, w, witchPrey(10), 1500) >= 0 {
		t.Errorf("a witch with a target ten blocks away drank %s", potionNameOf(t, near))
	}
}

func TestAWitchTakesAFractionOfMagicDamage(t *testing.T) {
	for _, tc := range []struct {
		damageType registry.DamageType
		want       float64
	}{
		{rules.DamageTypeMagic, 26 - 10*0.15},
		{rules.DamageTypeIndirectMagic, 26 - 10*0.15},
		{rules.DamageTypeThorns, 26 - 10*0.15},
		{rules.DamageTypeGeneric, 16},
	} {
		m := newWitch()
		if landed, _ := m.plainHurt(10, tc.damageType, false); !landed {
			t.Fatalf("%s did not land", tc.damageType)
		}
		if math.Abs(m.Health-tc.want) > 1e-6 {
			t.Errorf("%s left %v health, want %v", tc.damageType, m.Health, tc.want)
		}
	}
}

func TestAWitchHasTheVanillaBody(t *testing.T) {
	m := newWitch()
	if m.MaxHealth != 26 || m.Health != 26 {
		t.Errorf("health = %v/%v, want 26", m.Health, m.MaxHealth)
	}
	if !MonsterKind(TypeWitch) {
		t.Error("the witch is not a monster")
	}
}

func TestAWitchDropsItsLootTable(t *testing.T) {
	seen := map[string]bool{}
	for seed := range int64(300) {
		m := NewMob(TypeWitch, 1, 0, 64, 0, seed)
		for _, stack := range m.RollDrops(rules.LootKiller{}, nil) {
			name, _ := registry.ItemName(stack.ItemID)
			seen[name] = true
		}
	}
	for _, want := range []string{"minecraft:redstone", "minecraft:glowstone_dust", "minecraft:sugar",
		"minecraft:spider_eye", "minecraft:glass_bottle", "minecraft:gunpowder", "minecraft:stick"} {
		if !seen[want] {
			t.Errorf("300 witch kills never dropped %s", want)
		}
	}
}

func TestAWitchShrugsOffItsOwnHarming(t *testing.T) {
	m := newWitch()
	harm := effect.Instance{ID: mobEffectInstantDamage, Duration: 1}
	if !m.OwnsHarming(harm, m.ID) {
		t.Error("a witch's own harming splash was not shrugged off")
	}
	if m.OwnsHarming(harm, m.ID+1) {
		t.Error("another thrower's harming splash was shrugged off")
	}
	if m.OwnsHarming(effect.Instance{ID: mobEffectPoison, Duration: 1}, m.ID) {
		t.Error("a witch's own poison was shrugged off")
	}
	other := NewMob(TypeZombie, 2, 0, 64, 0, 1)
	if other.OwnsHarming(harm, other.ID) {
		t.Error("a zombie shrugged off its own harming")
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

func TestAWitchOutsideARaidHealsNoRaider(t *testing.T) {
	w := newTestWorld()
	witchFloor(w, t)
	m := newWitch()
	pillager := NewMob(TypePillager, 2, 6.5, 64, 0.5, 1)
	pillager.Health = 3
	if potion, _ := witchThrowsAmong(m, w, nil, []*Mob{m, pillager}, 400); potion != "" {
		t.Errorf("a witch outside a raid threw %q at a pillager", potion)
	}
}

func TestAWitchHealingARaiderLeavesPlayersAloneForAHundredTicks(t *testing.T) {
	w := newTestWorld()
	witchFloor(w, t)
	m := newWitch()
	m.InRaid = true
	pillager := NewMob(TypePillager, 2, 4.5, 64, 0.5, 1)
	others := []*Mob{m, pillager}
	players := witchPrey(8)
	if potion, _ := witchThrowsAmong(m, w, players, others, 400); potion != "minecraft:regeneration" {
		t.Fatalf("the raiding witch's first throw was %q, want regeneration for the pillager", potion)
	}
	pillager.Dead = true
	huntedAt := -1
	for tick := range 400 {
		cooling := m.witch.healCooldown > 0
		m.tick(w, players, others)
		if m.TargetID == players[0].EntityID {
			if cooling {
				t.Fatalf("the witch took the player as its target on tick %d with the heal cooldown running", tick)
			}
			huntedAt = tick
			break
		}
	}
	if huntedAt < 0 {
		t.Fatal("the witch never turned on the player once the cooldown ran out")
	}
}
