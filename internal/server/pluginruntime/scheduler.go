package pluginruntime

import (
	"container/heap"
	"sync"
	"sync/atomic"
	"time"

	"github.com/golem-mc/golem/plugin"
)

type pluginScheduler struct{ host *Host }
type pluginTask struct {
	host        *Host
	fn          func()
	due, period int64
	order       uint64
	cancelled   atomic.Bool
	done        chan struct{}
}

func (t *pluginTask) Cancel() {
	if t.cancelled.CompareAndSwap(false, true) {
		close(t.done)
	}
}
func (t *pluginTask) Cancelled() bool { return t.cancelled.Load() }
func (t *pluginTask) run() {
	if t.Cancelled() || t.host.closed.Load() {
		return
	}
	if err := t.host.guard(func() error { t.fn(); return nil }); err != nil {
		t.host.Logger().Error("Error in scheduled task", "error", err)
		t.Cancel()
	}
}

type pluginTasks struct {
	mu        sync.Mutex
	ticks     taskQueue
	nextOrder uint64
	all       map[*pluginTask]struct{}
	// Workers share a condition and queue, so scheduling never waits for a worker.
	work    []*pluginTask
	ready   *sync.Cond
	workers sync.WaitGroup
	stopped bool
}

func (s *pluginScheduler) task(fn func()) *pluginTask {
	if fn == nil {
		panic("plugin: nil task")
	}
	t := &pluginTask{host: s.host, fn: fn, done: make(chan struct{})}
	r := &s.host.runtime.tasks
	r.mu.Lock()
	defer r.mu.Unlock()
	if s.host.closed.Load() || r.stopped {
		t.Cancel()
		return t
	}
	if r.all == nil {
		r.all = make(map[*pluginTask]struct{})
	}
	r.all[t] = struct{}{}
	return t
}
func (s *pluginScheduler) RunTask(fn func()) plugin.Task             { return s.RunLater(1, fn) }
func (s *pluginScheduler) RunLater(delay int, fn func()) plugin.Task { return s.RunTimer(delay, 0, fn) }
func (s *pluginScheduler) RunTimer(delay, period int, fn func()) plugin.Task {
	if period < 0 {
		panic("plugin: negative task period")
	}
	t := s.task(fn)
	r := &s.host.runtime.tasks
	r.mu.Lock()
	defer r.mu.Unlock()
	t.due = s.host.runtime.current.Load() + int64(max(delay, 1))
	t.period = int64(period)
	if !t.Cancelled() {
		t.order = r.nextOrder
		r.nextOrder++
		heap.Push(&r.ticks, t)
	}
	return t
}
func (r *Runtime) runTasks() {
	tasks := &r.tasks
	tasks.mu.Lock()
	var due []*pluginTask
	// The earliest deadline gates all future tasks; equal deadlines use registration order.
	now := r.current.Load()
	for len(tasks.ticks) > 0 {
		t := tasks.ticks[0]
		if !t.Cancelled() && t.due > now {
			break
		}
		heap.Pop(&tasks.ticks)
		if t.Cancelled() {
			delete(tasks.all, t)
			continue
		}
		due = append(due, t)
		if t.period == 0 {
			delete(tasks.all, t)
		} else {
			t.due = now + t.period
			heap.Push(&tasks.ticks, t)
		}
	}
	tasks.mu.Unlock()
	for _, t := range due {
		t.run()
	}
}
func (s *pluginScheduler) RunAsync(fn func()) plugin.Task { return s.RunAsyncLater(0, fn) }
func (s *pluginScheduler) RunAsyncLater(delay time.Duration, fn func()) plugin.Task {
	return s.RunAsyncTimer(delay, 0, fn)
}
func (s *pluginScheduler) RunAsyncTimer(delay, period time.Duration, fn func()) plugin.Task {
	if period < 0 {
		panic("plugin: negative async task period")
	}
	t := s.task(fn)
	t.period = int64(period)
	r := &s.host.runtime.tasks
	r.mu.Lock()
	if r.ready == nil && !r.stopped {
		r.ready = sync.NewCond(&r.mu)
		for range 4 {
			r.workers.Go(r.worker)
		}
	}
	r.mu.Unlock()
	go func() {
		defer func() {
			if period != 0 || t.Cancelled() {
				r.mu.Lock()
				delete(r.all, t)
				r.mu.Unlock()
			}
		}()
		timer := time.NewTimer(max(delay, 0))
		defer timer.Stop()
		for {
			select {
			case <-t.done:
				return
			case <-timer.C:
			}
			r.mu.Lock()
			if !t.Cancelled() && !r.stopped {
				r.work = append(r.work, t)
				r.ready.Signal()
			}
			r.mu.Unlock()
			if period == 0 {
				return
			}
			timer.Reset(period)
		}
	}()
	return t
}
func (r *pluginTasks) worker() {
	for {
		r.mu.Lock()
		for len(r.work) == 0 && !r.stopped {
			r.ready.Wait()
		}
		if r.stopped {
			r.mu.Unlock()
			return
		}
		t := r.work[0]
		r.work[0] = nil
		r.work = r.work[1:]
		r.mu.Unlock()
		t.run()
		r.mu.Lock()
		// A one-shot stays owned until the worker finishes, including queue time.
		if t.period == 0 {
			delete(r.all, t)
		}
		r.mu.Unlock()
	}
}
func (s *pluginScheduler) cancelAll() {
	r := &s.host.runtime.tasks
	r.mu.Lock()
	defer r.mu.Unlock()
	for t := range r.all {
		if t.host == s.host {
			t.Cancel()
			delete(r.all, t)
		}
	}
}
func (r *pluginTasks) stop() {
	r.mu.Lock()
	r.stopped = true
	for t := range r.all {
		t.Cancel()
	}
	r.ticks, r.work = nil, nil
	if r.ready != nil {
		r.ready.Broadcast()
	}
	r.mu.Unlock()
	r.workers.Wait()
}

// taskQueue orders deadlines, retaining original registration order for repeats.
type taskQueue []*pluginTask

func (q taskQueue) Len() int { return len(q) }
func (q taskQueue) Less(i, j int) bool {
	if q[i].due == q[j].due {
		return q[i].order < q[j].order
	}
	return q[i].due < q[j].due
}
func (q taskQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }
func (q *taskQueue) Push(v any)   { *q = append(*q, v.(*pluginTask)) }
func (q *taskQueue) Pop() any {
	old := *q
	t := old[len(old)-1]
	old[len(old)-1] = nil
	*q = old[:len(old)-1]
	return t
}
