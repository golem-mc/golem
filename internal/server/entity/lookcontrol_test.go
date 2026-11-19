package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestTheHeadStaysTurnedAfterALookEnds(t *testing.T) {
	m := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	step := func(look bool) {
		if look {
			m.lookAt(m.X-10, m.Y+m.eyeHeight(), m.Z, 10, 40) // yaw 90
		}
		m.lookControlTick(false)
		m.bodyRotationStep(m.X, m.Z)
	}
	for range 40 {
		step(true)
	}
	for range 40 {
		step(false)
	}
	if math.Abs(float64(m.HeadYaw)-90) > 1 {
		t.Fatalf("HeadYaw = %v, want 90", m.HeadYaw)
	}
}

func TestTheHeadIsHeldWithin75DegreesOfTheBodyOnAPath(t *testing.T) {
	m := aimless(NewMob(TypeCow, 1, 0.5, 64, 0.5, 1))
	m.nav.path = []Waypoint{{}}
	for range 60 {
		m.lookAt(m.X, m.Y+m.eyeHeight(), m.Z-10, 10, 40)
		m.lookControlTick(false)
		fromX, fromZ := m.X, m.Z
		m.Z += 0.1
		m.bodyRotationStep(fromX, fromZ)
		if d := math.Abs(wrapDegrees(float64(m.HeadYaw - m.Yaw))); d > 75.001 {
			t.Fatalf("head %v from body yaw %v", m.HeadYaw, m.Yaw)
		}
	}
}

func TestASwimmersPitchFollowsItsPathAndEasesBackAt5ATick(t *testing.T) {
	w := newTestWorld()
	water := registry.BlockWater.DefaultState()
	for y := int32(58); y < 76; y++ {
		floor(w, -4, -4, 4, 4, y, water)
	}
	m := aimless(NewMob(TypeDolphin, 1, 0.5, 60, 0.5, 1))
	m.updateMobFluids(w)
	path, end, ok := m.createSwimPath(w, 2, 72, 0, 1)
	if !ok {
		t.Fatal("no swim path up")
	}
	m.moveTo(path, end, 1)
	var most float32
	for i := 0; i < 12 && !m.navDone(); i++ {
		m.tickInto(nil, &tickView{}, w, nil, nil)
		most = min(most, m.Pitch)
	}
	if most > -5.001 {
		t.Fatalf("steepest climbing pitch %v, want beyond -5", most)
	}
	m.stopSeeking()
	if m.Pitch > -5.001 {
		t.Fatalf("pitch %v when the path ended, want beyond -5", m.Pitch)
	}
	for m.Pitch != 0 {
		before := m.Pitch
		m.lookControlTick(false)
		if d := m.Pitch - before; d > 5.0001 || d <= 0 {
			t.Fatalf("pitch went %v -> %v, want up by at most 5", before, m.Pitch)
		}
	}
}

func TestADolphinLooksAfterItsMoveControl(t *testing.T) {
	w := newTestWorld()
	for y := int32(58); y < 76; y++ {
		floor(w, -4, -4, 4, 4, y, registry.BlockWater.DefaultState())
	}
	m := aimless(NewMob(TypeDolphin, 1, 0.5, 60, 0.5, 1))
	end := Waypoint{X: 0, Y: 70, Z: 3}
	m.moveTo([]Waypoint{end}, end, 1)
	// The path turns the pitch up by 5; the level look target then brings it
	// to SmoothSwimmingLookControl's 10-degree offset.
	m.lookAt(m.X, m.Y+m.eyeHeight(), m.Z+10, 10, 40)
	m.Tick(w, nil)
	if m.navDone() {
		t.Fatal("the dolphin finished its path")
	}
	if m.Pitch != 10 {
		t.Fatalf("pitch = %v, want the look control's 10-degree offset", m.Pitch)
	}
	if m.HeadYaw != 1 {
		t.Fatalf("head yaw = %v, want 1 after the body clamps the look control's head turn", m.HeadYaw)
	}
}

func TestADolphinEasesItsPitchOnTheTickItsPathEnds(t *testing.T) {
	w := newTestWorld()
	floor(w, -1, -1, 1, 1, 60, registry.BlockWater.DefaultState())
	m := aimless(NewMob(TypeDolphin, 1, 0.5, 60, 0.5, 1))
	end := Waypoint{X: 0, Y: 60, Z: 0}
	m.moveTo([]Waypoint{end}, end, 1)
	m.Pitch = -30
	m.Tick(w, nil)
	if !m.navDone() {
		t.Fatal("the dolphin did not finish its path")
	}
	if m.Pitch != -25 {
		t.Fatalf("pitch = %v, want -25 on the tick navigation finishes", m.Pitch)
	}
}
