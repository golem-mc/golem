package rules

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// slabWorld is solid below y=0 and air above, with the floor block as given.
type slabWorld struct{ floor uint16 }

func (w slabWorld) BlockAt(_, y, _ int32) uint16 {
	switch {
	case y == -1:
		return w.floor
	case y < -1:
		return w.floor
	}
	return 0
}

func spreadState(t *testing.T, name string) uint16 {
	t.Helper()
	s, ok := registry.FindState(name, nil)
	if !ok {
		t.Fatalf("no block %s", name)
	}
	return s
}

func TestSpreadKeepsPointsApartInsideTheArea(t *testing.T) {
	area := SpreadArea{MinX: -50, MinZ: -50, MaxX: 50, MaxZ: 50, MaxHeight: 100, MinY: -64}
	w := slabWorld{floor: spreadState(t, "minecraft:stone")}
	points, _, ok := SpreadPositions(rand.New(rand.NewSource(1)), w, 6, 10, area)
	if !ok {
		t.Fatal("spread failed")
	}
	for i, p := range points {
		if p.X < area.MinX || p.X > area.MaxX || p.Z < area.MinZ || p.Z > area.MaxZ {
			t.Errorf("point %d %v outside the area", i, p)
		}
		for j := i + 1; j < len(points); j++ {
			if d := p.dist(points[j]); d < 10 {
				t.Errorf("points %d and %d are %.2f apart", i, j, d)
			}
		}
		if y := SpreadY(w, p, area); y != 0 {
			t.Errorf("point %d stands at y=%d, want 0", i, y)
		}
	}
}

func TestSpreadFailsWhenCrowdedOrWithoutASafeFloor(t *testing.T) {
	area := SpreadArea{MinX: -1, MinZ: -1, MaxX: 1, MaxZ: 1, MaxHeight: 100, MinY: -64}
	if _, _, ok := SpreadPositions(rand.New(rand.NewSource(1)), slabWorld{floor: spreadState(t, "minecraft:stone")}, 3, 100, area); ok {
		t.Error("three points 100 apart in a 2 block square spread")
	}
	area = SpreadArea{MinX: -50, MinZ: -50, MaxX: 50, MaxZ: 50, MaxHeight: 100, MinY: -64}
	for _, floor := range []string{"minecraft:water", "minecraft:lava", "minecraft:fire"} {
		if _, _, ok := SpreadPositions(rand.New(rand.NewSource(1)), slabWorld{floor: spreadState(t, floor)}, 1, 1, area); ok {
			t.Errorf("a point spread onto %s", floor)
		}
	}
}
