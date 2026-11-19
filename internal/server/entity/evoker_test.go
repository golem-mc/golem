package entity

import (
	"math"
	"testing"
)

// castFangs runs the fang spell of an evoker at the origin against a player
// dx blocks east and returns the fangs it raised.
func castFangs(t *testing.T, dx float64) []MobEvent {
	t.Helper()
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-5); x <= 20; x++ {
		for z := int32(-5); z <= 5; z++ {
			w.set(x, 63, z, stone)
		}
	}
	m := NewMob(TypeEvoker, 1, 0.5, 64, 0.5, 1)
	m.TargetID = 9
	v := &tickView{w: w, players: []Player{{EntityID: 9, X: 0.5 + dx, Y: 64, Z: 0.5, Huntable: true}}}
	fangsSpell{}.perform(&spellGoal{m: m, spell: fangsSpell{}}, v)
	var out []MobEvent
	for _, e := range v.events {
		if e.Kind == MobCastFangs {
			out = append(out, e)
		}
	}
	return out
}

// TestFangSpellRingsANearTargetAndLinesAFarOne: inside three blocks five fangs
// stand 1.5 out and eight 2.5 out, delayed 3; beyond, sixteen run in a line.
// vanilla: Evoker.EvokerAttackSpellGoal.performSpellCasting
func TestFangSpellRingsANearTargetAndLinesAFarOne(t *testing.T) {
	near := castFangs(t, 2)
	if len(near) != evokerFangsInner+evokerFangsOuter {
		t.Fatalf("near cast raised %d fangs, want 13", len(near))
	}
	inner, outer := 0, 0
	for _, e := range near {
		r := math.Hypot(e.X-0.5, e.Z-0.5)
		switch {
		case math.Abs(r-1.5) < 1e-4 && e.Amount == 0:
			inner++
		case math.Abs(r-2.5) < 1e-4 && e.Amount == 3:
			outer++
		}
	}
	if inner != 5 || outer != 8 {
		t.Errorf("ring has %d inner and %d outer fangs, want 5 and 8", inner, outer)
	}
	far := castFangs(t, 10)
	if len(far) != evokerFangsLine {
		t.Fatalf("far cast raised %d fangs, want %d", len(far), evokerFangsLine)
	}
	for i, e := range far {
		wantX := 0.5 + evokerFangsStep*float64(i+1)
		if math.Abs(e.X-wantX) > 1e-4 || math.Abs(e.Z-0.5) > 1e-4 || e.Amount != i {
			t.Errorf("line fang %d at (%v, %v) delay %d, want x %v delay %d", i, e.X, e.Z, e.Amount, wantX, i)
		}
	}
}
