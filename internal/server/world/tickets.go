package world

import (
	"sync"
	"sync/atomic"
)

// MaxTicketRadius caps the radius of a plugin chunk ticket.
const MaxTicketRadius = 32

type ticketKey struct {
	pos    ChunkPos
	radius int
}

// chunkTickets is the plugin tickets of one dimension. They are not saved:
// a plugin adds them again when it enables.
type chunkTickets struct {
	mu sync.Mutex
	// generation is bumped under mu after changing ticket counts.
	generation atomic.Uint64
	// held counts the identical tickets added at one centre and radius.
	held map[ticketKey]int
	// ticking counts, per chunk, the tickets that block-tick it.
	ticking map[ChunkPos]int
	// loaded and entity count, per chunk, the tickets that load it and that
	// entity-tick it.
	loaded, entity map[ChunkPos]int
	// entityHeld counts, per chunk, the test holds that entity-tick it.
	entityHeld map[ChunkPos]int
	// timed is the expiry tick of each ticket placed with AddTimedTicket.
	timed map[ticketKey]int64
}

// TicketGeneration identifies the current ticket counts for ticking decisions.
func (w *World) TicketGeneration() uint64 { return w.tickets.generation.Load() }

// ticketSquare lists the chunks within reach (Chebyshev) of pos.
func ticketSquare(pos ChunkPos, reach int) []ChunkPos {
	if reach < 0 {
		return nil
	}
	out := make([]ChunkPos, 0, (2*reach+1)*(2*reach+1))
	for dz := -reach; dz <= reach; dz++ {
		for dx := -reach; dx <= reach; dx++ {
			out = append(out, ChunkPos{X: pos.X + int32(dx), Z: pos.Z + int32(dz)})
		}
	}
	return out
}

// AddTicket keeps the chunks within radius of pos loaded, and those within
// radius-1 block-ticking, until a matching RemoveTicket. It returns the
// loaded chunks; a radius outside 0..MaxTicketRadius adds nothing.
// vanilla: ServerChunkCache.addTicketWithRadius (level 33 - radius,
// ChunkLevel.isBlockTicking)
func (w *World) AddTicket(pos ChunkPos, radius int) []ChunkPos {
	if radius < 0 || radius > MaxTicketRadius {
		return nil
	}
	t := &w.tickets
	t.mu.Lock()
	if t.held == nil {
		t.held = map[ticketKey]int{}
		t.ticking = map[ChunkPos]int{}
		t.loaded = map[ChunkPos]int{}
		t.entity = map[ChunkPos]int{}
	}
	t.held[ticketKey{pos, radius}]++
	for _, c := range ticketSquare(pos, radius-1) {
		t.ticking[c]++
	}
	for _, c := range ticketSquare(pos, radius) {
		t.loaded[c]++
	}
	for _, c := range ticketSquare(pos, radius-2) {
		t.entity[c]++
	}
	t.generation.Add(1)
	t.mu.Unlock()
	loaded := ticketSquare(pos, radius)
	for range w.AcquireMany(loaded) {
	}
	return loaded
}

// RemoveTicket undoes one AddTicket with the same pos and radius; without
// one it does nothing.
func (w *World) RemoveTicket(pos ChunkPos, radius int) {
	t := &w.tickets
	key := ticketKey{pos, radius}
	t.mu.Lock()
	if t.held[key] == 0 {
		t.mu.Unlock()
		return
	}
	if t.held[key]--; t.held[key] == 0 {
		delete(t.held, key)
	}
	for _, c := range ticketSquare(pos, radius-1) {
		if t.ticking[c]--; t.ticking[c] == 0 {
			delete(t.ticking, c)
		}
	}
	for _, c := range ticketSquare(pos, radius) {
		if t.loaded[c]--; t.loaded[c] == 0 {
			delete(t.loaded, c)
		}
	}
	for _, c := range ticketSquare(pos, radius-2) {
		if t.entity[c]--; t.entity[c] == 0 {
			delete(t.entity, c)
		}
	}
	t.generation.Add(1)
	t.mu.Unlock()
	for _, c := range ticketSquare(pos, radius) {
		w.Release(c)
	}
}

// AddTimedTicket places an AddTicket that ExpireTickets removes once the
// world's age reaches until; placing it again at the same pos and radius
// moves the expiry instead of adding a second ticket.
// vanilla: TicketStorage.addTicket (a ticket's timeout is renewed)
func (w *World) AddTimedTicket(pos ChunkPos, radius int, until int64) {
	key := ticketKey{pos, radius}
	t := &w.tickets
	t.mu.Lock()
	if _, ok := t.timed[key]; ok {
		t.timed[key] = until
		t.mu.Unlock()
		return
	}
	if t.timed == nil {
		t.timed = map[ticketKey]int64{}
	}
	t.timed[key] = until
	t.mu.Unlock()
	w.AddTicket(pos, radius)
}

// ExpireTickets removes the timed tickets whose expiry is at or before now.
func (w *World) ExpireTickets(now int64) {
	t := &w.tickets
	t.mu.Lock()
	var due []ticketKey
	for key, until := range t.timed {
		if until <= now {
			due = append(due, key)
			delete(t.timed, key)
		}
	}
	t.mu.Unlock()
	for _, key := range due {
		w.RemoveTicket(key.pos, key.radius)
	}
}

// ChunkTicketed reports whether a plugin ticket makes pos tick.
func (w *World) ChunkTicketed(pos ChunkPos) bool {
	t := &w.tickets
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ticking[pos] > 0
}

// ChunkTicketEntityTicking reports whether a plugin ticket or a test hold entity-ticks pos.
// vanilla: ChunkLevel.isEntityTicking (level <= 31)
func (w *World) ChunkTicketEntityTicking(pos ChunkPos) bool {
	t := &w.tickets
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.entity[pos] > 0 || t.entityHeld[pos] > 0
}

// AnyChunkTicketed reports whether any plugin ticket is held.
func (w *World) AnyChunkTicketed() bool {
	t := &w.tickets
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.held) > 0
}

// HoldEntityTicking makes pos entity-tick, and keeps it loaded, until the
// release it returns is called. It is the test hooks' way to simulate a chunk
// no player stands near; plugin tickets are not involved.
func (w *World) HoldEntityTicking(pos ChunkPos) (release func()) {
	t := &w.tickets
	t.mu.Lock()
	if t.entityHeld == nil {
		t.entityHeld = map[ChunkPos]int{}
	}
	t.entityHeld[pos]++
	t.generation.Add(1)
	t.mu.Unlock()
	_, _ = w.Acquire(pos)
	return func() {
		t.mu.Lock()
		if t.entityHeld[pos]--; t.entityHeld[pos] == 0 {
			delete(t.entityHeld, pos)
		}
		t.generation.Add(1)
		t.mu.Unlock()
		w.Release(pos)
	}
}

// AnyEntityTicketed reports whether a plugin ticket or a held chunk
// entity-ticks anything.
func (w *World) AnyEntityTicketed() bool {
	t := &w.tickets
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.held) > 0 || len(t.entityHeld) > 0
}
