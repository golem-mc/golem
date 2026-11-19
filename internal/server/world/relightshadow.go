package world

import (
	"sync"
)

// relightTries is how often relightArea computes off the lock before giving up
// on a region that keeps changing and relighting under the write lock.
const relightTries = 3

// lightShadow is a view of the chunks a relight touches, blocks and light
// only, for the relight to run on without a world lock. Each view borrows its
// live chunk's arrays: blocks are read with atomic loads (sharedBlockAt) and
// light is copied before its first write (writableLight), so the live chunks
// are never written. Its light is installed if nothing viewed changed
// meanwhile, which also means every block it read was the block stamped.
type lightShadow struct {
	w *World // the shadow world, holding the views in its chunk map
	chunkStamps
	// views are the shadow's chunks; past len, the spares an earlier relight
	// left in lightShadows.
	views []*loadedChunk
}

// lightShadows recycles shadows with their world and views: a shadow world is
// a whole World struct, and a relight builds one with a view per chunk.
var lightShadows = sync.Pool{New: func() any {
	return &lightShadow{w: &World{chunks: make(map[ChunkPos]*loadedChunk)}}
}}

// chunkStamps records a chunk range as off-lock work found it: orig holds each
// loaded chunk (nil where none) with its block and light write counts. The work
// may be installed only if the same unchanged chunks still stand everywhere.
type chunkStamps struct {
	minCx, minCz, maxCx, maxCz int32
	orig                       []*loadedChunk
	writes                     []uint64
	lightGen                   []uint64
}

// stamp records the range's chunks, reusing s's slices. The caller holds w.mu,
// read or write.
func (s *chunkStamps) stamp(w *World, minCx, minCz, maxCx, maxCz int32) {
	s.minCx, s.minCz, s.maxCx, s.maxCz = minCx, minCz, maxCx, maxCz
	s.orig, s.writes, s.lightGen = s.orig[:0], s.writes[:0], s.lightGen[:0]
	for cx := minCx; cx <= maxCx; cx++ {
		for cz := minCz; cz <= maxCz; cz++ {
			loaded := w.chunks[ChunkPos{cx, cz}]
			var writes, gen uint64
			if loaded != nil {
				writes, gen = loaded.chunk.writes, loaded.chunk.lightGen
			}
			s.orig = append(s.orig, loaded)
			s.writes = append(s.writes, writes)
			s.lightGen = append(s.lightGen, gen)
		}
	}
}

// newLightShadow views every loaded chunk overlapping the box. The caller
// holds w.mu, read or write.
func (w *World) newLightShadow(minX, minZ, maxX, maxZ int32) *lightShadow {
	s := lightShadows.Get().(*lightShadow)
	s.stamp(w, floorDiv(minX, sectionSize), floorDiv(minZ, sectionSize),
		floorDiv(maxX, sectionSize), floorDiv(maxZ, sectionSize))
	for _, loaded := range s.orig {
		if loaded == nil {
			continue
		}
		n := len(s.views)
		if n < cap(s.views) {
			s.views = s.views[:n+1]
		} else {
			s.views = append(s.views, nil)
		}
		if s.views[n] == nil {
			s.views[n] = &loadedChunk{chunk: new(Chunk)}
		}
		v := s.views[n]
		v.chunk.viewOf(loaded.chunk)
		s.w.chunks[ChunkPos{v.chunk.x, v.chunk.z}] = v
	}
	return s
}

// viewOf makes c a shadow's view of live: the same block and light arrays,
// light marked borrowed, and what else the light engine reads.
func (c *Chunk) viewOf(live *Chunk) {
	c.x, c.z = live.x, live.z
	c.skyLight, c.blockLight = live.skyLight, live.blockLight
	c.lightBorrowed = [2]uint32{allLightSections, allLightSections}
	live.lightFormsMu.Lock()
	c.lightEncoded, c.chunkLightEncoded, c.lightFormsStale = live.lightEncoded, live.chunkLightEncoded, live.lightFormsStale
	live.lightFormsMu.Unlock()
	c.lightCorrect = live.lightCorrect
	for i := range live.sections {
		// A section gaining its array after this keeps reading fill here,
		// the stamped block: the write that grew it fails current.
		c.sections[i].fill = live.sections[i].fill
		c.sections[i].emitting = live.sections[i].emitting
		c.sections[i].spawn.Store(nil)
		c.sections[i].blocks.Store(live.sections[i].blocks.Load())
	}
}

// release returns the shadow to lightShadows, and with it the light arrays it
// copied unless they were installed; the shadow is not used again.
func (s *lightShadow) release(installed bool) {
	for _, v := range s.views {
		c := v.chunk
		if !installed {
			for layer := range 2 {
				for si, a := range c.lightLayer(layer) {
					if a != nil && c.lightBorrowed[layer]&(1<<si) == 0 {
						lightArrays.Put(a)
					}
				}
			}
		}
		// Dropped so the pool keeps no chunk's arrays alive.
		c.skyLight, c.blockLight = [lightSections]*[lightArrayLen]byte{}, [lightSections]*[lightArrayLen]byte{}
		c.lightEncoded, c.chunkLightEncoded, c.lightFormsStale = nil, nil, false
		for i := range c.sections {
			c.sections[i].spawn.Store(nil)
			c.sections[i].blocks.Store(nil)
		}
	}
	s.views = s.views[:0]
	clear(s.w.chunks)
	clear(s.orig)
	lightShadows.Put(s)
}

