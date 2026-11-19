package world

// ChunkHooks observe chunks entering and leaving memory. Both run under the
// world's lock or on a worker, so they must only record the event.
type ChunkHooks struct {
	// Load runs when a chunk enters memory; fresh is true for a generated one.
	Load func(pos ChunkPos, c *Chunk, fresh bool)
	// Unload runs when a chunk leaves memory.
	Unload func(pos ChunkPos, c *Chunk)
}

// SetChunkHooks installs the world's chunk hooks, nil to remove them.
func (w *World) SetChunkHooks(h *ChunkHooks) { w.chunkHooks.Store(h) }

func (w *World) chunkUnloaded(pos ChunkPos, c *Chunk) {
	if h := w.chunkHooks.Load(); h != nil && h.Unload != nil {
		h.Unload(pos, c)
	}
}
