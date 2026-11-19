package entity

import (
	"math"
	"testing"
)

func TestWaterSplashRiddenUsesPassengerVelocity(t *testing.T) {
	cases := []struct{ vx, vy, vz float64 }{
		{0, 0, 0},
		{0.1, -0.2, 0.05},
		{3, -3, 3},
	}
	for _, c := range cases {
		s := &WaterSplash{Volume: 0.25, Pitch: 1.1}
		s.Ridden(c.vx, c.vy, c.vz)
		want := float32(min(1, math.Sqrt(c.vx*c.vx*0.2+c.vy*c.vy+c.vz*c.vz*0.2)*0.9))
		if s.Volume != want {
			t.Errorf("Ridden(%v,%v,%v) volume = %v, want %v", c.vx, c.vy, c.vz, s.Volume, want)
		}
		if s.Pitch != 1.1 {
			t.Errorf("Ridden changed pitch to %v", s.Pitch)
		}
	}
	s := &WaterSplash{}
	s.Ridden(3, -3, 3)
	if s.Volume != 1 {
		t.Errorf("fast ridden volume = %v, want clamp to 1", s.Volume)
	}
}
