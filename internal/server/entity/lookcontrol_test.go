package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

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
