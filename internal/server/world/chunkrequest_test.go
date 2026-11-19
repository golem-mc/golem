package world

import (
	"reflect"
	"sync"
	"testing"
	"time"
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

func TestChunkRequestCancelRunningAndReady(t *testing.T) {
	w, unpark := parkedChunkRequests(t)
	w.SetDropUntouchedChunks(false)
	gate, started := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	w.SetChunkGenerationGate(func(ChunkPos) { close(started); <-gate })
	pos := ChunkPos{}
	running := w.RequestChunk(pos, 0)
	unpark()
	<-started
	if running.CancelQueued() {
		t.Fatal("queued-only cancellation discarded running work")
	}
	var saves sync.WaitGroup
	running.CancelWait(&saves)
	waited := make(chan struct{})
	go func() { saves.Wait(); close(waited) }()
	select {
	case <-waited:
		t.Fatal("disconnect finished before its running acquisition")
	case <-time.After(10 * time.Millisecond):
	}
	release()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled acquisition did not finish its tracked save")
	}
	w.FlushLighting()
	if w.ChunkLoaded(pos) {
		t.Fatal("cancelled running chunk is still held")
	}
	// A completed request and a second interested player each own a hold.
	w.SetChunkGenerationGate(nil)
	ready := w.RequestChunk(pos, 0)
	w.FlushLighting()
	other := w.RequestChunk(pos, 0)
	ready.Cancel()
	if !w.ChunkLoaded(pos) {
		t.Fatal("cancellation released another player's hold")
	}
	if c := <-other.Ready(); c == nil {
		t.Fatal("other player lost its chunk")
	}
	w.Release(pos)
	if w.ChunkLoaded(pos) {
		t.Fatal("completed request leaked a hold")
	}
}

func TestChunkRequestCancelKeepsQueuedInterest(t *testing.T) {
	w, unpark := parkedChunkRequests(t)
	var generated int
	w.SetChunkGenerationGate(func(ChunkPos) { generated++ })
	pos := ChunkPos{X: 10}
	departing := w.RequestChunk(pos, 1)
	remaining := w.RequestChunk(pos, 2)
	departing.Cancel()
	unpark()
	w.FlushLighting()
	if generated != 1 {
		t.Fatalf("generated %d times, want 1 for the remaining player", generated)
	}
	if c := <-remaining.Ready(); c == nil {
		t.Fatal("remaining player lost its acquisition")
	}
	w.Release(pos)
	if w.ChunkLoaded(pos) {
		t.Fatal("departing player leaked a hold")
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

// TestChunkRequestCancelAfterAPanickedGeneration cancels a request whose
// generation panicked: the disconnect's wait must not hang on a worker that
// will never answer.
func TestChunkRequestCancelAfterAPanickedGeneration(t *testing.T) {
	pos := ChunkPos{X: 3, Z: -2}
	w, err := NewWorld(t.TempDir(), panicGenerator{NewFlatGenerator(), pos})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	r := w.RequestChunk(pos, 0)
	w.exec.waitChunk()
	var saves sync.WaitGroup
	r.CancelWait(&saves)
	waited := make(chan struct{})
	go func() { saves.Wait(); close(waited) }()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelling a panicked generation blocked its waiter")
	}
}
