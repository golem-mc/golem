package world

import (
	"sync"
	"testing"
	"time"
)

func TestChunkLoadedNearCacheTakesNoLock(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	pos := ChunkPos{}
	w.Acquire(pos)
	defer w.Release(pos)

	w.mu.Lock()
	got := make(chan bool)
	go func() { got <- w.ChunkLoaded(pos) }()
	select {
	case loaded := <-got:
		w.mu.Unlock()
		if !loaded {
			t.Error("cached chunk reported missing")
		}
	case <-time.After(5 * time.Second):
		w.mu.Unlock()
		<-got
		t.Fatal("a cached existence check waited for the write lock")
	}
}

func TestChunkLoadedCacheCollisions(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	first, second, missing := ChunkPos{}, ChunkPos{X: nearSlots}, ChunkPos{X: -nearSlots}
	w.Acquire(first)
	defer w.Release(first)
	w.Acquire(second)
	defer w.Release(second)

	for _, cached := range []ChunkPos{first, second} {
		w.BlockAt(cached.X*16, 64, cached.Z*16)
		if w.nearAt(cached) == nil {
			t.Fatalf("chunk %v is not in the cache", cached)
		}
		for _, tc := range []struct {
			pos    ChunkPos
			loaded bool
		}{{first, true}, {second, true}, {missing, false}} {
			if nearIndex(tc.pos) != nearIndex(cached) {
				t.Fatalf("%v and %v do not share a cache slot", tc.pos, cached)
			}
			if got := w.ChunkLoaded(tc.pos); got != tc.loaded {
				t.Errorf("ChunkLoaded(%v) with %v cached = %v, want %v", tc.pos, cached, got, tc.loaded)
			}
			if got := w.ChunkLoadedAt(tc.pos.X*16+15, tc.pos.Z*16+15); got != tc.loaded {
				t.Errorf("ChunkLoadedAt in %v with %v cached = %v, want %v", tc.pos, cached, got, tc.loaded)
			}
		}
	}
	if got := w.LoadedChunks(); got != 2 {
		t.Errorf("existence checks left %d loaded chunks, want 2", got)
	}
}

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

func TestChunkLoadedConcurrentAcquireRelease(t *testing.T) {
	t.Parallel()
	w := newTestWorld(t, NewFlatGenerator())
	w.SetDropUntouchedChunks(true)
	first, second := ChunkPos{}, ChunkPos{X: nearSlots}
	w.Acquire(first)
	defer w.Release(first)

	done := make(chan struct{})
	var ready, readers sync.WaitGroup
	for range 3 {
		ready.Add(1)
		readers.Go(func() {
			ready.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				// The second chunk may enter or leave during either call.
				w.ChunkLoaded(first)
				w.ChunkLoaded(second)
			}
		})
	}
	defer func() {
		close(done)
		readers.Wait()
	}()
	ready.Wait()
	for range 100 {
		w.Acquire(second)
		if !w.ChunkLoaded(second) || !w.ChunkLoaded(first) {
			t.Fatal("acquired chunk reported missing")
		}
		w.Release(second)
		w.FlushSaves()
		// Only this goroutine loads/unloads; the completed release and save
		// are the barrier after which the answer must be false.
		if w.ChunkLoaded(second) {
			t.Fatal("released chunk reported loaded after saves finished")
		}
		if !w.ChunkLoaded(first) {
			t.Fatal("releasing a colliding chunk unloaded the held chunk")
		}
	}
}
