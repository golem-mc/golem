package player

import (
	"sync"
	"testing"
)

func TestKnownMovementExpiresAfterAnIdleClientTick(t *testing.T) {
	t.Parallel()
	p := &Player{}
	want := Velocity{X: 1, Y: -2, Z: 3}
	p.SetVelocity(want)
	p.SetKnownMovement(want)
	p.EndMovementTick()
	if got := p.KnownMovement(); got != want {
		t.Fatalf("movement after its tick = %+v, want %+v", got, want)
	}
	p.EndMovementTick()
	if got := p.KnownMovement(); got != (Velocity{}) {
		t.Fatalf("movement after an idle tick = %+v, want zero", got)
	}
	if got := p.Velocity(); got != want {
		t.Fatalf("server velocity after an idle tick = %+v, want %+v", got, want)
	}
}

func TestKnownMovementSnapshotsAreCoherent(t *testing.T) {
	t.Parallel()
	p := &Player{}
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 10000 {
			v := float64(i)
			p.SetKnownMovement(Velocity{X: v, Y: v, Z: v})
			p.EndMovementTick()
			p.EndMovementTick()
		}
	})
	for range 10000 {
		if v := p.KnownMovement(); v.X != v.Y || v.Y != v.Z {
			t.Errorf("torn movement snapshot: %+v", v)
			break
		}
	}
	wg.Wait()
}
