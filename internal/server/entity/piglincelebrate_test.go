package entity

import (
	"testing"
)

func TestAPiglinCelebratesWhereItsTargetDied(t *testing.T) {
	w, ps, hs := huntWorld(t, 1, 1)
	ps[0].TargetID = hs[0].ID
	hs[0].Health = 0
	events := runHunt(w, []*Mob{ps[0], hs[0]}, 1)
	b := &ps[0].barter
	if b.celebrate == 0 || ps[0].TargetID != 0 {
		t.Fatalf("celebrate %d target %d, want a celebration and no target", b.celebrate, ps[0].TargetID)
	}
	if b.x != -4 || b.z != 2 {
		t.Errorf("celebrates at %d,%d, want the hoglin's block -4,2", b.x, b.z)
	}
	var sound bool
	for _, ev := range events {
		sound = sound || ev.Kind == MobSounded && ev.SoundID.Name() == "minecraft:entity.piglin.celebrate"
	}
	if !sound {
		t.Error("no celebrate sound")
	}
	runHunt(w, []*Mob{ps[0]}, 60)
	if d := Dist2(ps[0].X, ps[0].Y, ps[0].Z, -3.5, 64, 2.5); d > 9 {
		t.Errorf("piglin is %v from the kill after 60 ticks", d)
	}
}

func TestABlowEndsACelebrationUnlessItsFromAPiglin(t *testing.T) {
	cases := []struct {
		name      string
		attacker  func(ps, hs []*Mob) (int32, []Player)
		wantEnded bool
	}{
		{"hoglin", func(_, hs []*Mob) (int32, []Player) { return hs[0].ID, nil }, true},
		{"piglin", func(ps, _ []*Mob) (int32, []Player) { return ps[1].ID, nil }, false},
		{"creative player", func(_, _ []*Mob) (int32, []Player) {
			return 60, []Player{{EntityID: 60, X: -3.5, Y: 64, Z: -1.5}}
		}, true},
	}
	for _, c := range cases {
		w, ps, hs := huntWorld(t, 2, 1)
		m := ps[0]
		m.celebrateAt(0, 64, 0, false)
		id, players := c.attacker(ps, hs)
		m.retaliate = id
		m.tick(w, players, all(ps, hs))
		if ended := m.barter.celebrate == 0; ended != c.wantEnded {
			t.Errorf("blow from a %s: celebration ended = %v, want %v", c.name, ended, c.wantEnded)
		}
	}
}
