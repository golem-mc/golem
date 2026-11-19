package entity

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// slab raises a wall of stone across x = 5 from y = 60 to y = 70 over z in
// [z0, z1], so a flight round it has to go through the gap left.
func slab(t *testing.T, w testWorld, z0, z1 int32) {
	t.Helper()
	stone := defaultState(t, "minecraft:stone")
	for y := int32(60); y <= 70; y++ {
		for z := z0; z <= z1; z++ {
			w.set(5, y, z, stone)
		}
	}
}

func TestAFlightPathGoesThroughTheGapInAWall(t *testing.T) {
	w := newTestWorld()
	slab(t, w, -3, 3)
	w.set(5, 64, 0, 0)
	w.set(5, 65, 0, 0)
	path, reached := flyPathFind(w, &beeFlight, Waypoint{2, 64, 0}, Waypoint{8, 64, 0}, 1, beeFlight.pathLength*16)
	if !reached {
		t.Fatal("no flight reaches the far side of a wall with a gap")
	}
	through := false
	for _, c := range path {
		if c.X == 5 {
			through = c.Z == 0 && (c.Y == 64 || c.Y == 65)
			if !through {
				t.Fatalf("the path crosses the wall at %v, not in the gap", c)
			}
		}
	}
	if !through {
		t.Fatalf("the path %v never crosses the wall", path)
	}
}

func TestAFlightPathAvoidsWaterFireAndTheCellsOverAFence(t *testing.T) {
	w := newTestWorld()
	water := defaultState(t, "minecraft:water")
	fire := defaultState(t, "minecraft:fire")
	fence := defaultState(t, "minecraft:oak_fence")
	for _, c := range []struct {
		x, y, z int32
		state   uint16
		name    string
	}{{1, 64, 0, water, "water"}, {2, 64, 0, fire, "fire"}, {3, 63, 0, fence, "a fence below"}} {
		w.set(c.x, c.y, c.z, c.state)
		if c.name == "a fence below" {
			if _, ok := flyCellMalus(w, &beeFlight, c.x, c.y+1, c.z); ok {
				t.Errorf("a cell over a fence is a node")
			}
		} else if _, ok := flyCellMalus(w, &beeFlight, c.x, c.y, c.z); ok {
			t.Errorf("a cell of %s is a node", c.name)
		}
	}
	if malus, ok := flyCellMalus(w, &beeFlight, 1, 64, 1); !ok || malus != beeFlight.waterBorder {
		t.Errorf("a cell beside water costs %v (%v), want %v", malus, ok, beeFlight.waterBorder)
	}
	if malus, ok := flyCellMalus(w, &beeFlight, 6, 64, 0); !ok || malus != 0 {
		t.Errorf("open air costs %v (%v), want 0", malus, ok)
	}
}

func TestFlightCellPricesByKind(t *testing.T) {
	w := newTestWorld()
	water := defaultState(t, "minecraft:water")
	fire := defaultState(t, "minecraft:fire")
	cactus := defaultState(t, "minecraft:cactus")
	w.set(0, 64, 0, water)
	w.set(10, 64, 0, fire)
	w.set(20, 63, 0, cactus)
	for i, name := range []string{"minecraft:oak_stairs", "minecraft:oak_slab", "minecraft:oak_fence", "minecraft:mangrove_roots", "minecraft:mangrove_leaves"} {
		logged, ok := registry.WithBool(defaultState(t, name), registry.PropWaterlogged, true)
		if !ok {
			t.Fatalf("%s has no waterlogged state", name)
		}
		w.set(30+int32(i)*2, 64, 0, logged)
	}
	for _, c := range []struct {
		name    string
		f       *flyMaluses
		x, y, z int32
		malus   float64
		ok      bool
	}{
		{"an allay's water cell", &allayFlight, 0, 64, 0, 8, true},
		{"a bee's water cell", &beeFlight, 0, 64, 0, 0, false},
		{"a parrot's cell beside fire", &parrotFlight, 11, 64, 0, 0, false},
		{"a parrot's cell beside water", &parrotFlight, 1, 64, 0, 8, true},
		{"a bee's cell beside fire", &beeFlight, 11, 64, 0, 16, true},
		{"an allay's cell beside fire", &allayFlight, 11, 64, 0, 8, true},
		{"a bee's cell over a cactus", &beeFlight, 20, 64, 0, 0, false},
		{"a parrot's cell over a cactus", &parrotFlight, 20, 64, 0, 0, false},
		{"an allay's cell over a cactus", &allayFlight, 20, 64, 0, 0, false},
		{"an allay's cell beside a cactus", &allayFlight, 21, 63, 0, 8, true},
		{"an allay's cell in waterlogged stairs", &allayFlight, 30, 64, 0, 0, false},
		{"an allay's cell in a waterlogged slab", &allayFlight, 32, 64, 0, 0, false},
		{"a parrot's cell in a waterlogged fence", &parrotFlight, 34, 64, 0, 0, false},
		{"an allay's cell in waterlogged mangrove roots", &allayFlight, 36, 64, 0, 0, false},
		{"a parrot's cell in waterlogged mangrove leaves", &parrotFlight, 38, 64, 0, 0, false},
	} {
		if malus, ok := flyCellMalus(w, c.f, c.x, c.y, c.z); malus != c.malus || ok != c.ok {
			t.Errorf("%s is (%v, %v), want (%v, %v)", c.name, malus, ok, c.malus, c.ok)
		}
	}
}
