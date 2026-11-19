package world

import (
	"runtime"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func entityTag(id string) []nbt.Tag { return []nbt.Tag{nbt.Compound{"id": nbt.String(id)}} }

// savedEntityID is the id of the one entity pos's file holds.
func savedEntityID(t *testing.T, w *World, pos ChunkPos) string {
	t.Helper()
	got, _, err := w.LoadChunkEntities(pos)
	if err != nil || len(got) != 1 {
		t.Fatalf("entities = %v, %v; want one", got, err)
	}
	compound, ok := nbt.CompoundView(got[0])
	if !ok {
		t.Fatalf("entity = %T, want compound", got[0])
	}
	id, _ := nbt.Get[nbt.String](compound, "id")
	return string(id)
}

// TestSaveChunkEntitiesStampedDropsAnOlderRead checks a snapshot written after
// a newer read of the same chunk is skipped, so an unload's file is not
// overwritten by the auto-save's older entities.
func TestSaveChunkEntitiesStampedDropsAnOlderRead(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	pos := ChunkPos{X: 2, Z: 3}
	older, newer := w.StampChunkEntities(pos), w.StampChunkEntities(pos)

	if err := w.SaveChunkEntitiesStamped(pos, newer, entityTag("minecraft:cow")); err != nil {
		t.Fatal(err)
	}
	if err := w.SaveChunkEntitiesStamped(pos, older, entityTag("minecraft:pig")); err != nil {
		t.Fatal(err)
	}
	if id := savedEntityID(t, w, pos); id != "minecraft:cow" {
		t.Errorf("the file holds %q, want the newer read's minecraft:cow", id)
	}
}

// parkedInEntityWrite reports whether a goroutine is asleep on a mutex inside
// SaveChunkEntitiesStamped. The test holds cw.mu itself, so TryLock would
// fail whether or not the writer reached it; the goroutine's state says it
// did.
func parkedInEntityWrite() bool {
	buf := make([]byte, 1<<20)
	for len(buf) < 1<<26 {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			for g := range strings.SplitSeq(string(buf[:n]), "\n\n") {
				if strings.Contains(g, "[sync.Mutex.Lock") && strings.Contains(g, "SaveChunkEntitiesStamped") {
					return true
				}
			}
			return false
		}
		buf = make([]byte, 2*len(buf))
	}
	return false
}
