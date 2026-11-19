package entity

import "testing"

// TestTurtleEggBreakers pins who may break an egg: nobody of the turtle or
// bat kinds, and by landing no zombie either. vanilla: TurtleEggBlock.canDestroyEgg
func TestTurtleEggBreakers(t *testing.T) {
	for _, tc := range []struct {
		kind          MobType
		step, landing bool
	}{
		{TypeCow, true, true},
		{TypeTurtle, false, false},
		{typeBat, false, false},
		{TypeZombie, true, false},
		{TypeHusk, true, false},
		{TypeDrowned, true, false},
		{TypeZombieVillager, true, false},
		{TypeZombifiedPiglin, true, false},
	} {
		if got := BreaksTurtleEggsStepping(tc.kind); got != tc.step {
			t.Errorf("%s stepping = %v, want %v", tc.kind, got, tc.step)
		}
		if got := BreaksTurtleEggsLanding(tc.kind); got != tc.landing {
			t.Errorf("%s landing = %v, want %v", tc.kind, got, tc.landing)
		}
	}
}
