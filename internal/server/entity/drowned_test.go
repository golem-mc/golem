package entity

import "testing"

func TestDrownedBeachSearch(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		bright, dry, blocked bool
		y                    float64
		want                 bool
	}{
		{name: "night", y: 62, want: true},
		{name: "day", y: 62, bright: true},
		{name: "deep", y: 59},
		{name: "dry", y: 62, dry: true},
		{name: "blocked", y: 62, blocked: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			w.bright = tc.bright
			sand := defaultState(t, "minecraft:sand")
			water := defaultState(t, "minecraft:water")
			for x := int32(0); x <= 3; x++ {
				w.set(x, 60, 0, sand)
				if x < 3 {
					w.set(x, 61, 0, water)
					w.set(x, 62, 0, water)
				}
			}
			w.set(3, 61, 0, sand)
			if tc.blocked {
				w.set(3, 63, 0, water)
			}
			m := NewMob(TypeDrowned, 1, 0.5, tc.y, 0.5, 1)
			if !tc.dry {
				m.waterFluid.height = 1
			}
			g := newDrownedBeachGoal(m)
			tv := &tickView{w: w}
			if got := g.CanStart(tv); got != tc.want {
				t.Fatalf("can start = %v, want %v", got, tc.want)
			}
			if tc.want {
				if g.target != (Waypoint{3, 61, 0}) {
					t.Fatalf("target = %v", g.target)
				}
				m.searchingForLand = true
				g.Start(tv)
				if m.searchingForLand || m.navDone() {
					t.Error("beach start did not clear swimming search and start walking")
				}
				w.set(3, 62, 0, defaultState(t, "minecraft:water"))
				if g.ShouldContinue(tv) {
					t.Error("beach goal continued with water above the target")
				}
			}
		})
	}
}
