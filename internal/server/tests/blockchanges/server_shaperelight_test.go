package blockchanges

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// A shape pass that reshapes several cells of one chunk books one relight
// box, and when the reshaped fence arms never mark light it books none.
// vanilla: ChunkHolder.broadcastChanges
func TestAShapePassBooksOneRelightPerChunkAndNoneWhenLightIsUnmarked(t *testing.T) {
	t.Parallel()
	srv, _ := tellRig(t)
	fence := servertest.BlockState(t, "minecraft:oak_fence")
	stone := servertest.BlockState(t, "minecraft:stone")
	air := servertest.BlockState(t, "minecraft:air")

	// Fences placed raw leave every arm unset, so the pass at the middle one
	// rewrites it and both neighbours: several changes, one chunk.
	for x := int32(2); x <= 4; x++ {
		srv.SetBlock(x, 70, 2, fence)
	}
	srv.SetBlockAndTell(8, 70, 8, stone)
	srv.SetBlockAndTell(8, 70, 8, air)
	srv.WaitRelights()

	var booked, changed int
	srv.RecordRelights(func(n int, changes []world.BlockChange) {
		booked += n
		changed += len(changes)
	})
	before := srv.RelightsRun()
	srv.ReconnectShapesAt(3, 70, 2)
	srv.WaitRelights()
	if changed < 2 {
		t.Fatalf("the pass made %d changes, want several in one chunk", changed)
	}
	if booked != 1 {
		t.Errorf("the pass booked %d relight boxes over %d changes, want 1", booked, changed)
	}
	if got := srv.RelightsRun() - before; got != 0 {
		t.Errorf("a fence-arm pass ran %d relights, want 0 as it marks no light", got)
	}

	// The skip is only for an unmarked box: a removal that changes light
	// still relights.
	srv.SetBlockAndTell(5, 70, 5, stone)
	srv.WaitRelights()
	before = srv.RelightsRun()
	srv.SetBlockAndTell(5, 70, 5, air)
	srv.WaitRelights()
	if got := srv.RelightsRun() - before; got < 1 {
		t.Errorf("removing a block ran %d relights, want at least 1", got)
	}
}
