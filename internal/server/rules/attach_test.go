package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
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
