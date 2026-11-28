package world

import (
	"fmt"
	"sync"
	"time"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// lodestoneCacheMax bounds the unloaded-destination answers kept, and the
// chunks whose read failed.
const lodestoneCacheMax = 1024

// lodestoneRetryDelay is how long a chunk whose read failed is not read again
// unless it is loaded or written, so a compass aimed into it does not read the
// disk every tick.
const lodestoneRetryDelay = 5 * time.Second

// lodestoneCache remembers whether a saved chunk holds a lodestone at a
// position, per chunk so a load or write of that chunk drops its answers.
type lodestoneCache struct {
	mu      sync.Mutex
	chunks  map[ChunkPos]map[posKey]bool
	entries int
	// pending is the chunks with a read in flight; drop bumps the version a
	// read started under, so its stale answer is not published.
	pending map[ChunkPos]*lodestoneRead
	// retry is when a chunk whose read failed may be read again.
	retry map[ChunkPos]time.Time
	now   func() time.Time // time.Now when nil
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

// begin registers a read of the chunk; end must follow, and the returned
// version tells valid and publish whether a load or write came between.
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

// publish keeps the answers of a read begun at version unless the chunk was
// loaded or written since, reporting whether it did. A full cache gives up one
// other answer rather than all of them.
func (c *lodestoneCache) publish(pos ChunkPos, version uint64, answers map[posKey]bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending[pos].version != version {
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

// fail holds off rereading a chunk whose read begun at version failed, unless
// the chunk was loaded or written since. A full set gives up an expired or
// arbitrary chunk.
func (c *lodestoneCache) fail(pos ChunkPos, version uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending[pos].version != version {
		return
	}
	now := c.clock()
	if c.retry == nil {
		c.retry = map[ChunkPos]time.Time{}
	}
	if _, dup := c.retry[pos]; !dup && len(c.retry) >= lodestoneCacheMax {
		victim := pos
		for p, at := range c.retry {
			victim = p
			if !now.Before(at) {
				break
			}
		}
		delete(c.retry, victim)
	}
	c.retry[pos] = now.Add(lodestoneRetryDelay)
}

// holding reports whether a failed read of the chunk is not to be repeated yet.
func (c *lodestoneCache) holding(pos ChunkPos) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	at, ok := c.retry[pos]
	if ok && !c.clock().Before(at) {
		delete(c.retry, pos)
		return false
	}
	return ok
}

func (c *lodestoneCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// valid reports whether the chunk is unloaded and unwritten since the read
// begun at version.
func (c *lodestoneCache) valid(pos ChunkPos, version uint64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pending[pos].version == version
}

// end releases a read begun by begin.
func (c *lodestoneCache) end(pos ChunkPos) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if r := c.pending[pos]; r != nil {
		if r.readers--; r.readers == 0 {
			delete(c.pending, pos)
		}
	}
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
	delete(c.retry, pos)
	if m, ok := c.chunks[pos]; ok {
		c.entries -= len(m)
		delete(c.chunks, pos)
	}
}

// LodestoneCached reports whether a lodestone stands at the position when that
// is known without reading the disk: the live block in a loaded chunk, a kept
// answer for a saved one. known is false otherwise; ResolveLodestone finds out.
func (w *World) LodestoneCached(x, y, z int32) (present, known bool) {
	pos := ChunkPos{X: x >> 4, Z: z >> 4}
	if chunk := w.sharedChunkAt(pos); chunk != nil {
		return registry.BlockOf(chunk.sharedBlockAt(x&15, y, z&15)) == registry.BlockLodestone, true
	}
	return w.lodestones.get(pos, posKey{x, y, z})
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

// LodestoneAnswers are what ResolveLodestones found out. An answer holds only
// while no load or write of its chunk came after it was read; Lookup checks
// that at each use, so Close the answers when done.
type LodestoneAnswers struct {
	w        *World
	found    map[LodestonePos]bool
	versions map[ChunkPos]uint64
}

// Lookup reports whether a lodestone stands at p when the read that found out
// is still current; known is false otherwise. A nil receiver knows nothing.
func (a *LodestoneAnswers) Lookup(p LodestonePos) (present, known bool) {
	if a == nil {
		return false, false
	}
	if present, known = a.found[p]; !known {
		return false, false
	}
	pos := ChunkPos{X: p.X >> 4, Z: p.Z >> 4}
	if !a.w.lodestones.valid(pos, a.versions[pos]) {
		return false, false
	}
	return present, true
}

// Close ends the answers' hold on their chunks' versions.
func (a *LodestoneAnswers) Close() {
	if a == nil {
		return
	}
	for pos := range a.versions {
		a.w.lodestones.end(pos)
	}
}

// ResolveLodestones reads the saved chunks behind positions
// LodestoneCached did not know, one read per chunk, and keeps the answers in
// the cache. A chunk never saved has an answer of absent without being
// generated or loaded, and one that could not be read has none, nor is read
// again for lodestoneRetryDelay unless it is loaded or written. It reads the
// disk, so no lock a tick holds may be held.
// vanilla: PoiManager.existsAtPosition
func (w *World) ResolveLodestones(positions []LodestonePos) *LodestoneAnswers {
	a := &LodestoneAnswers{w: w, found: make(map[LodestonePos]bool, len(positions)), versions: map[ChunkPos]uint64{}}
	var cold map[ChunkPos][]LodestonePos
	for _, p := range positions {
		pos := ChunkPos{X: p.X >> 4, Z: p.Z >> 4}
		if w.sharedChunkAt(pos) != nil {
			continue // the live block answers
		}
		if _, held := a.versions[pos]; !held {
			a.versions[pos] = w.lodestones.begin(pos)
		}
		if present, known := w.lodestones.get(pos, posKey{p.X, p.Y, p.Z}); known {
			a.found[p] = present
			continue
		}
		if cold == nil {
			cold = map[ChunkPos][]LodestonePos{}
		}
		cold[pos] = append(cold[pos], p)
	}
	for pos, ps := range cold {
		if w.lodestones.holding(pos) {
			continue
		}
		found, err := w.savedLodestones(pos, ps)
		if err != nil {
			w.lodestones.fail(pos, a.versions[pos])
			continue
		}
		published := make(map[posKey]bool, len(found))
		for p, f := range found {
			published[posKey{p.X, p.Y, p.Z}] = f
			a.found[p] = f
		}
		w.lodestones.publish(pos, a.versions[pos], published)
	}
	return a
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
			sy, _ := nbt.ByteField(sec, "Y")
			var asked []LodestonePos
			for _, p := range ps {
				if p.Y>>4 == int32(sy) {
					asked = append(asked, p)
				}
			}
			if len(asked) == 0 {
				continue
			}
			cells := make([]int, len(asked))
			for i, p := range asked {
				cells[i] = int(p.Y&15<<8 | p.Z&15<<4 | p.X&15)
			}
			states := make([]uint16, len(asked))
			if bs, ok := sectionBlockStates(sec, dataVersion); ok {
				if err := blockStatesAt(bs, cells, states); err != nil {
					return fmt.Errorf("section %d: %w", int32(sy), err)
				}
			}
			for i, p := range asked {
				found[p] = registry.BlockOf(states[i]) == registry.BlockLodestone
			}
		}
		return nil
	})
	return found, err
}
