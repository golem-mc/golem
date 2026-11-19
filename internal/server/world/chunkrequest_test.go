package world

import (
	"reflect"
	"sync"
	"testing"
)

// Park the only runner so cancellation and reprioritisation happen before
// dequeue, independently of the test machine's scheduling and CPU count.
func parkedChunkRequests(t *testing.T) (*World, func()) {
	t.Helper()
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	w.SetDropUntouchedChunks(true)
	w.exec = &executors{chunkSem: make(chan struct{}, 1), ioSem: make(chan struct{}, 1)}
	gate, started := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unpark := func() { once.Do(func() { close(gate) }) }
	w.exec.submitChunk(func() { close(started); <-gate })
	<-started
	t.Cleanup(func() { unpark(); w.Close() })
	return w, unpark
}

func TestChunkRequestCancellationAndPriority(t *testing.T) {
	w, unpark := parkedChunkRequests(t)
	var generated []ChunkPos
	w.SetChunkGenerationGate(func(pos ChunkPos) { generated = append(generated, pos) })
	stale := w.RequestChunk(ChunkPos{X: 100}, 0)
	far := w.RequestChunk(ChunkPos{X: 8}, 8)
	moving := w.RequestChunk(ChunkPos{X: 4}, 4)
	near := w.RequestChunk(ChunkPos{X: 1}, 1)
	stale.Cancel()
	stale.Cancel() // Cancelling twice must not decrement the pool twice.
	moving.SetPriority(0)
	unpark()
	w.FlushLighting()
	want := []ChunkPos{{X: 4}, {X: 1}, {X: 8}}
	if !reflect.DeepEqual(generated, want) {
		t.Fatalf("generated %v, want %v", generated, want)
	}
	for _, r := range []*ChunkRequest{far, moving, near} {
		if c := <-r.Ready(); c == nil {
			t.Fatal("nil chunk")
		}
		w.Release(r.pos)
	}
	if w.ChunkLoaded(stale.pos) {
		t.Fatal("cancelled queued chunk was loaded")
	}
}

// panicGenerator is a flat generator whose generation of one chunk panics.
type panicGenerator struct {
	*FlatGenerator
	at ChunkPos
}

func (g panicGenerator) Generate(c *Chunk) {
	if (ChunkPos{c.x, c.z}) == g.at {
		panic("generation failed")
	}
	g.FlatGenerator.Generate(c)
}
