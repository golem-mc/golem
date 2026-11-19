package entity

import "testing"

// vanilla: Creeper.causeFallDamage
func TestALandingCreeperWindsItsFuseByTheFall(t *testing.T) {
	for _, tc := range []struct {
		drop float64
		want func(m *Mob) int
	}{
		{12, func(*Mob) int { return 18 }},
		{30, func(m *Mob) int { return m.MaxSwell - 5 }},
	} {
		w := newTestWorld()
		floor(w, -16, -16, 16, 16, 63, defaultState(t, "minecraft:stone"))
		m := NewMob(TypeCreeper, 1, 0.5, 64+tc.drop, 0.5, 3)
		m.OnGround = false
		for range 200 {
			m.Tick(w, nil)
			if m.OnGround {
				break
			}
		}
		if !m.OnGround {
			t.Fatalf("drop %v: never landed", tc.drop)
		}
		if want := tc.want(m); m.swell != want {
			t.Fatalf("drop %v: swell=%d want %d (landed %v)", tc.drop, m.swell, want, m.LandedFall)
		}
	}
}
