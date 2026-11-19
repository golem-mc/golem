package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// TestChestPlacementPairsWithASingleBeside places a chest at the origin,
// the player looking south so it faces north, beside one other chest.
func TestChestPlacementPairsWithASingleBeside(t *testing.T) {
	chest := func(name, facing, typ string) uint16 {
		state, ok := registry.FindState(name, map[string]string{"facing": facing, "type": typ})
		if !ok {
			t.Fatalf("no %s state facing %s type %s", name, facing, typ)
		}
		return state
	}
	east, west, south := [3]int32{1, 0, 0}, [3]int32{-1, 0, 0}, [3]int32{0, 0, 1}
	for _, c := range []struct {
		what       string
		at         [3]int32
		neighbour  uint16
		face       int32
		sneaking   bool
		wantFacing string
		wantType   string
	}{
		{"a single on the clockwise side", east, chest("minecraft:chest", "north", "single"), protocol.FaceTop, false, "north", "left"},
		{"a single on the counter-clockwise side", west, chest("minecraft:chest", "north", "single"), protocol.FaceTop, false, "north", "right"},
		{"a single facing another way", east, chest("minecraft:chest", "south", "single"), protocol.FaceTop, false, "north", "single"},
		{"a trapped chest", east, chest("minecraft:trapped_chest", "north", "single"), protocol.FaceTop, false, "north", "single"},
		{"a half already paired", east, chest("minecraft:chest", "north", "left"), protocol.FaceTop, false, "north", "single"},
		{"a single, sneaking on the ground", east, chest("minecraft:chest", "north", "single"), protocol.FaceTop, true, "north", "single"},
		{"sneaking against a west-facing single's front", east, chest("minecraft:chest", "west", "single"), protocol.FaceWest, true, "north", "single"},
		{"sneaking against a single's west side", east, chest("minecraft:chest", "north", "single"), protocol.FaceWest, true, "north", "left"},
		{"sneaking against a single's front", south, chest("minecraft:chest", "north", "single"), protocol.FaceNorth, true, "north", "single"},
		{"sneaking against an east-facing single's north side", south, chest("minecraft:chest", "east", "single"), protocol.FaceNorth, true, "east", "left"},
	} {
		w := testWorld{}
		w.set(c.at[0], c.at[1], c.at[2], c.neighbour)
		ctx := PlacementContext{Face: c.face, Sneaking: c.sneaking}
		state, ok := ChestPlacementState(w, registry.BlockChest, ctx, 0, 0, 0)
		if !ok {
			t.Fatalf("%s: no state", c.what)
		}
		_, raw, _ := registry.BlockStateName(state)
		props := registry.ParseStateProps(raw)
		if props["facing"] != c.wantFacing || props["type"] != c.wantType {
			t.Errorf("%s: placed %v, want facing %s type %s", c.what, props, c.wantFacing, c.wantType)
		}
	}
}
