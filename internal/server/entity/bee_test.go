package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

// TestAStungBeeDiesAtTheLatestOnTheTwelveHundredthTick holds Bee.
// customServerAiStep's worst case: the death roll is made on every fifth tick
// since the sting and cannot miss once the countdown has run out.
func TestAStungBeeDiesAtTheLatestOnTheTwelveHundredthTick(t *testing.T) {
	for seed := range int64(100) {
		m := NewMob(TypeBee, 1, 0, 64, 0, seed)
		m.Sting()
		tv := &tickView{w: newTestWorld()}
		died := 0
		for tick := 1; tick <= 1200 && died == 0; tick++ {
			m.beeStep(tv)
			if m.Dead {
				died = tick
			}
		}
		if died == 0 {
			t.Fatalf("seed %d: the bee is alive 1200 ticks after its sting", seed)
		}
		if died%5 != 0 {
			t.Fatalf("seed %d: the bee died on tick %d, want a multiple of 5", seed, died)
		}
	}
}

func TestAStungBeeNeverDiesBeforeTheFirstRoll(t *testing.T) {
	for seed := range int64(100) {
		m := NewMob(TypeBee, 1, 0, 64, 0, seed)
		m.Sting()
		tv := &tickView{w: newTestWorld()}
		for range 4 {
			m.beeStep(tv)
		}
		if m.Dead {
			t.Fatalf("seed %d: the bee died before the fifth tick", seed)
		}
	}
}

func TestAnUnstungBeeLivesOn(t *testing.T) {
	m := NewMob(TypeBee, 1, 0, 64, 0, 1)
	tv := &tickView{w: newTestWorld()}
	for range 3000 {
		m.beeStep(tv)
	}
	if m.Dead || m.Health != 10 {
		t.Errorf("an unstung bee has %v health, dead %v", m.Health, m.Dead)
	}
}

// TestABeeDrownsAfterTwentyTicksInWater: the first bite comes on the 21st
// tick in water, Bee.customServerAiStep's underWaterTicks > 20.
func TestABeeDrownsAfterTwentyTicksInWater(t *testing.T) {
	m := NewMob(TypeBee, 1, 0, 64, 0, 1)
	m.waterFluid.height = 1
	tv := &tickView{w: newTestWorld()}
	for range 20 {
		m.beeStep(tv)
	}
	if m.Health != 10 {
		t.Fatalf("health after 20 ticks in water = %v, want 10", m.Health)
	}
	m.beeStep(tv)
	if m.Health != 9 {
		t.Errorf("health on the 21st tick in water = %v, want 9", m.Health)
	}
	if len(tv.events) != 1 || tv.events[0].DamageType != rules.DamageTypeDrown {
		t.Errorf("events = %+v, want one drowning bite", tv.events)
	}
}

func TestABeeSting(t *testing.T) {
	m := NewMob(TypeBee, 1, 0, 64, 0, 1)
	m.SetAnger(100, [16]byte{}, false)
	m.Sting()
	if !m.HasStung() || m.Angry() {
		t.Errorf("after the sting: stung %v, angry %v; want stung and calm", m.HasStung(), m.Angry())
	}
	if got := m.BeeFlags(); got != BeeFlagStung {
		t.Errorf("flags = %#x, want %#x", got, BeeFlagStung)
	}
	m.SetBeeState(false, true)
	if got := m.BeeFlags(); got != BeeFlagNectar {
		t.Errorf("flags = %#x, want %#x", got, BeeFlagNectar)
	}
	if a := m.kind().attackDamage; a != 2 {
		t.Errorf("attack damage = %v, want 2", a)
	}
}

// TestABeesStingPoisonsByDifficulty: ten seconds on normal, eighteen on hard,
// none on easy.
func TestABeesStingPoisonsByDifficulty(t *testing.T) {
	m := NewMob(TypeBee, 1, 0, 64, 0, 1)
	for _, c := range []struct {
		difficulty uint8
		ticks      int32
		ok         bool
	}{
		{rules.DifficultyPeaceful, 0, false},
		{rules.DifficultyEasy, 0, false},
		{rules.DifficultyNormal, 200, true},
		{rules.DifficultyHard, 360, true},
	} {
		name, ticks, ok := m.BlowEffect(c.difficulty, 1)
		if ok != c.ok || ticks != c.ticks || (ok && name != "minecraft:poison") {
			t.Errorf("difficulty %d: %q %d %v, want poison %d %v", c.difficulty, name, ticks, ok, c.ticks, c.ok)
		}
	}
}