// current reports whether every position still holds its stamped chunk with no
// block or light written since. The caller holds w.mu.
func (s *chunkStamps) current(w *World) bool {
	i := 0
	for cx := s.minCx; cx <= s.maxCx; cx++ {
		for cz := s.minCz; cz <= s.maxCz; cz++ {
			loaded := w.chunks[ChunkPos{cx, cz}]
			if loaded != s.orig[i] {
				return false
			}
			if loaded != nil && (loaded.chunk.writes != s.writes[i] || loaded.chunk.lightGen != s.lightGen[i]) {
				return false
			}
			i++
		}
	}
	return true
}

// install moves the views' light into the real chunks, before release(true).
// The caller holds w.mu for writing and has checked current, or has held it
// since the shadow was taken. It returns the light sections whose contents
// differ from the live chunks', in the order of s.orig.
func (s *lightShadow) install() []LightChange {
	var changes []LightChange
	for _, loaded := range s.orig {
		if loaded == nil {
			continue
		}
		c := loaded.chunk
		cp := s.w.chunks[ChunkPos{c.x, c.z}].chunk
		ch := LightChange{
			Pos:   ChunkPos{c.x, c.z},
			Sky:   changedLightSections(&c.skyLight, &cp.skyLight),
			Block: changedLightSections(&c.blockLight, &cp.blockLight),
		}
		if ch.Sky|ch.Block != 0 {
			changes = append(changes, ch)
			// A light change reaches disk even with no block write.
			// vanilla: ChunkHolder.sectionLightChanged
			loaded.markDirty()
		}
		c.skyLight, c.blockLight = cp.skyLight, cp.blockLight
		c.lightBorrowed = cp.lightBorrowed
		c.lightEncoded, c.chunkLightEncoded, c.lightFormsStale = cp.lightEncoded, cp.chunkLightEncoded, cp.lightFormsStale
		if !c.lightFormsRetained {
			c.lightFormsStale = c.lightFormsStale || c.lightEncoded != nil
			c.lightEncoded, c.chunkLightEncoded = nil, nil
		}
		c.lightCorrect = cp.lightCorrect
		c.lightGen++
	}
	return changes
}

// changedLightSections is the mask of light sections whose contents differ
// between was and now; nil reads as dark. A shared pointer is unchanged.
func changedLightSections(was, now *[lightSections]*[lightArrayLen]byte) uint32 {
	var mask uint32
	for si := range lightSections {
		a, b := was[si], now[si]
		if a == b {
			continue
		}
		if a == nil {
			a = &darkSection
		}
		if b == nil {
			b = &darkSection
		}
		if *a != *b {
			mask |= 1 << si
		}
	}
	return mask
}

// lightJoin is a new chunk's border crossing computed under the read lock
// before it is in the map: copies of the sections it raised and the encodings
// of the chunks it raised.
type lightJoin struct {
	chunkStamps
	touched []*Chunk
	over    []*lightOverlay
	changed []ChunkPos
	// lights is the sections it raised in loaded chunks, for Update Light.
	lights []LightChange
}

// newLightJoin joins c, not yet in the map, to the chunks around pos with every
// write going to copies, leaving the world unchanged. The caller holds w.mu,
// read or write, and pos is not loaded.
func (w *World) newLightJoin(pos ChunkPos, c *Chunk) *lightJoin {
	// Border light spreads at most 15 blocks, so only into the 3x3 around pos.
	j := &lightJoin{}
	j.stamp(w, pos.X-1, pos.Z-1, pos.X+1, pos.Z+1)
	j.touched = []*Chunk{c}
	ctx := &lightCtx{w: w, extra: c, touched: &j.touched, cow: make(map[*Chunk]*lightOverlay, 9)}
	ctx.joinBorders(pos)
	for i, t := range j.touched {
		o := ctx.overlay(t)
		sky := changedLightSections(&t.skyLight, &o.layers[skyLayer])
		block := changedLightSections(&t.blockLight, &o.layers[blockLayer])
		if !t.hasLightForms() || sky != 0 || block != 0 {
			j.changed = append(j.changed, ChunkPos{t.x, t.z})
		}
		if i > 0 && sky|block != 0 {
			j.lights = append(j.lights, LightChange{Pos: ChunkPos{t.x, t.z}, Sky: sky, Block: block})
		}
		j.over = append(j.over, o)
	}
	return j
}

// install moves the join's light into the chunks it raised, the new one
// included; the loaded chunks it changed are marked for saving and for Update
// Light, the new one is left to putChunk. The caller holds w.mu for writing and has checked current.
func (j *lightJoin) install(w *World) {
	for i, c := range j.touched {
		o := j.over[i]
		// A new chunk lit by lightAlone has no encoded form yet; one loaded with
		// its light (lightCorrect) has the stale mark from its load.
		if (i > 0 || c.lightCorrect) &&
			changedLightSections(&c.skyLight, &o.layers[skyLayer])|changedLightSections(&c.blockLight, &o.layers[blockLayer]) == 0 {
			continue
		}
		c.skyLight, c.blockLight = o.layers[skyLayer], o.layers[blockLayer]
		c.lightBorrowed = o.borrowed
		c.lightFormsStale = true
		c.lightGen++
	}
	j.touched[0].lightCorrect = true
	for _, pos := range j.changed {
		if l := w.chunks[pos]; l != nil && l.chunk != j.touched[0] {
			l.markDirty()
		}
	}
	// vanilla: ChunkHolder.sectionLightChanged
	for _, ch := range j.lights {
		w.lightChanged.Mark(ch)
	}
}
