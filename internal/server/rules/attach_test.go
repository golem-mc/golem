package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// newAttachWorld is a flat world with the chunks around the origin loaded,
// the SurvivalReader the placement walks ask.
func newAttachWorld(t testing.TB) *world.World {
	t.Helper()
	w, err := world.NewWorld(t.TempDir(), world.NewFlatGenerator())
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	for cx := int32(-1); cx <= 0; cx++ {
		for cz := int32(-1); cz <= 0; cz++ {
			w.Acquire(world.ChunkPos{X: cx, Z: cz})
		}
	}
	return w
}

// TestLadderTakesTheClickedWall: with two walls to lean on, the ladder
// placed beside a clicked block takes that block, the clicked face's
// opposite leading BlockPlaceContext.getNearestLookingDirections.
func TestLadderTakesTheClickedWall(t *testing.T) {
	w := newAttachWorld(t)
	stone := registry.BlockStates["minecraft:stone"]
	w.SetBlock(-1, 0, 0, stone)
	w.SetBlock(0, 0, -1, stone)
	got, ok := AttachPlacementState(w, registry.BlockLadder, PlacementContext{Face: protocol.FaceEast, Yaw: 180}, 0, 0, 0)
	want, _ := registry.FindState("minecraft:ladder", map[string]string{"facing": "east"})
	if !ok || got != want {
		_, props, _ := registry.BlockStateName(got)
		t.Errorf("ladder = %s, %v; want facing=east", props, ok)
	}
}
