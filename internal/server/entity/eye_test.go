package entity

import (
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
