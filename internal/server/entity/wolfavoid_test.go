package entity

import (
	"testing"
)

// vanilla: Wolf.registerGoals, Wolf.WolfAvoidEntityGoal
func TestWolfLlamaAvoidanceSelectsAndClearsTarget(t *testing.T) {
	w := newTestWorld()
	floor(w, -32, -32, 32, 32, 63, defaultState(t, "minecraft:stone"))
	for _, kind := range []MobType{TypeLlama, TypeTraderLlama} {
		wolf := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 1)
		wolf.OnGround = true
		llama := NewMob(kind, 2, 4.5, 64, 0.5, 2)
		llama.Horse.Strength = 5
		view := &tickView{w: w, others: []*Mob{llama}}
		g := newWolfAvoidGoal(wolf)
		started := false
		for range 100 {
			if g.CanStart(view) {
				started = true
				break
			}
		}
		if !started {
			t.Fatalf("no escape planned from %s", kind)
		}
		// The selector stops the previous mover between CanStart and Start.
		(&meleeAttackGoal{m: wolf}).Stop(view)
		wolf.TargetID = 7
		g.Start(view)
		if wolf.TargetID != 0 || len(wolf.nav.path) == 0 || wolf.nav.speedFactor != 1.5 {
			t.Fatalf("start: target %d, path %v, pace %v", wolf.TargetID, wolf.nav.path, wolf.nav.speedFactor)
		}
		for _, x := range []float64{4.5, 20.5} {
			llama.X, wolf.TargetID = x, 7
			g.Tick(view)
			if wolf.TargetID != 0 || wolf.nav.speedFactor != 1.5 {
				t.Errorf("llama at %v: target %d, pace %v, want no target and pace 1.5", x, wolf.TargetID, wolf.nav.speedFactor)
			}
		}
		wolf.Tamable.Tame = true
		if !g.ShouldContinue(view) {
			t.Error("taming interrupted the existing escape path")
		}
		wolf.nav.path = nil
		if g.ShouldContinue(view) {
			t.Error("a finished escape path continued")
		}
	}
}
