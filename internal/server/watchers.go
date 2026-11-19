package server

import (
	"iter"

	"github.com/google/uuid"
)

// assignPlayerSlot runs under s.mu, before publishing the connection.
func (s *Server) assignPlayerSlot(c *client) {
	c.slot = s.nextPlayerSlot
	if n := len(s.freePlayerSlots); n > 0 {
		c.slot = s.freePlayerSlots[n-1]
		s.freePlayerSlots = s.freePlayerSlots[:n-1]
	} else {
		s.nextPlayerSlot++
	}
	s.playerSlotGeneration++
	c.slotGeneration = s.playerSlotGeneration
}

type watcherEntry[T any] struct {
	viewer     *client
	generation uint64
	value      *T
}

// watcherSet indexes connection-local state by an online player's reusable slot.
// Identity and generation checks keep departures and old tick snapshots from
// touching the next connection to occupy a slot. vanilla: TrackedEntity.seenBy
// The body's kind lock (trackMu for waypoints) guards every access.
type watcherSet[T any] []watcherEntry[T]

func (s watcherSet[T]) get(c *client) (*T, bool) {
	if c.slot < len(s) && s[c.slot].viewer == c {
		return s[c.slot].value, true
	}
	return nil, false
}

func (s *watcherSet[T]) set(c *client, value *T) {
	if c.slot >= len(*s) {
		*s = append(*s, make([]watcherEntry[T], c.slot+1-len(*s))...)
	}
	e := &(*s)[c.slot]
	if e.generation <= c.slotGeneration {
		*e = watcherEntry[T]{viewer: c, generation: c.slotGeneration, value: value}
	}
}

func (s watcherSet[T]) delete(c *client) {
	if c.slot < len(s) && s[c.slot].viewer == c {
		s[c.slot].viewer, s[c.slot].value = nil, nil
	}
}

func (s watcherSet[T]) all() iter.Seq2[uuid.UUID, *T] {
	return func(yield func(uuid.UUID, *T) bool) {
		for _, e := range s {
			if e.viewer != nil && !yield(e.viewer.player.UUID, e.value) {
				return
			}
		}
	}
}

func (s watcherSet[T]) clients() iter.Seq[*client] {
	return func(yield func(*client) bool) {
		for _, e := range s {
			if e.viewer != nil && !yield(e.viewer) {
				return
			}
		}
	}
}
