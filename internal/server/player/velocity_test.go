package player

import "testing"

func TestKnockKeepsHalfTheOldMotionAndCapsTheLift(t *testing.T) {
	t.Parallel()
	p := &Player{}
	p.SetVelocity(Velocity{X: 0.4, Y: 0.3, Z: -0.2})
	got := p.Knock(-0.1, 0.4, 0.3, true)
	want := Velocity{X: 0.1, Y: 0.4, Z: 0.2}
	if !near(got, want) || !near(p.Velocity(), want) {
		t.Errorf("grounded knock = %+v, stored %+v, want %+v", got, p.Velocity(), want)
	}
}

func TestStepVelocityDragsAndFalls(t *testing.T) {
	t.Parallel()
	p := &Player{}
	p.SetVelocity(Velocity{X: 0.4, Y: 0.4, Z: 0})
	p.StepVelocity(false)
	if want := (Velocity{X: 0.364, Y: 0.3136}); !near(p.Velocity(), want) {
		t.Errorf("air step = %+v, want %+v", p.Velocity(), want)
	}
	p.StepVelocity(true)
	if want := (Velocity{X: 0.364 * 0.546, Y: (0.3136 - 0.08) * 0.98}); !near(p.Velocity(), want) {
		t.Errorf("ground step = %+v, want %+v", p.Velocity(), want)
	}
}

func near(a, b Velocity) bool {
	d := func(x, y float64) bool { return x-y < 1e-9 && y-x < 1e-9 }
	return d(a.X, b.X) && d(a.Y, b.Y) && d(a.Z, b.Z)
}
