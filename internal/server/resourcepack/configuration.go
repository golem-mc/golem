package resourcepack

import "github.com/google/uuid"

// Configuration tracks unresolved packs before the connection can join.
// Its caller serialises updates with pack writes and phase changes.
type Configuration struct {
	pending  map[uuid.UUID]struct{}
	started  bool
	finished bool
}

// Offer holds configuration for id unless Finish Configuration is already sent.
func (c *Configuration) Offer(id uuid.UUID) {
	if c.finished {
		return
	}
	if c.pending == nil {
		c.pending = make(map[uuid.UUID]struct{})
	}
	c.pending[id] = struct{}{}
}

// Start activates the pack task after the earlier configuration tasks finish.
// It reports whether to send Finish Configuration.
func (c *Configuration) Start() bool {
	c.started = true
	return c.finish()
}

// Resolve removes a pack after its terminal response or server-side removal,
// reporting whether to send Finish Configuration.
func (c *Configuration) Resolve(id uuid.UUID) bool {
	delete(c.pending, id)
	return c.finish()
}

// Pending reports whether the active pack task is holding configuration.
func (c *Configuration) Pending() bool { return c.started && !c.finished }

func (c *Configuration) finish() bool {
	if !c.started || c.finished || len(c.pending) != 0 {
		return false
	}
	c.finished = true
	return true
}
