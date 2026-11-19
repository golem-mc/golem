package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TestOrderedByNearest checks Direction.orderedByNearest at the exact
// quadrants, where Mth's sine table decides the ties: the horizontals of a
// level look run facing, clockwise side, counter-clockwise side, behind.
func TestOrderedByNearest(t *testing.T) {
	cases := []struct {
		yaw  float32
		want [4]int
	}{
		{0, [4]int{dirSouth, dirWest, dirEast, dirNorth}},
		{90, [4]int{dirWest, dirNorth, dirSouth, dirEast}},
		{180, [4]int{dirNorth, dirEast, dirWest, dirSouth}},
		{270, [4]int{dirEast, dirSouth, dirNorth, dirWest}},
	}
	for _, c := range cases {
		for _, pitch := range []float32{-60, 0, 45, 90} {
			var horizontals []int
			for _, d := range orderedByNearest(c.yaw, pitch) {
				if d >= dirNorth {
					horizontals = append(horizontals, d)
				}
			}
			if [4]int(horizontals) != c.want {
				t.Errorf("yaw %v pitch %v: horizontals %v, want %v", c.yaw, pitch, horizontals, c.want)
			}
		}
	}
	if got, want := orderedByNearest(0, 0), [6]int{dirSouth, dirDown, dirWest, dirEast, dirUp, dirNorth}; got != want {
		t.Errorf("a level south look orders %v, want %v", got, want)
	}
}

func TestVinePlacementState(t *testing.T) {
	stone := registry.BlockStates["minecraft:stone"]
	eastVine, _ := registry.FindState("minecraft:vine", map[string]string{"east": "true"})

	// A vine against a wall the click came through hangs on that wall.
	w := testWorld{}
	w.set(1, 0, 0, stone)
	ctx := PlacementContext{Face: protocol.FaceWest}
	if got, ok := VinePlacementState(w, ctx, 0, 0, 0); !ok || got != eastVine {
		t.Errorf("vine against an east wall = %d, %v; want %d", got, ok, eastVine)
	}

	// Leaves hold a vine by their collision cube, having no support shape.
	leafWall := testWorld{}
	leaves, _ := registry.FindState("minecraft:jungle_leaves", map[string]string{"persistent": "true"})
	leafWall.set(1, 0, 0, leaves)
	if got, ok := VinePlacementState(leafWall, ctx, 0, 0, 0); !ok || got != eastVine {
		t.Errorf("vine against leaves = %d, %v; want %d", got, ok, eastVine)
	}

	// A fresh vine with nothing to hang on is refused.
	if _, ok := VinePlacementState(testWorld{}, ctx, 0, 0, 0); ok {
		t.Error("a vine in the open was placed")
	}

	// A vine clicked with a vine and no other wall stays as it was.
	w.set(0, 0, 0, eastVine)
	clicked := PlacementContext{Face: protocol.FaceWest, ReplacingClickedOn: true}
	if got, ok := VinePlacementState(w, clicked, 0, 0, 0); !ok || got != eastVine {
		t.Errorf("vine on a vine with no free wall = %d, %v; want it unchanged", got, ok)
	}

	// With a second wall it gains that face.
	w.set(0, 0, -1, stone)
	both, _ := registry.FindState("minecraft:vine", map[string]string{"east": "true", "north": "true"})
	if got, ok := VinePlacementState(w, clicked, 0, 0, 0); !ok || got != both {
		t.Errorf("vine on a vine beside a north wall = %d, %v; want %d", got, ok, both)
	}
}
