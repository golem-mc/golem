package entity

import (
	"math"
	"testing"
)

// The allay accelerates by the control's current speed, not its base speed.
func TestAllayFlyInputAcceleratesByTheControlSpeed(t *testing.T) {
	speed := kinds[TypeAllay].speed
	for _, factor := range []float64{1, 2.25} {
		m := NewMob(TypeAllay, 1, 0, 64, 0, 1)
		m.Yaw = 0
		m.flyInput(factor, 0)
		want := factor * speed * factor * speed
		if got := math.Abs(m.VZ); math.Abs(got-want) > 1e-12 {
			t.Fatalf("factor %v: acceleration %v, want %v", factor, got, want)
		}
	}
}
