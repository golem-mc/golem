package server

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

func TestMissingChunksRespectsThePendingHoldQueue(t *testing.T) {
	center := world.ChunkPos{X: 8}
	load := int32(2)
	// The old hold and a position in the new view wrap to the same bit.
	departed := world.ChunkPos{X: -2, Z: 2}
	newPos := world.ChunkPos{X: 6, Z: 2}
	promoted := world.ChunkPos{X: 7}
	first := world.ChunkPos{X: 10, Z: -2}
	last := world.ChunkPos{X: 8, Z: 2}
	c := client{
		center: center, radius: 1, load: load,
		loaded:       newChunkSet(center, load),
		pendingHolds: []world.ChunkPos{first, departed, promoted, last},
	}
	c.loaded.add(center)

	missing := c.missingChunks()
	for _, pos := range []world.ChunkPos{newPos, promoted} {
		if !slices.Contains(missing, pos) {
			t.Errorf("missing chunks must include %v", pos)
		}
	}
	for _, pos := range []world.ChunkPos{center, first, last} {
		if slices.Contains(missing, pos) {
			t.Errorf("missing chunks must exclude %v", pos)
		}
	}
	if want := []world.ChunkPos{first, departed, last}; !slices.Equal(c.pendingHolds, want) {
		t.Errorf("pending holds = %v, want %v", c.pendingHolds, want)
	}
}
