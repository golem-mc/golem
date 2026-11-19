package world

import (
	"runtime"
	"sync"
	"testing"
	"time"
)

// waitFlushWaiting waits until the marked FlushSaves goroutine is blocked in
// its wait, so a submission made next is ordered after the flush has entered
// the wait rather than racing its start.
func waitFlushWaiting(t *testing.T, mark string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	buf := make([]byte, 1<<20)
	for {
		dump := buf[:runtime.Stack(buf, true)]
		if goroutineSitsIn(dump, mark, "world.(*World).FlushSaves") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the flush never reached its wait:\n%s", dump)
		}
		time.Sleep(time.Millisecond)
	}
}

// TestAPanickingTaskLeavesThePoolRunning pins that a task's panic is the
// task's alone: the process survives it, its pool slot is given back, and the
// tasks after it still run.
func TestAPanickingTaskLeavesThePoolRunning(t *testing.T) {
	e := &executors{chunkSem: make(chan struct{}, 1), ioSem: make(chan struct{}, 1)}
	e.submitChunk(func() { panic("test panic") })
	e.waitChunk()
	ran := make(chan struct{})
	e.submitChunk(func() { close(ran) })
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("the task after a panicking one did not run")
	}
	e.stop()
}

// TestChunkBacklogKeepsSubmissionOrder parks the single runner while a view
// distance of 32 queues, then checks that nearest-first submissions stay in
// order instead of racing thousands of goroutines for a semaphore.
func TestChunkBacklogKeepsSubmissionOrder(t *testing.T) {
	e := &executors{chunkSem: make(chan struct{}, 1)}
	gate := make(chan struct{})
	e.submitChunk(func() { <-gate })
	const chunks = 65 * 65
	order := make([]int, 0, chunks)
	for i := range chunks {
		e.submitChunk(func() { order = append(order, i) })
	}
	close(gate)
	e.waitChunk()
	for i, got := range order {
		if got != i {
			t.Fatalf("task %d ran at position %d; closest-first submission order lost", got, i)
		}
	}
}

// BenchmarkChunkBacklog includes submission and draining a view-distance-32
// backlog, holding the runners until all work is queued.
func BenchmarkChunkBacklog(b *testing.B) {
	for b.Loop() {
		e := &executors{chunkSem: make(chan struct{}, 8)}
		gate := make(chan struct{})
		for range 8 {
			e.submitChunk(func() { <-gate })
		}
		for range 65 * 65 {
			e.submitChunk(func() {})
		}
		close(gate)
		e.waitChunk()
	}
}

func TestChunkDrainCancelsEveryPendingTask(t *testing.T) {
	e := &executors{chunkSem: make(chan struct{}, 1)}
	gate, started := make(chan struct{}), make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	defer release()
	e.submitChunk(func() { close(started); <-gate })
	<-started
	for range 64 {
		task := e.queueChunk(func() { t.Error("cancelled task ran") }, 0)
		if !e.cancelChunk(task) || e.cancelChunk(task) {
			t.Fatal("cancellation accounting")
		}
	}
	release()
	e.stop()
	e.chunkMu.Lock()
	defer e.chunkMu.Unlock()
	if len(e.chunkQueue) != 0 {
		t.Fatal("cancelled backlog retained")
	}
}

func waitChunkMutex(t *testing.T, mark string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	buf := make([]byte, 1<<20)
	for {
		dump := buf[:runtime.Stack(buf, true)]
		if goroutineSitsIn(dump, mark, "sync.(*Mutex).Lock(") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("runner or submitter did not reach queue mutex")
		}
		runtime.Gosched()
	}
}

// BenchmarkChunkBursts includes submission, draining and waiting across idle gaps.
func BenchmarkChunkBursts(b *testing.B) {
	e := newExecutors()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		for range 32 {
			gate := make(chan struct{})
			for range 8 {
				e.submitChunk(func() { <-gate })
			}
			for range 128 {
				e.submitChunk(func() {})
			}
			close(gate)
			e.waitChunk()
		}
	}
	b.StopTimer()
	e.stop()
}
