package entity

import (
	"math/rand"
	"testing"
)

// vanilla: Wolf.WolfAvoidEntityGoal.avoidLlama
func TestWolfLlamaAvoidanceStrengthRoll(t *testing.T) {
	wolf := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 1)
	llama := NewMob(TypeLlama, 2, 4.5, 64, 0.5, 2)
	g := newWolfAvoidGoal(wolf)
	for _, tame := range []bool{false, true} {
		wolf.Tamable.Tame = tame
		for strength := int32(1); strength <= 5; strength++ {
			llama.Horse.Strength = strength
			for seed := range int64(30) {
				wolf.Rand = rand.New(rand.NewSource(seed))
				roll := rand.New(rand.NewSource(seed)).Intn(5)
				if got, want := g.avoidLlama(llama), !tame && int32(roll) <= strength; got != want {
					t.Errorf("tame %v, strength %d, roll %d: flee %v, want %v", tame, strength, roll, got, want)
				}
			}
		}
	}
}

// TestWolfLlamaAvoidanceRollsForTheNearest checks that a rejected roll leaves
// navigation alone rather than falling back to a stronger llama farther away.
func TestWolfLlamaAvoidanceRollsForTheNearest(t *testing.T) {
	w := newTestWorld()
	floor(w, -32, -32, 32, 32, 63, defaultState(t, "minecraft:stone"))
	wolf := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 1)
	wolf.OnGround = true
	near := NewMob(TypeLlama, 2, 4.5, 64, 0.5, 2)
	far := NewMob(TypeLlama, 3, 8.5, 64, 0.5, 3)
	near.Horse.Strength, far.Horse.Strength = 1, 5
	g := newWolfAvoidGoal(wolf)
	rejected, accepted := 0, 0
	g.canFlee = func(o *Mob) bool {
		if o != near {
			t.Fatal("the strength roll was for the farther llama")
		}
		if g.avoidLlama(o) {
			accepted++
			return true
		}
		rejected++
		return false
	}
	for range 100 {
		wolf.nav.path = nil
		if !g.CanStart(&tickView{w: w, others: []*Mob{far, near}}) && len(wolf.nav.path) != 0 {
			t.Fatal("a rejected escape changed navigation")
		}
	}
	if rejected == 0 || accepted == 0 {
		t.Fatalf("strength-one llama: %d accepted, %d rejected escapes, want both", accepted, rejected)
	}
}

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

// TestWolfLlamaAvoidanceRejectsThreats checks the gates before movement starts.
func TestWolfLlamaAvoidanceRejectsThreats(t *testing.T) {
	w := newTestWorld()
	floor(w, -32, -32, 32, 32, 63, defaultState(t, "minecraft:stone"))
	for _, scene := range []string{"tame", "distant", "above", "dead", "other", "hidden"} {
		t.Run(scene, func(t *testing.T) {
			wolf := NewMob(TypeWolf, 1, 0.5, 64, 0.5, 1)
			wolf.OnGround = true
			llama := NewMob(TypeLlama, 2, 4.5, 64, 0.5, 2)
			llama.Horse.Strength = 5
			switch scene {
			case "tame":
				wolf.Tamable.Tame = true
			case "distant":
				llama.X = 24.6
			case "above":
				llama.Y += 5
			case "dead":
				llama.Dead = true
			case "other":
				llama.Type = TypeHorse
			case "hidden":
				for y := int32(64); y < 68; y++ {
					w.set(2, y, 0, defaultState(t, "minecraft:stone"))
				}
			}
			g := newWolfAvoidGoal(wolf)
			for range 30 {
				if g.CanStart(&tickView{w: w, others: []*Mob{llama}}) || len(wolf.nav.path) != 0 {
					t.Fatal("wolf planned a flight from an ineligible threat")
				}
			}
		})
	}
}
