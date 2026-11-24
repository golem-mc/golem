package world

import (
	"fmt"
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

// finish ends a read begun at version and publishes its answers unless the
// chunk was loaded or written since, reporting whether it did. A full cache
// gives up one other answer rather than all of them.
func (c *lodestoneCache) finish(pos ChunkPos, version uint64, answers map[posKey]bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	r := c.pending[pos]
	current := r.version == version
	if r.readers--; r.readers == 0 {
		delete(c.pending, pos)
	}
	if !current {
		return false
	}
	if c.chunks == nil {
		c.chunks = map[ChunkPos]map[posKey]bool{}
	}
	for key, found := range answers {
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
	return true
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

// ForgetLodestones drops every kept answer, so a benchmark can measure the cold
// read again.
func (w *World) ForgetLodestones() {
	w.lodestones.mu.Lock()
	w.lodestones.chunks, w.lodestones.entries = nil, 0
	w.lodestones.mu.Unlock()
}

// LodestonePos is a block position whose lodestone answer is asked for.
type LodestonePos struct{ X, Y, Z int32 }

// ResolveLodestones reads the saved chunks behind positions
// LodestoneCached did not know, one read per chunk, and keeps the answers.
// The result holds an answer for each position found out, from the live block
// or from a read no load or write of its chunk overtook; a chunk never saved
// has none, and is not generated or loaded to find out. A position whose chunk
// could not be read is left out, not answered absent. It reads the disk, so no
// lock a tick holds may be held.
// vanilla: PoiManager.existsAtPosition
func (w *World) ResolveLodestones(positions []LodestonePos) map[LodestonePos]bool {
	answers := make(map[LodestonePos]bool, len(positions))
	var cold map[ChunkPos][]LodestonePos
	for _, p := range positions {
		if present, known := w.LodestoneCached(p.X, p.Y, p.Z); known {
			answers[p] = present
			continue
		}
		pos := ChunkPos{X: p.X >> 4, Z: p.Z >> 4}
		if cold == nil {
			cold = map[ChunkPos][]LodestonePos{}
		}
		cold[pos] = append(cold[pos], p)
	}
	for pos, ps := range cold {
		version := w.lodestones.begin(pos)
		found, err := w.savedLodestones(pos, ps)
		var published map[posKey]bool
		if err == nil {
			published = make(map[posKey]bool, len(found))
			for p, f := range found {
				published[posKey{p.X, p.Y, p.Z}] = f
			}
		}
		if err != nil {
			found = nil
		}
		current := w.lodestones.finish(pos, version, published)
		for _, p := range ps {
			// A chunk that loaded meanwhile answers from its live block.
			if present, known := w.LodestoneCached(p.X, p.Y, p.Z); known {
				answers[p] = present
			} else if f, ok := found[p]; ok && current {
				answers[p] = f
			}
		}
	}
	return answers
}

// savedLodestones reads a saved chunk once and tells which of ps, all in it,
// hold a lodestone, decoding each section asked about once. A chunk with no
// full record answers absent, as loading it generates a new one.
func (w *World) savedLodestones(pos ChunkPos, ps []LodestonePos) (map[LodestonePos]bool, error) {
	found := make(map[LodestonePos]bool, len(ps))
	for _, p := range ps {
		found[p] = false
	}
	_, err := w.withChunkRoot(pos, true, func(root nbt.Compound) error {
		if full, err := chunkIsFull(root); !full {
			return err
		}
		dataVersion := int32(0)
		if v, ok := nbt.Get[nbt.Int](root, "DataVersion"); ok {
			dataVersion = int32(v)
		}
		sections, _ := nbt.Get[nbt.List](root, "sections")
		for _, item := range sections.Items {
			sec, ok := nbt.CompoundView(item)
			if !ok {
				continue
			}
			sy, _ := nbt.Get[nbt.Byte](sec, "Y")
			var asked []LodestonePos
			for _, p := range ps {
				if p.Y>>4 == int32(sy) {
					asked = append(asked, p)
				}
			}
			if len(asked) == 0 {
				continue
			}
			s := section{fill: airState}
			if err := decodeSectionStates(&s, sec, dataVersion); err != nil {
				return fmt.Errorf("section %d: %w", int32(sy), err)
			}
			blocks := s.blocks.Load()
			for _, p := range asked {
				state := s.fill
				if blocks != nil {
					state = blocks.get(int(p.Y&15<<8 | p.Z&15<<4 | p.X&15))
				}
				found[p] = registry.BlockOf(state) == registry.BlockLodestone
			}
		}
		return nil
	})
	return found, err
}
