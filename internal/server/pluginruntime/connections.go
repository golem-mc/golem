package pluginruntime

import (
	"sync"
	"sync/atomic"
)

type connectionTasks struct {
	pending atomic.Bool
	mu      sync.Mutex
	queue   []func()
}

// Enqueue delivers a connection's continuation at the next tick, outside server locks.
func (r *Runtime) Enqueue(fn func()) {
	q := &r.connections
	q.mu.Lock()
	q.queue = append(q.queue, fn)
	q.pending.Store(true)
	q.mu.Unlock()
}

func (r *Runtime) runConnections() {
	q := &r.connections
	// No lock or allocation when no connection has a listener to call.
	if !q.pending.Load() {
		return
	}
	q.mu.Lock()
	queue := q.queue
	q.queue = nil
	q.pending.Store(false)
	q.mu.Unlock()
	for _, fn := range queue {
		fn()
	}
}
