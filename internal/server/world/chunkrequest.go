package world

import "sync"

// AcquireSaved holds a loaded or saved chunk without generating missing or damaged terrain.
// The caller must Release a non-nil result.
func (w *World) AcquireSaved(pos ChunkPos) *Chunk {
	if c := w.holdLoaded(pos); c != nil {
		return c
	}
	c, err := w.loadChunk(pos)
	if err != nil || c == nil {
		return nil
	}
	if !c.lightCorrect {
		w.lightAlone(c)
	}
	c, _ = w.insertChunk(pos, c, w.primeMotion(c), false)
	return c
}

// ChunkRequest owns one asynchronous hold until Ready is received or Cancel
// gives it up. Receiving Ready transfers the hold to the caller, who must Release.
// Cancel must not be called after receiving Ready.
type ChunkRequest struct {
	world       *World
	pos         ChunkPos
	done        chan *Chunk
	task        *chunkTask
	mu          sync.Mutex
	cancelled   bool
	failed      bool // its task panicked, so no worker will answer it
	releaseDone *sync.WaitGroup
}

// RequestChunk queues generation at priority (lower runs first). The caller
// can change its priority or cancel without waiting for generation or lighting.
func (w *World) RequestChunk(pos ChunkPos, priority int) *ChunkRequest {
	if r := w.RequestLoadedChunk(pos); r != nil {
		return r
	}
	r := w.newChunkRequest(pos)
	r.task = w.exec.queueChunk(func() {
		r.mu.Lock()
		cancelled := r.cancelled
		doneSaving := r.releaseDone
		r.mu.Unlock()
		if cancelled {
			if doneSaving != nil {
				doneSaving.Done()
			}
			return
		}
		defer func() {
			if p := recover(); p != nil {
				// A canceller registered on releaseDone must not wait for a
				// result that will never come; r.done stays unanswered.
				r.mu.Lock()
				r.failed = true
				doneSaving := r.releaseDone
				r.releaseDone = nil
				r.mu.Unlock()
				if doneSaving != nil {
					doneSaving.Done()
				}
				panic(p)
			}
		}()
		done := make(chan *Chunk, 1)
		w.prepareChunk(pos, done)
		c := <-done
		r.mu.Lock()
		cancelled = r.cancelled
		if !cancelled {
			r.done <- c
		}
		doneSaving = r.releaseDone
		r.mu.Unlock()
		if cancelled {
			w.ReleaseWait(pos, doneSaving)
			if doneSaving != nil {
				doneSaving.Done()
			}
		}
	}, priority)
	return r
}

func (w *World) newChunkRequest(pos ChunkPos) *ChunkRequest {
	return &ChunkRequest{world: w, pos: pos, done: make(chan *Chunk, 1)}
}

// RequestLoadedChunk holds only already-loaded terrain, returning nil otherwise.
func (w *World) RequestLoadedChunk(pos ChunkPos) *ChunkRequest {
	c := w.HoldIfLoaded(pos)
	if c == nil {
		return nil
	}
	r := w.newChunkRequest(pos)
	r.done <- c
	return r
}

// Ready receives the acquired chunk once generation and lighting finish.
func (r *ChunkRequest) Ready() <-chan *Chunk { return r.done }

// SetPriority moves queued work to the caller's current distance from it.
func (r *ChunkRequest) SetPriority(priority int) {
	r.world.exec.prioritizeChunk(r.task, priority)
}

// Cancel removes queued work and releases a completed or running acquisition.
// Running generation finishes safely; its result is released by the worker.
func (r *ChunkRequest) Cancel() { r.CancelWait(nil) }

// CancelWait cancels like Cancel and registers a running acquisition and its
// save with done, so a disconnect can wait for its writes without waiting for
// unrelated players' generation.
func (r *ChunkRequest) CancelWait(done *sync.WaitGroup) {
	r.mu.Lock()
	if r.cancelled {
		r.mu.Unlock()
		return
	}
	r.cancelled = true
	ready := false
	select {
	case <-r.done:
		ready = true
	default:
	}
	if !ready && !r.failed && !r.world.exec.cancelChunk(r.task) && done != nil {
		// A dequeued worker sees this under mu before releasing its result.
		done.Add(1)
		r.releaseDone = done
	}
	r.mu.Unlock()
	if ready {
		r.world.ReleaseWait(r.pos, done)
	}
}

// CancelQueued cancels only work that has not started, so a nearer request
// can take its window slot without duplicating running generation.
func (r *ChunkRequest) CancelQueued() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancelled || !r.world.exec.cancelChunk(r.task) {
		return false
	}
	r.cancelled = true
	return true
}
