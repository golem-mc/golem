package entity

import (
	"testing"

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
