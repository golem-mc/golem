package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// pounceScene is a fox mid-crouch beside a chicken it targets, on stone.
func pounceScene(t *testing.T) (testWorld, *Mob, *Mob) {
	t.Helper()
	w := newTestWorld()
	floor64(w, t)
	fox := NewMob(TypeFox, 2, 0.5, 64, 0.5, 3)
	chicken := NewMob(TypeChicken, 3, 3.5, 64, 0.5, 4)
	fox.TargetID = chicken.ID
	return w, fox, chicken
}

// TestAPouncingFoxPitchesAlongItsFlight: the leap tilts the head along the
// climb, then down on the fall.
// vanilla: Fox.FoxPounceGoal.tick
func TestAPouncingFoxPitchesAlongItsFlight(t *testing.T) {
	w, fox, chicken := pounceScene(t)
	view := &tickView{w: w, others: []*Mob{fox, chicken}}
	g := &foxPounceGoal{m: fox}
	g.Start(view)
	fox.VY = 0.9
	g.Tick(view)
	if fox.Pitch >= -30 {
		t.Errorf("pitch on the climb = %v, want a steep upward tilt", fox.Pitch)
	}
	fox.VX, fox.VY = 0.3, -0.5
	g.Tick(view)
	if fox.Pitch <= 0 {
		t.Errorf("pitch on the fall = %v, want a downward tilt", fox.Pitch)
	}
}

// TestAFoxPouncingIntoSnowFaceplants: landing head-down in a snow layer
// drops the target, plants the head at sixty degrees, which the look control
// turns thirty back toward the target the tick began with, and stops the leap.
// vanilla: Fox.FoxPounceGoal.tick, Fox.FaceplantGoal
func TestAFoxPouncingIntoSnowFaceplants(t *testing.T) {
	w, fox, chicken := pounceScene(t)
	chicken.X = 9.5
	w.set(0, 64, 0, defaultState(t, "minecraft:snow"))
	if registry.BlockOf(w.BlockAt(0, 64, 0)) != registry.BlockSnow {
		t.Fatal("the test cell is not snow")
	}
	view := &tickView{w: w, others: []*Mob{fox, chicken}}
	g := &foxPounceGoal{m: fox}
	g.Start(view)
	g.Tick(view)
	fox.OnGround, fox.VX, fox.VY = true, 0.3, -0.5
	g.Tick(view)
	if !fox.Fox.Faceplanted || fox.Pitch != 30 || fox.TargetID != 0 {
		t.Fatalf("faceplanted = %v, pitch = %v, target = %d", fox.Fox.Faceplanted, fox.Pitch, fox.TargetID)
	}
	if g.ShouldContinue(view) {
		t.Error("the pounce goes on after the faceplant")
	}
}

// TestAFoxLandingOnBareGroundDoesNotFaceplant is the same dive without snow.
func TestAFoxLandingOnBareGroundDoesNotFaceplant(t *testing.T) {
	w, fox, chicken := pounceScene(t)
	chicken.X = 9.5
	view := &tickView{w: w, others: []*Mob{fox, chicken}}
	g := &foxPounceGoal{m: fox}
	g.Start(view)
	g.Tick(view)
	fox.OnGround, fox.VX, fox.VY = true, 0.3, -0.5
	g.Tick(view)
	if fox.Fox.Faceplanted {
		t.Error("the fox faceplanted on bare ground")
	}
}

// TestAFaceplantedFoxStaysForTwoSeconds: the faceplant goal holds the fox
// for forty ticks and then lets the flag go.
// vanilla: Fox.FaceplantGoal
func TestAFaceplantedFoxStaysForTwoSeconds(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	fox := NewMob(TypeFox, 2, 0.5, 64, 0.5, 3)
	fox.Fox.Faceplanted = true
	view := &tickView{w: w}
	ticks := 0
	for ; fox.Fox.Faceplanted && ticks < 100; ticks++ {
		fox.ai.run(fox, view)
		fox.Age++
	}
	if ticks < 38 || ticks > 42 {
		t.Errorf("the faceplant lasted %d ticks, want about 40", ticks)
	}
}

// TestAPouncingFoxCannotBePreempted: a goal of better priority on the same
// controls waits for the leap to end.
// vanilla: Fox.FoxPounceGoal.isInterruptable
func TestAPouncingFoxCannotBePreempted(t *testing.T) {
	w, fox, chicken := pounceScene(t)
	fox.ai.goals, fox.ai.running = nil, nil
	pounce := &foxPounceGoal{m: fox}
	rival := &controlGoal{m: fox, flags: GoalMove, priority: 1}
	fox.ai.wire(rival, pounce)
	fox.ai.running = append(fox.ai.running, pounce)
	view := &tickView{w: w, others: []*Mob{fox, chicken}}
	fox.ai.tick(view, 0)
	if fox.ai.isRunning(rival) || !fox.ai.isRunning(pounce) {
		t.Error("a better goal took the controls from the leap")
	}
	if math.IsNaN(float64(fox.Pitch)) {
		t.Error("pitch is NaN")
	}
}

