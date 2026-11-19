package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// convertedAt is the tick a hoglin on a stone floor reports MobConverted on,
// 0 when it never does in 400 ticks.
func convertedAt(nether bool) int {
	w := newTestWorld()
	w.nether = nether
	stone := registry.BlockStates["minecraft:stone"]
	for x := int32(-3); x <= 3; x++ {
		for z := int32(-3); z <= 3; z++ {
			w.set(x, 63, z, stone)
		}
	}
	m := NewMob(TypeHoglin, 1, 0.5, 64, 0.5, 1)
	for tick := 1; tick <= 400; tick++ {
		for _, ev := range m.Tick(w, nil) {
			if ev.Kind == MobConverted && ev.Into == TypeZoglin {
				return tick
			}
		}
	}
	return 0
}

func TestAHoglinConvertsOnTheThreeHundredFirstTickOutsideTheNether(t *testing.T) {
	if got := convertedAt(false); got != hoglinConvertTicks+1 {
		t.Errorf("converted at tick %d, want %d", got, hoglinConvertTicks+1)
	}
	if got := convertedAt(true); got != 0 {
		t.Errorf("a hoglin in the Nether converted at tick %d", got)
	}
}

func TestAnImmuneHoglinDoesNotConvert(t *testing.T) {
	w := newTestWorld()
	m := NewMob(TypeHoglin, 1, 0.5, 64, 0.5, 1)
	m.ZombificationImmune = true
	for range 400 {
		for _, ev := range m.Tick(w, nil) {
			if ev.Kind == MobConverted {
				t.Fatal("an immune hoglin converted")
			}
		}
	}
}

func TestABabyHoglinDealsHalfAPointAndAnAdultThrows(t *testing.T) {
	baby := NewMob(TypeHoglin, 1, 0.5, 64, 0.5, 1)
	baby.SetBaby(true)
	var ev MobEvent
	baby.hoglinBlow(&ev)
	if ev.Damage != hoglinBabyAttack || ev.Hoglin.Throw {
		t.Errorf("baby blow %v throw %v, want 0.5 and none", ev.Damage, ev.Hoglin.Throw)
	}
	adult := NewMob(TypeHoglin, 2, 0.5, 64, 0.5, 1)
	ev = MobEvent{}
	adult.hoglinBlow(&ev)
	if ev.Damage < 3 || ev.Damage > 8 || !ev.Hoglin.Throw {
		t.Errorf("adult blow %v throw %v, want 3 to 8 and a throw", ev.Damage, ev.Hoglin.Throw)
	}
}

func TestHoglinsAndZoglinsRollBabiesWithoutAGroup(t *testing.T) {
	for _, kind := range []MobType{TypeHoglin, TypeZoglin} {
		babies := 0
		for i := range 400 {
			m := NewMob(kind, int32(i+1), 0.5, 64, 0.5, int64(i))
			m.FinalizeSpawn(GoDice{m.Rand}, testBiome("minecraft:nether_wastes"), nil, rules.LocalDifficulty{})
			if m.IsBaby() {
				babies++
			}
		}
		// vanilla: Hoglin.finalizeSpawn, 0.2 of 400 is 80
		if babies < 50 || babies > 110 {
			t.Errorf("%s: %d babies of 400, want about 80", kind, babies)
		}
	}
}

func TestAPacifiedHoglinCannotBeFedIntoLove(t *testing.T) {
	m := NewMob(TypeHoglin, 1, 0.5, 64, 0.5, 1)
	m.hoglin.pacified = 5
	if loved, _ := m.Feed(); loved || m.IsInLove() {
		t.Error("a pacified hoglin fell in love")
	}
	m.hoglin.pacified = 0
	if loved, _ := m.Feed(); !loved {
		t.Error("an unpacified hoglin did not fall in love")
	}
}

