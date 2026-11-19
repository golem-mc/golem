package pluginruntime

type tickCall struct {
	fn   func()
	done chan struct{}
}

// Call runs a connection's callback on the tick goroutine, or refuses during shutdown.
// Called from the tick goroutine it runs fn inline, as waiting would block the tick on itself.
func (r *Runtime) Call(fn func()) bool {
	if r.OnTick() {
		fn()
		return true
	}
	call := tickCall{fn: fn, done: make(chan struct{})}
	if !r.enqueue(call) {
		return false
	}
	select {
	case <-call.done:
		return true
	case <-r.stopped:
		return false
	}
}

// Post queues a callback without waiting, including from another tick callback.
func (r *Runtime) Post(fn func()) { r.enqueue(tickCall{fn: fn}) }

func (r *Runtime) enqueue(call tickCall) bool {
	r.callsMu.Lock()
	defer r.callsMu.Unlock()
	select {
	case <-r.stopped:
		return false
	default:
	}
	r.calls = append(r.calls, call)
	r.callsPending.Store(true)
	return true
}

func (r *Runtime) runCalls() {
	if !r.callsPending.Load() {
		return
	}
	r.callsMu.Lock()
	calls := r.calls
	r.calls = nil
	r.callsPending.Store(false)
	r.callsMu.Unlock()
	// A callback's new work belongs to the next tick, bounding this batch.
	for _, call := range calls {
		func() {
			if call.done != nil {
				defer close(call.done)
			}
			call.fn()
		}()
	}
}

func (r *Runtime) stopCalls() {
	r.callsMu.Lock()
	defer r.callsMu.Unlock()
	close(r.stopped)
	r.calls = nil
	r.callsPending.Store(false)
}