// TestAFoxScreechesOnlyAtNightAwayFromPlayers: the idle sound becomes the
// screech, at twice the volume, in the dark with nobody within sixteen
// blocks.
// vanilla: Fox.getAmbientSound, Fox.playAmbientSound
func TestAFoxScreechesOnlyAtNightAwayFromPlayers(t *testing.T) {
	screeches := func(bright bool, players []Player) (n int) {
		w := newTestWorld()
		w.bright = bright
		fox := NewMob(TypeFox, 2, 0.5, 64, 0.5, 3)
		for range 500 {
			moan := MobEvent{Mob: fox, Kind: MobMoaned}
			fox.foxScreech(w, players, &moan)
			if moan.SoundID.Name() == "minecraft:entity.fox.screech" {
				n++
				if moan.Volume != 2 {
					t.Errorf("screech volume %v, want 2", moan.Volume)
				}
			}
		}
		return n
	}
	if screeches(false, nil) == 0 {
		t.Error("no screech at night with nobody near")
	}
	if n := screeches(true, nil); n != 0 {
		t.Errorf("%d screeches by day", n)
	}
	if n := screeches(false, []Player{{X: 10, Y: 64, Z: 0.5}}); n != 0 {
		t.Errorf("%d screeches with a player ten blocks off", n)
	}
	if screeches(false, []Player{{X: 40, Y: 64, Z: 0.5}}) == 0 {
		t.Error("a player forty blocks off silenced the screech")
	}
	if screeches(false, []Player{{X: 10, Y: 64, Z: 0.5, Spectator: true}}) == 0 {
		t.Error("a spectator silenced the screech")
	}
}

// TestAFoxSniffsOnTheWayToBerries: the goal sniffs now and then while the
// fox has not yet reached the bush.
// vanilla: Fox.FoxEatBerriesGoal.tick
func TestAFoxSniffsOnTheWayToBerries(t *testing.T) {
	w := newTestWorld()
	floor64(w, t)
	fox := NewMob(TypeFox, 2, 0.5, 64, 0.5, 3)
	g := newFoxBerriesGoal(fox)
	view := &tickView{w: w}
	for range 400 {
		g.Tick(view)
	}
	var sniffs int
	for _, ev := range view.events {
		if ev.Kind == MobSounded && ev.SoundID.Name() == "minecraft:entity.fox.sniff" {
			sniffs++
		}
	}
	if sniffs == 0 {
		t.Error("no sniff on the way")
	}
}

// TestAFaceplantedFoxKicksUpTheBlockItStandsIn: the dust event names the
// block state of the cell, now and then.
// vanilla: Fox.tick
func TestAFaceplantedFoxKicksUpTheBlockItStandsIn(t *testing.T) {
	w := newTestWorld()
	snow := defaultState(t, "minecraft:snow")
	w.set(0, 64, 0, snow)
	fox := NewMob(TypeFox, 2, 0.5, 64, 0.5, 3)
	fox.Fox.Faceplanted = true
	var seen int
	for range 100 {
		for _, ev := range fox.foxStep(w, nil) {
			if ev.Kind == MobFoxFaceplantDust && ev.Status == int32(snow) {
				seen++
			}
		}
	}
	if seen == 0 {
		t.Error("no dust around the faceplanted fox")
	}
	fox.Fox.Faceplanted = false
	if evs := fox.foxStep(w, nil); len(evs) != 0 {
		t.Error("dust around a fox that is not faceplanted")
	}
}

// TestAFoxKeepsItsPitchThroughARealTickWhileItStalksOrPounces: Mob.Tick eases
// a head no goal holds back to level, except for a fox that is pouncing,
// crouching, interested or faceplanted.
// vanilla: Fox.FoxLookControl.resetXRotOnTick
func TestAFoxKeepsItsPitchThroughARealTickWhileItStalksOrPounces(t *testing.T) {
	for name, set := range map[string]func(*FoxState){
		"pouncing":   func(f *FoxState) { f.Pouncing = true },
		"crouching":  func(f *FoxState) { f.Crouching = true },
		"interested": func(f *FoxState) { f.Interested = true },
		"faceplant":  func(f *FoxState) { f.Faceplanted = true },
		"idle":       func(*FoxState) {},
	} {
		w, fox, chicken := pounceScene(t)
		_ = chicken
		fox.ai.goals, fox.ai.running = nil, nil
		set(&fox.Fox)
		fox.Pitch = -22
		fox.Tick(w, nil)
		if held := name != "idle"; held != (fox.Pitch == -22) {
			t.Errorf("%s: pitch after a tick = %v", name, fox.Pitch)
		}
	}
}
