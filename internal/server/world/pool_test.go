package world

import (
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestFlushSavesDoesNotBlockSubmissions parks a save on its region file and
// flushes while it is parked. The flush must wait only for the saves already
// in flight, so an unrelated task submitted beside it — the chunk pool's lock
// is never held — still runs. Otherwise one player's departure, whose flush
// covers a slow backlog, freezes every other player's relight for as long as
// the disk takes.
func TestFlushSavesDoesNotBlockSubmissions(t *testing.T) {
	w, _ := NewWorld(t.TempDir(), NewFlatGenerator())
	t.Cleanup(func() { _ = w.Close() })
	pos := ChunkPos{}
	_, _ = w.Acquire(pos)
	w.SetBlock(5, GroundLevel-1, 3, rsState(t, "minecraft:diamond_block", nil))

	widenIOPool(w)
	baseline, unpark := parkRelease(t, w, pos)
	w.Release(pos)
	waitParkedSaves(t, baseline+1)

	flushed := make(chan struct{})
	flushMark := make(chan string, 1)
	go func() {
		flushMark <- goroutineMark()
		w.FlushSaves()
		close(flushed)
	}()
	// Wait for the flush to be inside its wait before submitting, so the
	// submission is ordered after the flush has entered the wait rather
	// than racing its start.
	waitFlushWaiting(t, <-flushMark)

	// Submitted the way the server submits a relight: while the flush is
	// waiting on the parked save, this has to reach the chunk pool. Run off
	// the test goroutine so a submit that blocks fails the timeout instead
	// of wedging the test.
	relit := make(chan []LightChange, 1)
	go func() { relit <- <-w.RelightAsync(0, 0) }()
	select {
	case <-relit:
	case <-time.After(2 * time.Second):
		t.Fatal("a flush of a parked save blocked an unrelated relight")
	}

	unpark()
	select {
	case <-flushed:
	case <-time.After(2 * time.Second):
		t.Fatal("the flush did not finish once the save was unparked")
	}
}

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

// TestAReleaseWaitLeavesOtherBacklogsAlone queues one save a release tracks and
// another the pool already holds, then waits only the first. A departure must
// not wait out saves it did not queue, so the wait has to return with the other
// save still parked.
func TestAReleaseWaitLeavesOtherBacklogsAlone(t *testing.T) {
	w, _ := NewWorld(t.TempDir(), NewFlatGenerator())
	t.Cleanup(func() { _ = w.Close() })
	widenIOPool(w)

	other := ChunkPos{X: 32, Z: 0}
	_, _ = w.Acquire(other)
	w.SetBlock(32*16+5, GroundLevel-1, 3, rsState(t, "minecraft:gold_block", nil))
	otherBase, otherUnpark := parkRelease(t, w, other)
	w.Release(other)
	waitParkedSaves(t, otherBase+1)

	mine := ChunkPos{}
	_, _ = w.Acquire(mine)
	w.SetBlock(5, GroundLevel-1, 3, rsState(t, "minecraft:diamond_block", nil))
	mineBase, mineUnpark := parkRelease(t, w, mine)
	var saves sync.WaitGroup
	w.ReleaseWait(mine, &saves)
	waitParkedSaves(t, mineBase+1)

	mineUnpark()
	waited := make(chan struct{})
	go func() {
		saves.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(2 * time.Second):
		t.Fatal("a release's wait blocked on a save it did not queue")
	}

	otherUnpark()
	w.FlushSaves()
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

// Park every global permit with gates, then wait until the second executor's
// drain goroutines are blocked on their sends. No task timing decides the limit.
func TestChunkRunnersShareAGlobalLimit(t *testing.T) {
	n := cap(chunkRunners())
	a, b := newExecutors(), newExecutors()
	started := make(chan struct{}, 2*n)
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	defer release()
	var active, peak atomic.Int32
	task := func() {
		v := active.Add(1)
		for old := peak.Load(); v > old; old = peak.Load() {
			if peak.CompareAndSwap(old, v) {
				break
			}
		}
		started <- struct{}{}
		<-gate
		active.Add(-1)
	}
	for range n {
		a.submitChunk(task)
	}
	for range n {
		<-started
	}
	for range n {
		b.submitChunk(task)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if peak.Load() > int32(n) {
			t.Fatalf("%d simultaneous tasks, limit %d", peak.Load(), n)
		}
		buf := make([]byte, 1<<20)
		dump := string(buf[:runtime.Stack(buf, true)])
		waiting := 0
		for stack := range strings.SplitSeq(dump, "\n\n") {
			if strings.Contains(stack, "[chan send]") && strings.Contains(stack, "world.(*executors).runChunks(") {
				waiting++
			}
		}
		if waiting >= n {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second executor never waited for global permits")
		}
		runtime.Gosched()
	}
	if active.Load() != int32(n) {
		t.Fatalf("active=%d, want %d", active.Load(), n)
	}
	release()
	a.stop()
	b.stop()
	if peak.Load() > int32(n) {
		t.Fatalf("peak=%d, limit=%d", peak.Load(), n)
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

// Park a drain at its empty-queue check and a new submitter on the same
// mutex. Either lock winner must leave a runner responsible for the new task.
func TestChunkDrainRestartsAcrossBursts(t *testing.T) {
	e := &executors{chunkSem: make(chan struct{}, 1)}
	for range 32 {
		entered, submitting := make(chan string), make(chan string)
		leave, submitted, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
		e.submitChunk(func() { entered <- goroutineMark(); <-leave })
		runner := <-entered
		e.chunkMu.Lock()
		func() {
			defer e.chunkMu.Unlock()
			close(leave)
			waitChunkMutex(t, runner)
			go func() {
				submitting <- goroutineMark()
				e.submitChunk(func() { close(finished) })
				close(submitted)
			}()
			waitChunkMutex(t, <-submitting)
		}()
		<-submitted
		<-finished
		e.waitChunk()
	}
	e.stop()
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

func TestChunkDrainSurvivesPanicAndStop(t *testing.T) {
	e := &executors{chunkSem: make(chan struct{}, 1)}
	entered, gate := make(chan struct{}), make(chan struct{})
	e.submitChunk(func() { close(entered); <-gate; panic("gated panic") })
	<-entered
	ran := false
	e.submitChunk(func() { ran = true })
	close(gate)
	e.stop()
	if !ran {
		t.Fatal("work after panic lost")
	}
	caller := goroutineMark()
	e.submitChunk(func() {
		if goroutineMark() != caller {
			t.Error("stopped submission was asynchronous")
		}
	})
}

func TestChunkDrainWithOneCPU(t *testing.T) {
	old := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(old)
	e := newExecutors()
	if cap(e.chunkSem) != 1 {
		t.Fatal("one CPU must have one local runner")
	}
	var calls int
	for range 100 {
		e.submitChunk(func() { calls++ })
	}
	e.stop()
	if calls != 100 {
		t.Fatalf("ran %d tasks", calls)
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

func TestChunkDrainStopWhileBacklogged(t *testing.T) {
	n := cap(chunkRunners())
	a, b := newExecutors(), newExecutors()
	gate, entered := make(chan struct{}), make(chan struct{}, n)
	for range n {
		a.submitChunk(func() { entered <- struct{}{}; <-gate })
	}
	for range n {
		<-entered
	}
	var calls int
	// One local runner keeps task writes serial even when other workers free up.
	b.chunkSem = make(chan struct{}, 1)
	for range 64 {
		b.submitChunk(func() { calls++ })
	}
	stopped := make(chan struct{})
	go func() { b.stop(); close(stopped) }()
	for {
		b.mu.RLock()
		stopping := b.stopped
		b.mu.RUnlock()
		if stopping {
			break
		}
		runtime.Gosched()
	}
	select {
	case <-stopped:
		t.Error("stop returned with queued work")
	default:
	}
	close(gate)
	<-stopped
	a.stop()
	if calls != 64 {
		t.Fatalf("completed %d tasks, want 64", calls)
	}
	caller := goroutineMark()
	b.submitChunk(func() {
		if goroutineMark() != caller {
			t.Error("stopped submission left caller")
		}
	})
	// Stopping either executor must leave global runner permits usable.
	c := newExecutors()
	ran := false
	c.submitChunk(func() { ran = true })
	c.stop()
	if !ran {
		t.Fatal("world stop prevented another executor from running")
	}
}
