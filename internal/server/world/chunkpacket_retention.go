package world

import (
	"container/list"
	"sync"
)

// Recently sent packets supplement the packets pinned by pending views. This
// bounds idle wire forms independently of the simulation distance; 512 holds
// a complete 21x21 view for later viewers.
const recentChunkPackets = 512

type retainedChunkPacket struct {
	pending int
	chunk   *Chunk
	recent  *list.Element
}

// chunkPacketRetention.mu precedes World.mu and lightFormsMu. It is never
// held while waiting for packetMu: a packet builder may be reading entity tags.
type chunkPacketRetention struct {
	mu      sync.Mutex
	entries map[ChunkPos]*retainedChunkPacket
	recent  list.List
}

// HoldChunkPacket pins a future send's cache until ReleaseChunkPacket, even
// when generation or another player's chunk batch has yet to finish.
func (w *World) HoldChunkPacket(pos ChunkPos) {
	w.packets.mu.Lock()
	defer w.packets.mu.Unlock()
	e := w.packetEntry(pos)
	e.pending++
	if e.recent != nil {
		w.packets.recent.Remove(e.recent)
		e.recent = nil
	}
}

// ReleaseChunkPacket releases one pending send, on send or cancellation.
func (w *World) ReleaseChunkPacket(pos ChunkPos) {
	w.packets.mu.Lock()
	e := w.packets.entries[pos]
	if e == nil || e.pending == 0 {
		panic("chunk packet released without a pending send")
	}
	e.pending--
	var evicted *Chunk
	if e.pending == 0 {
		if e.chunk == nil {
			delete(w.packets.entries, pos)
		} else {
			evicted = w.recentChunkPacket(pos, e)
		}
	}
	w.packets.mu.Unlock()
	if evicted != nil {
		w.evictChunkPacket(evicted)
	}
}

func (w *World) packetEntry(pos ChunkPos) *retainedChunkPacket {
	if w.packets.entries == nil {
		w.packets.entries = make(map[ChunkPos]*retainedChunkPacket)
	}
	e := w.packets.entries[pos]
	if e == nil {
		e = new(retainedChunkPacket)
		w.packets.entries[pos] = e
	}
	return e
}

func (w *World) retainChunkPacket(c *Chunk) {
	w.packets.mu.Lock()
	pos := ChunkPos{c.x, c.z}
	e := w.packetEntry(pos)
	e.chunk = c
	var evicted *Chunk
	if e.pending == 0 {
		evicted = w.recentChunkPacket(pos, e)
	}
	w.packets.mu.Unlock()
	if evicted != nil {
		w.evictChunkPacket(evicted)
	}
}

func (w *World) forgetChunkPacket(c *Chunk) {
	w.packets.mu.Lock()
	defer w.packets.mu.Unlock()
	pos := ChunkPos{c.x, c.z}
	e := w.packets.entries[pos]
	if e == nil || e.chunk != c {
		return
	}
	if e.recent != nil {
		w.packets.recent.Remove(e.recent)
		e.recent = nil
	}
	e.chunk = nil
	if e.pending == 0 {
		delete(w.packets.entries, pos)
	}
}

func (w *World) recentChunkPacket(pos ChunkPos, e *retainedChunkPacket) *Chunk {
	if e.recent == nil {
		e.recent = w.packets.recent.PushFront(pos)
	} else {
		w.packets.recent.MoveToFront(e.recent)
	}
	if w.packets.recent.Len() <= recentChunkPackets {
		return nil
	}
	last := w.packets.recent.Back()
	pos = last.Value.(ChunkPos)
	c := w.packets.entries[pos].chunk
	delete(w.packets.entries, pos)
	w.packets.recent.Remove(last)
	return c
}

func (w *World) evictChunkPacket(c *Chunk) {
	c.packetMu.Lock()
	defer c.packetMu.Unlock()
	w.packets.mu.Lock()
	defer w.packets.mu.Unlock()
	// A new view or send may have adopted the chunk while eviction waited.
	if e := w.packets.entries[ChunkPos{c.x, c.z}]; e != nil {
		if e.pending > 0 && e.chunk == nil {
			w.mu.RLock()
			if loaded := w.chunks[ChunkPos{c.x, c.z}]; loaded != nil && loaded.chunk == c {
				e.chunk = c
			}
			w.mu.RUnlock()
		}
		if e.chunk == c {
			return
		}
	}
	c.packet = chunkPacketCache{}
	w.mu.RLock()
	c.lightFormsMu.Lock()
	// Keep the initialized/stale distinction the light engine reads. Saves
	// and Update Light can rebuild without retaining these buffers again.
	c.lightFormsStale = c.lightFormsStale || c.lightEncoded != nil
	c.lightEncoded, c.chunkLightEncoded = nil, nil
	c.lightFormsRetained = false
	c.lightFormsMu.Unlock()
	w.mu.RUnlock()
}
