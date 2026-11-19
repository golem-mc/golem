package entity

import (
	"math"
	"math/rand"
	"testing"
)

func flyEye(e *EyeOfEnder, maxTicks int) (ticks int) {
	w := newTestWorld()
	for ticks < maxTicks {
		ticks++
		if ended, _ := e.Step(w); ended {
			return ticks
		}
	}
	return ticks
}

func TestAnEyeEndsItsFlightAfterEightyOneTicks(t *testing.T) {
	e := NewEyeOfEnder(1, 0, 70, 0)
	e.SignalTo(5, 70, 5, rand.New(rand.NewSource(1)))
	if got := flyEye(e, 200); got != 81 {
		t.Errorf("the flight ended on tick %d, want 81 (life > 80)", got)
	}
	if !e.Dead {
		t.Error("an ended eye is not dead")
	}
}

func TestAnEyeFartherThanTwelveBlocksAimsTwelveOutAndEightUp(t *testing.T) {
	e := NewEyeOfEnder(1, 0, 70, 0)
	e.SignalTo(100, 40, 0, rand.New(rand.NewSource(1)))
	w := newTestWorld()
	e.Step(w)
	e.Step(w)
	// Heading +x, rising toward y 78.
	if e.X <= 0 || e.VY <= 0 {
		t.Errorf("after two ticks the eye is at x=%v with vy=%v, want it moving along +x and rising", e.X, e.VY)
	}
	flyEye(e, 200)
	if e.X > 12.5 || e.Y < 70 {
		t.Errorf("the eye ended at (%v, %v), want it held within 12 blocks of the start", e.X, e.Y)
	}
}

func TestAnEyeOverItsTargetKeepsAFinitePosition(t *testing.T) {
	e := NewEyeOfEnder(1, 3, 70, 3)
	e.SignalTo(3, 70, 3, rand.New(rand.NewSource(1)))
	flyEye(e, 200)
	if math.IsNaN(e.X) || math.IsNaN(e.Z) {
		t.Errorf("the eye ended at (%v, %v)", e.X, e.Z)
	}
}

func TestOneInFiveEyesShatter(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	survived := 0
	const n = 5000
	for range n {
		e := NewEyeOfEnder(1, 0, 70, 0)
		e.SignalTo(5, 70, 5, rng)
		if e.Survives() {
			survived++
		}
	}
	if survived < n*77/100 || survived > n*83/100 {
		t.Errorf("%d of %d eyes survived, want about four in five", survived, n)
	}
}
