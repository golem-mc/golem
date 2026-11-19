package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestAngerEndTimeSaves: a kind whose end time is a plain long field saves 0
// until anger starts or stops; wolves and bees start at -1.
func TestAngerEndTimeSaves(t *testing.T) {
	t.Parallel()
	now := func() int64 { return 1000 }
	saved := func(m *Mob) nbt.Compound {
		tag := nbt.Compound{}
		m.SaveTo(tag, now)
		return tag
	}
	calmed := NewMob(TypePolarBear, 1, 0.5, 64, 0.5, 1)
	calmed.startAngerTimer()
	calmed.stopBeingAngry()
	reloaded := NewMob(TypePolarBear, 2, 0.5, 64, 0.5, 2)
	reloaded.stopBeingAngry()
	reloaded.LoadFrom(nbt.Compound{"anger_end_time": nbt.Long(0)}, nil, now)
	cases := []struct {
		name string
		m    *Mob
		want int64
	}{
		{"fresh polar bear", NewMob(TypePolarBear, 3, 0.5, 64, 0.5, 3), 0},
		{"fresh iron golem", NewMob(TypeIronGolem, 4, 0.5, 64, 0.5, 4), 0},
		{"fresh wolf", NewMob(TypeWolf, 5, 0.5, 64, 0.5, 5), -1},
		{"angered and calmed polar bear", calmed, -1},
		{"polar bear reloaded from 0", reloaded, 0},
	}
	for _, c := range cases {
		if got := saved(c.m)["anger_end_time"]; got != nbt.Long(c.want) {
			t.Errorf("%s: anger_end_time %v, want %d", c.name, got, c.want)
		}
	}
	reloaded.LoadFrom(nbt.Compound{"anger_end_time": nbt.Long(-1)}, nil, now)
	if got := saved(reloaded)["anger_end_time"]; got != nbt.Long(-1) {
		t.Errorf("polar bear reloaded from -1: anger_end_time %v, want -1", got)
	}
}