// TestTheHuntHasNoExpiryButAGrudgeHas: StartAttacking sets the target with no
// expiry, while a retaliation holds it for 200 ticks.
// vanilla: StartAttacking, HoglinAi.setAttackTarget
func TestTheHuntHasNoExpiryButAGrudgeHas(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	view := &tickView{w: w, players: []Player{survivor(7, 5.5)}}

	hunt := NewMob(TypeHoglin, 1, 0.5, 64, 0.5, 1)
	hunt.TargetID, hunt.Age = 7, 5000
	if !goalOf[*hoglinTargetGoal](hoglinGoals(hunt)).ShouldContinue(view) {
		t.Error("a hoglin's hunt ended with its age")
	}
	zhunt := NewMob(TypeZoglin, 2, 0.5, 64, 0.5, 1)
	zhunt.TargetID, zhunt.Age = 7, 5000
	if !goalOf[*zoglinTargetGoal](zoglinGoals(zhunt)).ShouldContinue(view) {
		t.Error("a zoglin's hunt ended with its age")
	}

	for _, m := range []*Mob{NewMob(TypeHoglin, 3, 0.5, 64, 0.5, 1), NewMob(TypeZoglin, 4, 0.5, 64, 0.5, 1)} {
		var g Goal = goalOf[*hoglinHurtGoal](hoglinGoals(m))
		if m.Type == TypeZoglin {
			g = goalOf[*grudgeGoal](zoglinGoals(m))
		}
		m.retaliate, m.Age = 7, 1000
		g.Start(view)
		m.Age += 200
		if !g.ShouldContinue(view) {
			t.Errorf("%s: the grudge ended after 200 ticks", m.Type)
		}
		m.Age++
		if g.ShouldContinue(view) {
			t.Errorf("%s: the grudge outlived 200 ticks", m.Type)
		}
	}
}

// goalOf returns the goal of type G in goals, or nil when there is none, so a
// test does not depend on a goal's position in the list.
func goalOf[G Goal](goals []Goal) G {
	for _, g := range goals {
		if g, ok := g.(G); ok {
			return g
		}
	}
	var zero G
	return zero
}

// A second blow while the grudge runs restarts its 200 ticks.
// vanilla: HoglinAi.setAttackTarget
func TestASecondBlowRestartsTheGrudge(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	view := &tickView{w: w, players: []Player{survivor(7, 5.5)}}
	for _, m := range []*Mob{NewMob(TypeHoglin, 1, 0.5, 64, 0.5, 1), NewMob(TypeZoglin, 2, 0.5, 64, 0.5, 1)} {
		var g Goal = goalOf[*hoglinHurtGoal](hoglinGoals(m))
		if m.Type == TypeZoglin {
			g = goalOf[*grudgeGoal](zoglinGoals(m))
		}
		m.retaliate, m.Age = 7, 1000
		g.Start(view)
		m.Age += 150
		m.retaliate = 7
		g.Tick(view)
		m.Age += 150
		if !g.ShouldContinue(view) {
			t.Errorf("%s: the second blow did not restart the grudge", m.Type)
		}
		m.Age += 51
		if g.ShouldContinue(view) {
			t.Errorf("%s: the grudge outlived 200 ticks of the second blow", m.Type)
		}
	}
}

func TestPiglinRepellentNeedsALitSoulCampfire(t *testing.T) {
	t.Parallel()
	state := func(name string, props map[string]string) uint16 {
		s, ok := registry.FindState(name, props)
		if !ok {
			t.Fatalf("no state %s %v", name, props)
		}
		return s
	}
	for name, tc := range map[string]struct {
		state uint16
		want  bool
	}{
		"lit soul campfire":   {state("minecraft:soul_campfire", map[string]string{"lit": "true"}), true},
		"unlit soul campfire": {state("minecraft:soul_campfire", map[string]string{"lit": "false"}), false},
		"soul torch":          {state("minecraft:soul_torch", nil), true},
		"soul wall torch":     {state("minecraft:soul_wall_torch", nil), true},
		"soul lantern":        {state("minecraft:soul_lantern", nil), true},
		"soul fire":           {state("minecraft:soul_fire", nil), true},
		"torch":               {state("minecraft:torch", nil), false},
		"lit campfire":        {state("minecraft:campfire", map[string]string{"lit": "true"}), false},
	} {
		if got := isPiglinRepellent(tc.state); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}

// vanilla: Hoglin.getBreedOffspring
func TestABredHoglinBabyIsPersistent(t *testing.T) {
	mother := NewMob(TypeHoglin, 1, 0.5, 64, 0.5, 1)
	father := NewMob(TypeHoglin, 2, 1.5, 64, 0.5, 2)
	baby := NewMob(TypeHoglin, 3, 0.5, 64, 0.5, 3)
	mother.BreedInto(baby, father)
	if !baby.Persistence {
		t.Error("the hoglin baby is not persistent")
	}
}
