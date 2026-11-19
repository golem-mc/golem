package world

import (
	"runtime"
	"strings"
	"testing"
	"time"

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

// TestChunkEntitiesStampCurrent checks a stamp stops being current once a newer
// read of the chunk is taken, and only for that chunk.
func TestChunkEntitiesStampCurrent(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	a, b := ChunkPos{X: 0, Z: 0}, ChunkPos{X: 1, Z: 0}
	first, other := w.StampChunkEntities(a), w.StampChunkEntities(b)
	if !w.ChunkEntitiesStampCurrent(a, first) {
		t.Error("the only read of a chunk is not current")
	}
	w.StampChunkEntities(a)
	if w.ChunkEntitiesStampCurrent(a, first) {
		t.Error("a read is current after a newer one of the same chunk")
	}
	if !w.ChunkEntitiesStampCurrent(b, other) {
		t.Error("a read of another chunk lost its place to a's newer read")
	}
}

// TestSaveChunkEntitiesStampedSerialisesOneChunk checks a write of a chunk
// waits for the write already holding that chunk, while a write of another
// chunk does not, and that the bookkeeping is gone once the writes are.
func TestSaveChunkEntitiesStampedSerialisesOneChunk(t *testing.T) {
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	held, other := ChunkPos{X: 4, Z: 4}, ChunkPos{X: 5, Z: 4}
	e := &w.entityWrites
	cw := &chunkWrite{users: 1}
	cw.mu.Lock() // a write of held in progress
	e.mu.Lock()
	e.writing = map[ChunkPos]*chunkWrite{held: cw}
	e.mu.Unlock()

	// hang guards below are not waits: the writes are expected to finish.
	otherDone := make(chan error, 1)
	go func() {
		otherDone <- w.SaveChunkEntitiesStamped(other, w.StampChunkEntities(other), entityTag("minecraft:pig"))
	}()
	select {
	case err := <-otherDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second): // a hang guard, not a wait
		t.Fatal("a write of another chunk waited for the held chunk's write")
	}

	heldDone := make(chan error, 1)
	go func() {
		heldDone <- w.SaveChunkEntitiesStamped(held, w.StampChunkEntities(held), entityTag("minecraft:cow"))
	}()
	// Parked on the held lock, or, with no lock, already through.
	for !parkedInEntityWrite() {
		select {
		case err := <-heldDone:
			t.Fatalf("a write of the held chunk went through while another held it (err %v)", err)
		default:
			runtime.Gosched()
		}
	}
	select {
	case err := <-heldDone:
		t.Fatalf("a write of the held chunk went through while another held it (err %v)", err)
	default:
	}
	e.mu.Lock()
	cw.users-- // the held write ends
	e.mu.Unlock()
	cw.mu.Unlock()
	if err := <-heldDone; err != nil {
		t.Fatal(err)
	}
	if id := savedEntityID(t, w, held); id != "minecraft:cow" {
		t.Errorf("the file holds %q, want minecraft:cow", id)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.writing) != 0 || len(e.latest) != 0 {
		t.Errorf("bookkeeping left behind: %d writing, %d latest", len(e.writing), len(e.latest))
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
