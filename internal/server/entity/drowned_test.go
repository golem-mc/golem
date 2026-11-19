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

func TestDrownedWalksOutOfWaterAtNight(t *testing.T) {
	w := newTestWorld()
	sand := defaultState(t, "minecraft:sand")
	water := defaultState(t, "minecraft:water")
	floor(w, -4, -3, 6, 3, 60, sand)
	for x := int32(-4); x <= 6; x++ {
		for z := int32(-3); z <= 3; z++ {
			if x < 2 {
				w.set(x, 61, z, water)
			} else {
				w.set(x, 61, z, sand)
			}
		}
	}
	m := NewMob(TypeDrowned, 1, 0.5, 61, 0.5, 1)
	m.Persistence = true
	for range 200 {
		m.tick(w, nil, []*Mob{m})
		if m.X >= 2 && m.Y >= 62 {
			return
		}
	}
	t.Fatalf("drowned stayed at (%v, %v, %v), want on the beach", m.X, m.Y, m.Z)
}

func TestDrownedStrollsUnderwaterByDay(t *testing.T) {
	w := newTestWorld()
	w.bright = true
	sand := defaultState(t, "minecraft:sand")
	water := defaultState(t, "minecraft:water")
	floor(w, -12, -12, 12, 12, 50, sand)
	for x := int32(-12); x <= 12; x++ {
		for z := int32(-12); z <= 12; z++ {
			for y := int32(51); y <= 66; y++ {
				w.set(x, y, z, water)
			}
		}
	}
	m := NewMob(TypeDrowned, 1, 0.5, 51, 0.5, 1)
	m.waterFluid.height = 1
	for _, goal := range drownedGoals(m) {
		g, ok := goal.(*randomStroll)
		if !ok {
			continue
		}
		for range 1000 {
			if !g.CanStart(&tickView{w: w}) {
				continue
			}
			if g.target.Y != 51 {
				t.Fatalf("stroll target = %v, want on the submerged floor", g.target)
			}
			g.Start(&tickView{w: w})
			if m.navDone() {
				t.Fatal("no path to the underwater stroll target")
			}
			return
		}
		t.Fatal("drowned never picked an underwater stroll")
	}
	t.Fatal("missing drowned stroll goal")
}