// TestABeeCallsOtherBeesOnlyWhenItSeesTheAttacker: BeeHurtByOtherGoal
// alerts a bee only if the struck one has a line of sight to the attacker,
// and only within the follow range.
// vanilla: Bee.BeeHurtByOtherGoal.alertOther
func TestABeeCallsOtherBeesOnlyWhenItSeesTheAttacker(t *testing.T) {
	player := Player{EntityID: 99, X: 0.5, Y: 64, Z: 0.5, Huntable: true}
	setup := func(wall bool, otherX float64) *Mob {
		w := newTestWorld()
		if wall {
			for y := int32(63); y <= 67; y++ {
				for z := int32(-3); z <= 3; z++ {
					w.set(3, y, z, defaultState(t, "minecraft:stone"))
				}
			}
		}
		struck := NewMob(TypeBee, 1, 6.5, 64, 0.5, 1)
		other := NewMob(TypeBee, 2, otherX, 64, 0.5, 2)
		g := &hurtByGoal{m: struck, alertOthers: true, sight: true}
		g.alertNearby(&tickView{w: w, players: []Player{player}, others: []*Mob{struck, other}}, player.EntityID)
		return other
	}
	if got := setup(false, 8.5).TargetID; got != player.EntityID {
		t.Errorf("a bee 2 blocks away targets %d, want the attacker", got)
	}
	if got := setup(true, 8.5).TargetID; got != 0 {
		t.Errorf("with a wall between the struck bee and the attacker the neighbour targets %d, want nobody", got)
	}
	if got := setup(false, 40.5).TargetID; got != 0 {
		t.Errorf("a bee 34 blocks away targets %d, want nobody", got)
	}
}

// TestABeeIsWorthOneToThreeExperienceAndDropsNothing: Animal's xpReward of
// 1 + nextInt(3), and an entities/bee table with no pools.
func TestABeeIsWorthOneToThreeExperienceAndDropsNothing(t *testing.T) {
	seen := map[int]bool{}
	for seed := range int64(200) {
		m := NewMob(TypeBee, 1, 0, 64, 0, seed)
		got := m.RollExperience(rules.LootKiller{})
		if got < 1 || got > 3 {
			t.Fatalf("seed %d: experience %d, want 1 to 3", seed, got)
		}
		seen[got] = true
	}
	if len(seen) != 3 {
		t.Errorf("rolled %v, want all of 1, 2 and 3", seen)
	}
	m := NewMob(TypeBee, 1, 0, 64, 0, 1)
	if drops := m.RollDrops(rules.LootKiller{}, nil); len(drops) != 0 {
		t.Errorf("a bee drops %+v, want nothing", drops)
	}
}

// TestABeeAttackGoalRefusesTheTickAngerStartsAndThenWaitsOutItsWindow:
// Bee.java runs MeleeAttackGoal.canUse (with its 20-tick window) before isAngry,
// and canContinueToUse has no window.
func TestABeeAttackGoalRefusesTheTickAngerStartsAndThenWaitsOutItsWindow(t *testing.T) {
	m := NewMob(TypeBee, 1, 0, 64, 0, 1)
	m.Age = 100
	tv := &tickView{w: newTestWorld(), players: []Player{{EntityID: 7, X: 0.5, Y: 64, Z: 0.5, Huntable: true}}}
	m.SetAnger(400, [16]byte{}, false)
	m.SetMetadataTargetID(7)
	g := &beeAttackGoal{m: m}

	m.angerStep(tv)
	if !m.bee.angerStartedNow {
		t.Fatal("angerStep on a new target did not mark the anger as started this tick")
	}
	if g.CanStart(tv) {
		t.Fatal("CanStart on the tick the anger started = true, want false")
	}
	if !g.ShouldContinue(tv) {
		t.Fatal("ShouldContinue = false, want true: it has no window")
	}
	for age := 101; age < 120; age++ {
		m.Age = age
		m.angerStep(tv)
		if g.CanStart(tv) {
			t.Fatalf("CanStart at age %d = true inside the 20-tick window", age)
		}
	}
	m.Age = 120
	m.angerStep(tv)
	if !g.CanStart(tv) {
		t.Fatal("CanStart at age 120 = false, want true after the window")
	}
}

// TestAFreshBeeAtAgeZeroIsNotTakenForOneWhoseAngerJustStarted: the goal's first
// pass on a bee that was angry already is not refused.
func TestAFreshBeeAtAgeZeroIsNotTakenForOneWhoseAngerJustStarted(t *testing.T) {
	m := NewMob(TypeBee, 1, 0, 64, 0, 1)
	tv := &tickView{w: newTestWorld(), players: []Player{{EntityID: 7, X: 0.5, Y: 64, Z: 0.5, Huntable: true}}}
	m.SetAnger(400, [16]byte{}, false)
	m.SetMetadataTargetID(7)
	if m.Age != 0 {
		t.Fatalf("Age = %d, want 0", m.Age)
	}
	g := &beeAttackGoal{m: m}
	if !g.CanStart(tv) {
		t.Fatal("CanStart on a fresh angry bee at Age 0 = false, want true")
	}
}
