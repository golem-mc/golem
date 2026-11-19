package rules

import (
	"math"
	"testing"
)

func TestMaxFlyingTicks(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		gravity float64
		want    int
	}{
		{"default", 0.08, 80},
		{"half", 0.04, 160},
		{"double", 0.16, 80},
		{"rounded up", 0.03, 214},
		{"zero", 0, math.MaxInt},
		{"negative", -0.1, math.MaxInt},
		{"below threshold", math.Nextafter(float64(float32(1e-5)), 0), math.MaxInt},
		{"threshold", float64(float32(1e-5)), 640001},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := MaxFlyingTicks(tt.gravity); got != tt.want {
				t.Fatalf("MaxFlyingTicks(%g) = %d, want %d", tt.gravity, got, tt.want)
			}
		})
	}
}

func hovering() FloatingStep { return FloatingStep{NoBlocksAround: true} }

func ticksToKick(t *testing.T, step FloatingStep, exempt bool) int {
	t.Helper()
	var f FlightCheck
	f.Moved(step)
	for n := 1; n <= 200; n++ {
		if f.Tick(exempt, MaxFlyingTicks(0.08)) {
			return n
		}
	}
	return 0
}

func TestFlightCheckKicksOnTheEightyFirstTick(t *testing.T) {
	t.Parallel()
	if got := ticksToKick(t, hovering(), false); got != 81 {
		t.Fatalf("kicked on tick %d, want 81", got)
	}
}

func TestFlightCheckExemptions(t *testing.T) {
	t.Parallel()
	for name, step := range map[string]FloatingStep{
		"falling":      {DY: -0.04, NoBlocksAround: true},
		"spectator":    {Spectator: true, NoBlocksAround: true},
		"allow-flight": {AllowFlight: true, NoBlocksAround: true},
		"may fly":      {MayFly: true, NoBlocksAround: true},
		"levitation":   {Levitation: true, NoBlocksAround: true},
		"gliding":      {FallFlying: true, NoBlocksAround: true},
		"block near":   {},
	} {
		if got := ticksToKick(t, step, false); got != 0 {
			t.Errorf("%s: kicked on tick %d", name, got)
		}
	}
	if got := ticksToKick(t, hovering(), true); got != 0 {
		t.Errorf("sleeper, rider or dead body kicked on tick %d", got)
	}
}

func TestFlightCheckResetRestartsTheCount(t *testing.T) {
	t.Parallel()
	var f FlightCheck
	f.Moved(hovering())
	for range 60 {
		f.Tick(false, MaxFlyingTicks(0.08))
	}
	f.Reset()
	for range 60 {
		if f.Tick(false, MaxFlyingTicks(0.08)) {
			t.Fatal("kicked 60 ticks after a reset")
		}
	}
}

func TestASpinningBodyIsNoFloatingCandidate(t *testing.T) {
	if (FloatingStep{DY: 0.5}).Candidate() != true {
		t.Fatal("a plain rising step should be a candidate")
	}
	if (FloatingStep{DY: 0.5, SpinAttack: true}).Candidate() {
		t.Error("a spinning body is exempt from the floating check")
	}
}
