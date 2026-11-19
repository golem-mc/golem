package entity

import (
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
