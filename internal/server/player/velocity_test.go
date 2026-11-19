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

func TestKnockKeepsTheOldLiftInTheAir(t *testing.T) {
	t.Parallel()
	p := &Player{}
	p.SetVelocity(Velocity{X: 0.2, Y: -0.3, Z: 0})
	got := p.Knock(0.1, 0.4, 0, false)
	if want := (Velocity{X: 0.2, Y: -0.3}); !near(got, want) {
		t.Errorf("airborne knock = %+v, want %+v", got, want)
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

func TestStepVelocityCutsSmallMotionBeforeTheDrag(t *testing.T) {
	t.Parallel()
	p := &Player{}
	p.SetVelocity(Velocity{X: 0.004, Z: 0.004})
	p.StepVelocity(true)
	if want := (Velocity{X: 0.004 * 0.546, Y: -0.0784, Z: 0.004 * 0.546}); !near(p.Velocity(), want) {
		t.Errorf("drag-first case = %+v, want %+v", p.Velocity(), want)
	}
	// Under 0.003 per axis, yet a squared length of 1.25E-5 over the cut.
	p.SetVelocity(Velocity{X: 0.0025, Z: 0.0025})
	p.StepVelocity(true)
	if want := (Velocity{X: 0.0025 * 0.546, Y: -0.0784, Z: 0.0025 * 0.546}); !near(p.Velocity(), want) {
		t.Errorf("combined-test case = %+v, want %+v", p.Velocity(), want)
	}
	p.SetVelocity(Velocity{X: 0.002, Z: 0.002, Y: 0.002})
	p.StepVelocity(true)
	if got := p.Velocity(); !near(got, Velocity{Y: -0.0784}) {
		t.Errorf("small motion left %+v, want only resting gravity", got)
	}
}

func near(a, b Velocity) bool {
	d := func(x, y float64) bool { return x-y < 1e-9 && y-x < 1e-9 }
	return d(a.X, b.X) && d(a.Y, b.Y) && d(a.Z, b.Z)
}

func TestStepVelocityKeepsRestingGravity(t *testing.T) {
	t.Parallel()
	for _, y := range []float64{0, -0.0784, -1} {
		p := &Player{}
		p.SetVelocity(Velocity{Y: y})
		for range 3 {
			p.StepVelocity(true)
			if got := p.Velocity(); !near(got, Velocity{Y: -0.0784}) {
				t.Fatalf("grounded start %v: velocity %+v, want resting -0.0784", y, got)
			}
		}
		p.Knock(0, 0.4, -0.4, true)
		p.StepVelocity(true)
		if got := p.Velocity(); !near(got, Velocity{Y: 0.275184, Z: -0.2184}) {
			t.Fatalf("stepped knockback = %+v, want lift 0.275184 and horizontal -0.2184", got)
		}
	}
}
