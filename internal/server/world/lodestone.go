package world

import (
	"sync"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// lodestoneCacheMax bounds the unloaded-destination answers kept.
const lodestoneCacheMax = 1024

// lodestoneCache remembers whether a saved chunk holds a lodestone at a
// position, per chunk so a load or write of that chunk drops its answers.
type lodestoneCache struct {
	mu      sync.Mutex
	chunks  map[ChunkPos]map[posKey]bool
	entries int
	// pending is the chunks with a read in flight; drop bumps the version a
	// read started under, so its stale answer is not published.
	pending map[ChunkPos]*lodestoneRead
}

type lodestoneRead struct {
	version uint64
	readers int
}

func (c *lodestoneCache) get(pos ChunkPos, key posKey) (found, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	found, ok = c.chunks[pos][key]
	return found, ok
}

// begin registers a read of the chunk; finish must follow with the returned
// version.
func (c *lodestoneCache) begin(pos ChunkPos) uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil {
		c.pending = map[ChunkPos]*lodestoneRead{}
	}
	r := c.pending[pos]
	if r == nil {
		r = &lodestoneRead{}
		c.pending[pos] = r
	}
	r.readers++
	return r.version
}

// finish ends a read begun at version and publishes its answer unless the
// chunk was loaded or written since. A full cache gives up one other answer
// rather than all of them.
func (c *lodestoneCache) finish(pos ChunkPos, key posKey, version uint64, found bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.pending[pos]
	current := r.version == version
	if r.readers--; r.readers == 0 {
		delete(c.pending, pos)
	}
	if !current {
		return
	}
	if c.chunks == nil {
		c.chunks = map[ChunkPos]map[posKey]bool{}
	}
	if _, dup := c.chunks[pos][key]; !dup && c.entries >= lodestoneCacheMax {
		c.evictOne(pos)
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

// evictOne removes an arbitrary answer, outside keep's chunk when it can.
func (c *lodestoneCache) evictOne(keep ChunkPos) {
	for pos, m := range c.chunks {
		if pos == keep && len(c.chunks) > 1 {
			continue
		}
		for key := range m {
			delete(m, key)
			c.entries--
			break
		}
		if len(m) == 0 {
			delete(c.chunks, pos)
		}
		return
	}
}

func (c *lodestoneCache) drop(pos ChunkPos) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r := c.pending[pos]; r != nil {
		r.version++
	}
	if m, ok := c.chunks[pos]; ok {
		c.entries -= len(m)
		delete(c.chunks, pos)
	}
}

// LodestoneCached reports whether a lodestone stands at the position when that
// is known without reading the disk: the live block in a loaded chunk, a kept
// answer for a saved one. known is false otherwise; ResolveLodestone finds out.
func (w *World) LodestoneCached(x, y, z int32) (present, known bool) {
	if state, ok := w.blockIfLoaded(x, y, z); ok {
		return registry.BlockOf(state) == registry.BlockLodestone, true
	}
	return w.lodestones.get(ChunkPos{X: x >> 4, Z: z >> 4}, posKey{x, y, z})
}

// ResolveLodestone reads the saved chunk for LodestoneCached's answer, and
// keeps it. A chunk never saved has none, and is not generated or loaded to
// find out. It reads the disk, so no lock a tick holds may be held.
// vanilla: PoiManager.existsAtPosition
func (w *World) ResolveLodestone(x, y, z int32) bool {
	if present, known := w.LodestoneCached(x, y, z); known {
		return present
	}
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	key := posKey{x, y, z}
	version := w.lodestones.begin(pos)
	found := w.savedLodestone(pos, x, y, z)
	w.lodestones.finish(pos, key, version, found)
	// A chunk that loaded meanwhile answers from its live block.
	if present, known := w.LodestoneCached(x, y, z); known {
		return present
	}
	return found
}

// savedLodestone reads the one section of the saved chunk that holds y.
func (w *World) savedLodestone(pos ChunkPos, x, y, z int32) (found bool) {
	_ = w.withChunkRoot(pos, func(root nbt.Compound) error {
		sections, _ := nbt.Get[nbt.List](root, "sections")
		for _, item := range sections.Items {
			sec, ok := nbt.CompoundView(item)
			if !ok {
				continue
			}
			sy, _ := nbt.Get[nbt.Byte](sec, "Y")
			if int32(sy) != y>>4 {
				continue
			}
			bs, ok := nbt.Get[nbt.Compound](sec, "block_states")
			if !ok {
				return nil
			}
			if version, ok := nbt.Get[nbt.Int](root, "DataVersion"); ok && version != 0 {
				palette, _ := nbt.Get[nbt.List](bs, "palette")
				fixLegacyPalette(palette, int32(version))
			}
			s := section{fill: airState}
			if err := decodeSectionBlocks(&s, bs); err != nil {
				return err
			}
			state := s.fill
			if b := s.blocks.Load(); b != nil {
				state = b.get(int(y&15<<8 | z&15<<4 | x&15))
			}
			found = registry.BlockOf(state) == registry.BlockLodestone
			return nil
		}
		return nil
	})
	return found
}
