package entity

import "testing"

func TestABabyPiglinRidesANearbyBabyHoglinThenGetsOffWhenItExpires(t *testing.T) {
	w, ps, hs := huntWorld(t, 1, 1)
	baby, calf := ps[0], hs[0]
	baby.SetBaby(true)
	calf.SetBaby(true)
	calf.X, calf.Z = baby.X+2, baby.Z
	mobs := all(ps, hs)
	var seated int
	for range 1500 {
		for _, m := range mobs {
			for _, ev := range m.tick(w, nil, mobs) {
				if ev.Kind == MobSeated {
					seated++
				}
			}
		}
		if baby.Vehicle == calf {
			break
		}
	}
	if baby.Vehicle != calf || seated == 0 {
		t.Fatalf("vehicle %v after the start interval, want the baby hoglin", baby.Vehicle)
	}
	baby.barter.rideLeft = 1
	runHunt(w, mobs, 3)
	if baby.Vehicle != nil {
		t.Error("the baby stays on after the ride target expired")
	}
}
