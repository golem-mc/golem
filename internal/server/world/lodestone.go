package world

import (
	"sync"

	"github.com/golem-mc/golem/registry"
)

// lodestoneCacheMax bounds the unloaded-destination answers kept.
const lodestoneCacheMax = 1024

// lodestoneCache remembers whether a saved chunk holds a lodestone at a
// position, per chunk so a save of that chunk drops its answers.
type lodestoneCache struct {
	mu      sync.Mutex
	chunks  map[ChunkPos]map[posKey]bool
	entries int
}

func (c *lodestoneCache) get(pos ChunkPos, key posKey) (found, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	found, ok = c.chunks[pos][key]
	return found, ok
}

func (c *lodestoneCache) put(pos ChunkPos, key posKey, found bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries >= lodestoneCacheMax {
		c.chunks, c.entries = nil, 0
	}
	if c.chunks == nil {
		c.chunks = map[ChunkPos]map[posKey]bool{}
	}
	m := c.chunks[pos]
	if m == nil {
		m = map[posKey]bool{}
		c.chunks[pos] = m
	}
	if _, dup := m[key]; !dup {
		c.entries++
	}
	m[key] = found
}

func (c *lodestoneCache) drop(pos ChunkPos) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := c.chunks[pos]; ok {
		c.entries -= len(m)
		delete(c.chunks, pos)
	}
}

// LodestoneAt reports whether a lodestone stands at the position: the live
// block in a loaded chunk, the saved chunk otherwise. A chunk never saved has
// none, and is not generated or loaded to find out.
// vanilla: PoiManager.existsAtPosition
func (w *World) LodestoneAt(x, y, z int32) bool {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	if w.ChunkLoaded(pos) {
		return registry.BlockOf(w.BlockAt(x, y, z)) == registry.BlockLodestone
	}
	key := posKey{x, y, z}
	if found, ok := w.lodestones.get(pos, key); ok {
		return found
	}
	found := false
	if c, err := w.loadChunk(pos); err == nil && c != nil {
		found = registry.BlockOf(c.BlockAt(x&15, y, z&15)) == registry.BlockLodestone
	}
	w.lodestones.put(pos, key, found)
	return found
}
