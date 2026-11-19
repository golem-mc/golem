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
	select {
	case r.callsQueued <- struct{}{}:
	default:
	}
	return true
}

// CallsQueued is signalled when a call is queued, so the tick loop can run
// it between ticks instead of keeping its connection waiting for the next
// tick's start.
func (r *Runtime) CallsQueued() <-chan struct{} { return r.callsQueued }

// RunCallsBetweenTicks runs the queued calls on the tick loop's goroutine
// while it waits for the next tick, which makes that goroutine the tick
// goroutine for them. vanilla: MinecraftServer.waitUntilNextTick, which
// runs the server thread's queued packet tasks while it waits
func (r *Runtime) RunCallsBetweenTicks() {
	r.tickGoroutine.Store(goroutineID())
	r.runCalls()
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
