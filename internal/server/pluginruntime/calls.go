package pluginruntime

import "time"

const (
	// callFollowUp is how long a tick's start waits for a connection it just
	// released to hand in its next call: reading an already received packet
	// takes microseconds, so a longer wait means the connection has none.
	callFollowUp = time.Millisecond
	// callDrainBudget bounds the follow-up calls a tick's start runs, so a
	// flooding client cannot stall the tick.
	callDrainBudget = 10 * time.Millisecond
)

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

// drainCalls runs the queued calls, then the calls the connections they
// released hand in next, until none arrives within callFollowUp or the
// budget is spent. A connection waits on each event it fires, so running
// one call per connection per tick lets a client that sends faster than
// the ticks run build an unbounded backlog behind its listeners.
// vanilla: MinecraftServer.tickServer, which runs every packet received
// since the last tick
func (r *Runtime) drainCalls() {
	if !r.runCalls() {
		return
	}
	deadline := time.Now().Add(callDrainBudget)
	for {
		// A signal left from before the last batch says nothing new.
		select {
		case <-r.callsQueued:
		default:
		}
		if !r.callsPending.Load() {
			wait := min(callFollowUp, time.Until(deadline))
			if wait <= 0 {
				return
			}
			timer := time.NewTimer(wait)
			select {
			case <-r.callsQueued:
				timer.Stop()
			case <-timer.C:
				return
			case <-r.stopped:
				timer.Stop()
				return
			}
		}
		if time.Now().After(deadline) || !r.runCalls() {
			return
		}
	}
}

// runCalls runs the calls queued so far; true means one of them released a
// waiting connection.
func (r *Runtime) runCalls() (released bool) {
	if !r.callsPending.Load() {
		return false
	}
	r.callsMu.Lock()
	calls := r.calls
	r.calls = nil
	r.callsPending.Store(false)
	r.callsMu.Unlock()
	// A callback's new work belongs to the next batch, bounding this one.
	for _, call := range calls {
		released = released || call.done != nil
		func() {
			if call.done != nil {
				defer close(call.done)
			}
			call.fn()
		}()
	}
	return released
}

func (r *Runtime) stopCalls() {
	r.callsMu.Lock()
	defer r.callsMu.Unlock()
	close(r.stopped)
	r.calls = nil
	r.callsPending.Store(false)
}
