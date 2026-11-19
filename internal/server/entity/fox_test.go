package entity

import (
	"testing"
)

// TestASleepingFoxSnores: a sleeping fox's ambient sound is its sleep sound.
// vanilla: Fox.getAmbientSound
func TestASleepingFoxSnores(t *testing.T) {
	fox := NewMob(TypeFox, 1, 0, 64, 0, 1)
	if got := fox.Sounds().Ambient.Name(); got != "minecraft:entity.fox.ambient" {
		t.Errorf("awake ambient %q", got)
	}
	fox.Fox.Sleeping = true
	if got := fox.Sounds().Ambient.Name(); got != "minecraft:entity.fox.sleep" {
		t.Errorf("sleeping ambient %q", got)
	}
}

// TestAFoxStrollsTowardAVillageAtNight: near a village at night the stroll
// starts, and does not with no village near.
// vanilla: StrollThroughVillageGoal.canUse
func TestAFoxStrollsTowardAVillageAtNight(t *testing.T) {
	w := newTestWorld()
	stone := defaultState(t, "minecraft:stone")
	for x := int32(-20); x <= 20; x++ {
		for z := int32(-20); z <= 20; z++ {
			w.set(x, 63, z, stone)
		}
	}
	fox := NewMob(TypeFox, 1, 0.5, 64, 0.5, 1)
	fox.Village = fakeVillage{w: w, pois: []Waypoint{{X: 48, Y: 64, Z: 0}}}
	g := &strollThroughVillage{m: fox, interval: 1}
	w.bright = true
	if g.CanStart(&tickView{w: w}) {
		t.Fatal("started by day")
	}
	w.bright = false
	// A pick lands on the one floor layer about one time in fifteen.
	started := false
	for range 100 {
		if started = g.CanStart(&tickView{w: w}); started {
			break
		}
	}
	if !started {
		t.Fatal("did not start at night near a village")
	}
	fox.Village = fakeVillage{w: w}
	if g.CanStart(&tickView{w: w}) {
		t.Error("started with no village near")
	}
}
