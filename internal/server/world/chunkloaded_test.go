package world

import (
	"testing"
)

func TestChunkLoadedReleaseAndReload(t *testing.T) {
	for _, dirty := range []bool{false, true} {
		name := "Clean"
		if dirty {
			name = "Dirty"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			w := newTestWorld(t, NewFlatGenerator())
			w.SetDropUntouchedChunks(true)
			pos := ChunkPos{}
			w.Acquire(pos)
			if dirty {
				w.SetBlock(5, GroundLevel-1, 3, airState)
			}
			if !w.ChunkLoaded(pos) {
				t.Fatal("acquired chunk reported missing")
			}
			w.Release(pos)
			w.FlushSaves()
			if w.ChunkLoaded(pos) {
				t.Fatal("released chunk reported loaded after saves finished")
			}
			w.Acquire(pos)
			defer w.Release(pos)
			if !w.ChunkLoaded(pos) {
				t.Fatal("reloaded chunk reported missing")
			}
		})
	}
}
