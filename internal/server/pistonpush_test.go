package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

func TestPistonPopulatedMobsAndCushions(t *testing.T) {
	s, w := newTrackerBenchServer(t)
	w.SetBoolRule(world.RuleEntityDrops, false)
	dim := s.allDims()[0]

	s.setPistonGrid(dim, true)
	if bits := s.pistonPopulated.Load(); bits&(pistonMobs|pistonCushions) != 0 {
		t.Fatalf("expected mobs/cushions bits clear on empty server, got %#x", bits)
	}

	// Spawning a mob mid-step must set pistonMobs so pushByPiston visits it.
	s.spawnMobInDim(dim, entity.TypeCow, 1.5, 64, 0.5, 1, nil)
	if s.pistonPopulated.Load()&pistonMobs == 0 {
		t.Fatal("expected pistonMobs set after spawning mob")
	}
	pp := &world.PistonPush{
		X: 1, Y: 64, Z: 0, Dir: 5, Progress: 0.5, Extending: true,
		Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}},
	}
	s.pushByPiston(w, pp)
	s.mobsMu.Lock()
	movedX := dim.mobs.herd[0].X
	dim.mobs.setHerd(nil)
	s.mobsMu.Unlock()
	if movedX <= 1.5 {
		t.Fatalf("expected mob to be pushed past x=1.5, got x=%v", movedX)
	}

	s.setPistonGrid(dim, true)
	if s.pistonPopulated.Load()&pistonMobs != 0 {
		t.Fatal("expected pistonMobs cleared when herd is empty")
	}

	// Creating a cushion mid-step must set pistonCushions so pushByPiston breaks it.
	ent := fixtureOfTag(s, w, summonTag(nil, entity.FixtureCushion, 1.75, 64, 0.5))
	s.registerFixture(ent)
	if s.pistonPopulated.Load()&pistonCushions == 0 {
		t.Fatal("expected pistonCushions set after creating cushion")
	}
	s.pushByPiston(w, pp)
	s.fixturesMu.Lock()
	dead := ent.dead
	s.fixturesMu.Unlock()
	if !dead {
		t.Fatal("expected cushion to be broken by piston push")
	}

	// A push that finds no live cushion clears the bit.
	s.pushByPiston(w, pp)
	if s.pistonPopulated.Load()&pistonCushions != 0 {
		t.Fatal("expected pistonCushions cleared once no live cushions remain")
	}
}

func TestBodyPistonGridBounds(t *testing.T) {
	var g bodyPistonGrid[int]
	g.active = true
	list := []int{1, 2}
	boxes := map[int][6]float64{
		1: {10, 64, 10, 11, 65, 11},
		2: {11, 65, 11, 12, 66, 12},
	}
	g.build(list, func(int) bool { return true }, func(id int) [6]float64 { return boxes[id] })

	pushAt := func(x, y, z int32) *world.PistonPush {
		// Dir 5 (+X), Progress 0.5, Extending: origin at x-0.5, box [0,1]^3 -> swept slab [x+0.5, x+1.0].
		return &world.PistonPush{
			X: x, Y: y, Z: z, Dir: 5, Progress: 0.5, Extending: true,
			Boxes: []world.CollisionBox{{MaxX: 1, MaxY: 1, MaxZ: 1}},
		}
	}

	// Disjoint in X, Y, Z, and touching-only at X=10 (slab [9.5, 10.0]).
	for _, tc := range []struct {
		name    string
		x, y, z int32
	}{
		{"farX", 50, 64, 10},
		{"farYBelow", 10, 60, 10},
		{"farYAbove", 10, 70, 10},
		{"farZ", 10, 64, 50},
		{"touchingX", 9, 64, 10},
	} {
		if got := g.candidates(list, pushAt(tc.x, tc.y, tc.z)); len(got) != 0 {
			t.Errorf("%s: expected no candidates, got %v", tc.name, got)
		}
	}

	// Overlapping slab [10.5, 11.0] x [64, 65] x [10, 11] hits box 1.
	if got := g.candidates(list, pushAt(10, 64, 10)); len(got) == 0 {
		t.Fatal("expected candidates for overlapping push")
	}

	// Empty eligible set leaves bounds empty, so any push returns nil.
	g.build(list, func(int) bool { return false }, func(id int) [6]float64 { return boxes[id] })
	if got := g.candidates(list, pushAt(10, 64, 10)); got != nil {
		t.Fatalf("expected nil for empty eligible grid, got %v", got)
	}
}
