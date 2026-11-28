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

type clientSlot struct {
	generation uint64
	indexPlus1 int
}

// clientSet tracks online clients by reusable slot with a dense slice for
// iteration. Identity and generation checks keep departures and reconnects from
// touching a newer connection that reused a slot.
// vanilla: ChunkMap.TrackedEntity.seenBy
type clientSet struct {
	list  []*client
	slots []clientSlot
}

func (s *clientSet) has(c *client) bool {
	if c.slot < len(s.slots) {
		if idx := s.slots[c.slot].indexPlus1; idx > 0 && s.list[idx-1] == c {
			return true
		}
	}
	return false
}

func (s *clientSet) add(c *client) bool {
	if c.slot >= len(s.slots) {
		s.slots = append(s.slots, make([]clientSlot, c.slot+1-len(s.slots))...)
	}
	e := &s.slots[c.slot]
	if idx := e.indexPlus1; idx > 0 {
		if s.list[idx-1] == c {
			return false
		}
		if e.generation > c.slotGeneration {
			return false
		}
		s.list[idx-1] = c
		e.generation = c.slotGeneration
		return true
	}
	if e.generation > c.slotGeneration {
		return false
	}
	s.list = append(s.list, c)
	*e = clientSlot{generation: c.slotGeneration, indexPlus1: len(s.list)}
	return true
}

func (s *clientSet) delete(c *client) bool {
	if c.slot >= len(s.slots) {
		return false
	}
	e := &s.slots[c.slot]
	idx := e.indexPlus1
	if idx == 0 || s.list[idx-1] != c {
		return false
	}
	last := len(s.list) - 1
	if idx-1 != last {
		moved := s.list[last]
		s.list[idx-1] = moved
		s.slots[moved.slot].indexPlus1 = idx
	}
	s.list[last] = nil
	s.list = s.list[:last]
	e.indexPlus1 = 0
	return true
}

func (s *clientSet) clear() {
	for _, c := range s.list {
		s.slots[c.slot].indexPlus1 = 0
	}
	clear(s.list)
	s.list = s.list[:0]
}
