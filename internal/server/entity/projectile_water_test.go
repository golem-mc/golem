package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// vanilla: FireworkRocketEntity.tick, EyeOfEnder.tick call baseTick first.
func TestRocketAndEyeSplashBeforeMovingEvenOnTheirLastTick(t *testing.T) {
	w := newTestWorld()
	w.set(0, 64, 0, registry.BlockWater.DefaultState())
	t.Run("rocket", func(t *testing.T) {
		f := NewFirework(1, 0.5, 66, 0.5, 0, 0, 0, 0, 3)
		f.ShotAtAngle = true
		f.VX, f.VY, f.VZ = 0, -1, 0
		for range 2 {
			if events := f.Step(w, nil, nil, nil); len(events) != 0 {
				t.Fatalf("splash before baseTick reached water: %v", events)
			}
		}
		f.Life = f.Lifetime
		events := f.Step(w, nil, nil, nil)
		if len(events) != 2 || events[0].Kind != FireworkSplashed || events[1].Kind != FireworkBurst {
			t.Fatalf("last tick events = %v, want splash then burst", events)
		}
		if ev := events[0]; ev.X != 0.5 || ev.Y != 64 || ev.Z != 0.5 || math.Abs(float64(ev.Volume)-0.2) > 1e-6 {
			t.Errorf("splash = %+v, want at (0.5,64,0.5) with volume 0.2", ev)
		}
		if events := f.Step(w, nil, nil, nil); len(events) != 0 {
			t.Errorf("dead rocket emitted %v", events)
		}
	})
	t.Run("eye", func(t *testing.T) {
		e := NewEyeOfEnder(1, 0.5, 66, 0.5)
		e.VY = -1
		for range 2 {
			if ended, splash := e.Step(w); ended || splash != nil {
				t.Fatalf("ended=%v splash=%v before baseTick reached water", ended, splash)
			}
		}
		e.life = eyeLifeTicks
		ended, splash := e.Step(w)
		if !ended || splash == nil {
			t.Fatalf("last tick ended=%v splash=%v, want both", ended, splash)
		}
		if splash.X != 0.5 || splash.Y != 64 || splash.Z != 0.5 || math.Abs(float64(splash.Volume)-0.2) > 1e-6 {
			t.Errorf("splash = %+v, want at (0.5,64,0.5) with volume 0.2", splash)
		}
		if ended, splash := e.Step(w); ended || splash != nil {
			t.Errorf("dead eye ended=%v splash=%v", ended, splash)
		}
	})
}
