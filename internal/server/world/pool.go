package world

import (
	"container/heap"
	"log/slog"
	"runtime"
	"runtime/debug"
	"sync"
)

// Generation and lighting share a priority queue with bounded runners; I/O
// has its own semaphore. Equal priorities retain submission order.
// vanilla: ChunkTaskPriorityQueue
// Relights after block changes merge through relightlane.go.
type executors struct {
	chunkSem   chan struct{}
	ioSem      chan struct{}
	chunkWg    sync.WaitGroup
	chunkMu    sync.Mutex
	chunkQueue chunkTasks
	chunkSeq   uint64
	ioWg       sync.WaitGroup

	// stopped is set by stop before it waits, so a task submitted during or
	// after the wait runs inline instead of Adding to a WaitGroup whose Wait
	// may be returning (Add concurrent with reaching zero is forbidden). That
	// window exists after Shutdown closes the world while the last connection
	// still asks for chunks. mu guards stopped.
	mu      sync.RWMutex
	stopped bool
}

func newExecutors() *executors {
	// Scale by the scheduler's CPU budget, leaving room for network, tick
	// and I/O work, as vanilla sizes its worldgen executor from available
	// processors.
	cores := runtime.GOMAXPROCS(0)
	chunk := chunkRunnerLimit(cores)
	io := max(1, min(cores/4, 2))
	return &executors{
		chunkSem: make(chan struct{}, chunk),
		ioSem:    make(chan struct{}, io),
	}
}

// submit runs task on a goroutine bounded by sem and tracked by wg, or inline
// on the caller once the pool has stopped. done, if non-nil, is the submitter's
// own counter, decremented with the task so it can wait on just its tasks; it
// is bumped under the same lock, and an inline run never touches it.
func (e *executors) submit(sem chan struct{}, wg, done *sync.WaitGroup, task func()) {
	e.mu.RLock()
	if e.stopped {
		e.mu.RUnlock()
		task()
		return
	}
	wg.Add(1)
	if done != nil {
		done.Add(1)
	}
	e.mu.RUnlock()
	go func() {
		defer wg.Done()
		if done != nil {
			defer done.Done()
		}
		sem <- struct{}{}
		defer func() { <-sem }()
		runPoolTask(task)
	}()
}

// runPoolTask returns the runner even when a task panics. A failed chunk's
// result channel stays unanswered, as with the I/O pool, but cancelling it
// does not block; a failed relight answers its requests nil.
func runPoolTask(task func()) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("world task panicked", "panic", p, "stack", string(debug.Stack()))
		}
	}()
	task()
}

// submitChunk queues task on the shared generation/lighting pool.
func (e *executors) submitChunk(task func()) {
	e.queueChunk(task, 0)
}

func (e *executors) queueChunk(task func(), priority int) *chunkTask {
	queued := &chunkTask{run: task, priority: priority, index: -1}
	e.mu.RLock()
	if e.stopped {
		e.mu.RUnlock()
		task()
		return queued
	}
	e.chunkWg.Add(1)
	e.chunkMu.Lock()
	queued.seq = e.chunkSeq
	e.chunkSeq++
	heap.Push(&e.chunkQueue, queued)
	start := false
	select {
	case e.chunkSem <- struct{}{}:
		start = true
	default:
	}
	e.chunkMu.Unlock()
	e.mu.RUnlock()
	if start {
		go e.runChunks()
	}
	return queued
}

// chunkRunners bounds generation and lighting across dimensions. Halo terrain
// is shaped inline so work cannot escape a runner's permit.
var chunkRunners = sync.OnceValue(func() chan struct{} {
	return make(chan struct{}, chunkRunnerLimit(runtime.GOMAXPROCS(0)))
})

func chunkRunnerLimit(cores int) int { return max(1, min(cores-3, 8)) }

func (e *executors) runChunks() {
	runners := chunkRunners()
	runners <- struct{}{}
	defer func() { <-runners }()
	for {
		e.chunkMu.Lock()
		if len(e.chunkQueue) == 0 {
			e.chunkQueue = nil
			<-e.chunkSem
			e.chunkMu.Unlock()
			return
		}
		task := heap.Pop(&e.chunkQueue).(*chunkTask)
		e.chunkMu.Unlock()
		runPoolTask(task.run)
		e.chunkWg.Done()
	}
}

// submitIOTracked runs task on the I/O pool, also registered with done (may be
// nil), so a caller can wait for just its saves.
func (e *executors) submitIOTracked(done *sync.WaitGroup, task func()) {
	e.submit(e.ioSem, &e.ioWg, done, task)
}

// waitChunk blocks until every queued generation and light task has finished,
// so a benchmark measures a change's own light work. Safe only while nothing
// else submits: a WaitGroup forbids an Add from zero concurrent with Wait.
func (e *executors) waitChunk() { e.chunkWg.Wait() }

// waitIO blocks until every queued save has finished. Like waitChunk it is safe
// only without concurrent submitters (writers quiesced or their saves parked);
// beside a live writer use drainIO.
func (e *executors) waitIO() { e.ioWg.Wait() }

// drainIO blocks until every queued save has finished, holding submissions out
// with the write lock so no release Adds beside the Wait; held-out tasks run
// after, the pool staying open (stop closes it). It waits for the whole
// backlog, not one submitter's.
func (e *executors) drainIO() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ioWg.Wait()
}

// FlushLighting blocks until every generation and light task taken has
// finished, for a benchmark of light-scheduling work to measure it inside its
// window rather than racing the next iteration.
func (w *World) FlushLighting() { w.exec.waitChunk() }

// stop waits for in-flight tasks; later submissions run inline on their
// callers.
func (e *executors) stop() {
	e.mu.Lock()
	e.stopped = true
	e.mu.Unlock()
	e.chunkWg.Wait()
	e.ioWg.Wait()
}

// chunkTask's index and priority are guarded by chunkMu; -1 means dequeued.
type chunkTask struct {
	run      func()
	priority int
	seq      uint64
	index    int
}

type chunkTasks []*chunkTask

func (q chunkTasks) Len() int { return len(q) }
func (q chunkTasks) Less(i, j int) bool {
	if q[i].priority != q[j].priority {
		return q[i].priority < q[j].priority
	}
	return q[i].seq < q[j].seq
}
func (q chunkTasks) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index, q[j].index = i, j
}
func (q *chunkTasks) Push(v any) {
	t := v.(*chunkTask)
	t.index = len(*q)
	*q = append(*q, t)
}
func (q *chunkTasks) Pop() any {
	n := len(*q) - 1
	t := (*q)[n]
	(*q)[n] = nil
	*q = (*q)[:n]
	t.index = -1
	return t
}

func (e *executors) cancelChunk(t *chunkTask) bool {
	removed := false
	e.chunkMu.Lock()
	if t != nil && t.index >= 0 {
		heap.Remove(&e.chunkQueue, t.index)
		e.chunkWg.Done()
		removed = true
	}
	e.chunkMu.Unlock()
	return removed
}

func (e *executors) prioritizeChunk(t *chunkTask, priority int) {
	e.chunkMu.Lock()
	if t != nil && t.index >= 0 && t.priority != priority {
		t.priority = priority
		heap.Fix(&e.chunkQueue, t.index)
	}
	e.chunkMu.Unlock()
}
