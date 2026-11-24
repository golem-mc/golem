package entity

import "sync"

// pathScratch is the working memory of one search, reset between searches.
type pathScratch struct {
	walked   map[Waypoint]float64
	cameFrom map[Waypoint]Waypoint
	maluses  map[Waypoint]float64
	cells    []pathCell
	origin   Waypoint
	side     int32
	epoch    uint32
	open     frontier
	blocks   blockCache
	// tiny scratches belong to searches that start next to their goal: their
	// maps are small enough to clear in nanoseconds and they read the world
	// directly, as a section read costs more than the few cells they touch.
	tiny bool
}

// pathCell keeps independent stamps because a malus may be read before a
// node is opened, and the start is opened before its malus is read.
type pathCell struct {
	// Most rejected candidates touch only geometry; keep its fields together.
	floor                   float64
	geometryEpoch           uint32
	geometryFlags           uint8
	walked, malus           float64
	parent                  Waypoint
	walkedEpoch, malusEpoch uint32
}

const (
	floorKnown uint8 = 1 << iota
	traversableKnown
	traversableValue
	columnOpenKnown
	columnOpenValue
)

// geometry initializes only geometry state; opening a node and pricing it
// have independent lifetimes within the search.
func (s *pathScratch) geometry(c Waypoint) *pathCell {
	// Unsigned offsets also reject coordinates below the box's origin.
	x, y, z := uint32(c.X-s.origin.X), uint32(c.Y-s.origin.Y), uint32(c.Z-s.origin.Z)
	side := uint32(s.side)
	if max(x, y, z) >= side {
		return nil
	}
	n := &s.cells[(x*side+y)*side+z]
	n.prepareGeometry(s.epoch)
	return n
}

// geometryInside is used only after the search's range guard.
func (s *pathScratch) geometryInside(c Waypoint) *pathCell {
	n := s.cell(c)
	n.prepareGeometry(s.epoch)
	return n
}

func (n *pathCell) prepareGeometry(epoch uint32) {
	if n.geometryEpoch != epoch {
		n.geometryFlags, n.geometryEpoch = 0, epoch
	}
}

// prepare uses dense storage only for a box no larger than 33 cubed.
func (s *pathScratch) prepare(from Waypoint, maxRange int32) {
	s.side = 0
	if maxRange < 0 || maxRange > 16 {
		return
	}
	s.side = 2*maxRange + 1
	s.origin = Waypoint{from.X - maxRange, from.Y - maxRange, from.Z - maxRange}
	n := int(s.side * s.side * s.side)
	if len(s.cells) < n {
		s.cells = make([]pathCell, n)
	}
	s.epoch++
	if s.epoch == 0 {
		clear(s.cells)
		s.epoch = 1
	}
}

// cell indexes only nodes within the search's Chebyshev bound.
func (s *pathScratch) cell(c Waypoint) *pathCell {
	return &s.cells[((c.X-s.origin.X)*s.side+c.Y-s.origin.Y)*s.side+c.Z-s.origin.Z]
}

func (s *pathScratch) cost(c Waypoint) (float64, bool) {
	if s.side == 0 {
		v, ok := s.walked[c]
		return v, ok
	}
	n := s.cell(c)
	return n.walked, n.walkedEpoch == s.epoch
}

// visit records a node only when this route improves its cost.
func (s *pathScratch) visit(c, parent Waypoint, cost float64) bool {
	if s.side == 0 {
		if old, ok := s.walked[c]; ok && old <= cost {
			return false
		}
		s.walked[c], s.cameFrom[c] = cost, parent
		return true
	}
	n := s.cell(c)
	if n.walkedEpoch == s.epoch && n.walked <= cost {
		return false
	}
	n.walked, n.parent, n.walkedEpoch = cost, parent, s.epoch
	return true
}

func (s *pathScratch) malus(w World, f *footprint, c Waypoint) float64 {
	if s.side == 0 {
		if v, ok := s.maluses[c]; ok {
			return v
		}
		v := cellMalus(w, *f, c.X, c.Y, c.Z)
		s.maluses[c] = v
		return v
	}
	n := s.cell(c)
	if n.malusEpoch != s.epoch {
		n.malus = cellMalus(w, *f, c.X, c.Y, c.Z)
		n.malusEpoch = s.epoch
	}
	return n.malus
}

// Map room per scratch kind, in cells. A tiny scratch is dropped once a
// search outgrows it, and a large one past scratchKeep, so one huge search
// does not keep its oversized tables alive.
const (
	// scratchHint is what the cells opened at the smallest range (sixteen per
	// block, at least 16 blocks) reach on open ground.
	scratchHint = 1024
	tinyHint    = 64
	scratchKeep = 16 * scratchHint
	// tinyReach is the largest manhattan distance from start to goal that
	// takes a tiny scratch; measured on open ground, the section cache pays
	// back from about eight blocks.
	tinyReach = 4
)

var (
	pathScratchPool = sync.Pool{New: func() any { return newPathScratch(false) }}
	tinyScratchPool = sync.Pool{New: func() any { return newPathScratch(true) }}
)

// MobPathScratch keeps the path-search buffers alive between a herd's ticks.
// Its zero value is ready to use, and the herd uses it serially.
type MobPathScratch struct {
	normal, tiny *pathScratch
	swim         *swimScratch
}

func (p *MobPathScratch) findPath(w World, f footprint, from, to Waypoint, maxRange, reachRange int32) ([]Waypoint, bool) {
	distance := manhattan(from, to)
	if p == nil {
		s := getPathScratch(distance)
		defer s.release()
		return s.search(w, f, from, to, maxRange, reachRange)
	}
	slot := &p.normal
	tiny := distance <= tinyReach
	if tiny {
		slot = &p.tiny
	}
	if *slot == nil {
		*slot = newPathScratch(tiny)
	}
	s := *slot
	defer func() {
		if !s.fits() {
			// A large sparse search can grow the maps without using the dense cells.
			fresh := newPathScratch(s.tiny)
			fresh.cells, fresh.epoch = s.cells, s.epoch
			*slot = fresh
			return
		}
		s.reset()
	}()
	return s.search(w, f, from, to, maxRange, reachRange)
}

func newPathScratch(tiny bool) *pathScratch {
	hint := scratchHint
	if tiny {
		hint = tinyHint
	}
	return &pathScratch{
		walked:   make(map[Waypoint]float64, hint),
		cameFrom: make(map[Waypoint]Waypoint, hint),
		maluses:  make(map[Waypoint]float64, hint),
		blocks:   blockCache{sections: make(map[cacheKey]*cacheSection)},
		tiny:     tiny,
	}
}

// getPathScratch returns an empty scratch for a search of the given manhattan
// distance.
func getPathScratch(distance int32) *pathScratch {
	if distance <= tinyReach {
		return tinyScratchPool.Get().(*pathScratch)
	}
	return pathScratchPool.Get().(*pathScratch)
}

// reset empties maps, heap and block cache; prepare invalidates dense cells.
func (s *pathScratch) reset() {
	clear(s.walked)
	clear(s.cameFrom)
	clear(s.maluses)
	s.open = s.open[:0]
	s.blocks.reset()
}

// fits reports whether the search left the scratch small enough to reuse.
func (s *pathScratch) fits() bool {
	grown := max(len(s.walked), len(s.maluses))
	if s.tiny {
		return grown <= tinyHint
	}
	return grown <= scratchKeep
}

// release resets the scratch and returns it to its pool, unless the search
// grew its maps past what the pool keeps.
func (s *pathScratch) release() {
	keep := s.fits()
	s.reset()
	switch {
	case !keep:
	case s.tiny:
		tinyScratchPool.Put(s)
	default:
		pathScratchPool.Put(s)
	}
}

// A cache section is an eight-cell cube of blocks, filled in one BlocksIn
// read the first time a search touches it.
const (
	cacheShift = 3
	cacheSide  = 1 << cacheShift
	cacheMask  = cacheSide - 1
	cacheCells = cacheSide * cacheSide * cacheSide
)

// cacheKey packs a section's coordinates, 21 bits each, so the map hashes a
// single word.
type cacheKey uint64

func sectionKey(x, y, z int32) cacheKey {
	const m = 1<<21 - 1
	return cacheKey(uint64(x>>cacheShift)&m | uint64(y>>cacheShift)&m<<21 | uint64(z>>cacheShift)&m<<42)
}

// sectionWay takes the same low bits as key ^ key>>21 ^ key>>42,
// keeping the way lookup independent of key packing.
func sectionWay(x, y, z int32) int32 {
	return ((x ^ y ^ z) >> cacheShift) & (cacheWays - 1)
}

type cacheSection [cacheCells]uint16

// cacheWays is the size of the direct-mapped table in front of the section
// map; consecutive reads of a search alternate among a few nearby sections.
const cacheWays = 16

type cacheWay struct {
	key     cacheKey
	section *cacheSection
}

// blockCache is a World whose block reads are remembered for the length of
// one search, which does not change the world. It reads each touched section
// of the area in one batch instead of cell by cell.
type blockCache struct {
	World
	sections map[cacheKey]*cacheSection
	ways     [cacheWays]cacheWay
	free     []*cacheSection
	buf      []uint16
}

// BlockAt is the world's block at the cell, as it was when the section was
// first read.
func (c *blockCache) BlockAt(x, y, z int32) uint16 {
	key := sectionKey(x, y, z)
	way := &c.ways[sectionWay(x, y, z)]
	section := way.section
	if section == nil || way.key != key {
		section = c.section(key, x, y, z)
		way.key, way.section = key, section
	}
	return section[(x&cacheMask)<<(2*cacheShift)|(y&cacheMask)<<cacheShift|z&cacheMask]
}

func (c *blockCache) section(key cacheKey, x, y, z int32) *cacheSection {
	if s, ok := c.sections[key]; ok {
		return s
	}
	var s *cacheSection
	if n := len(c.free); n > 0 {
		s, c.free = c.free[n-1], c.free[:n-1]
	} else {
		s = new(cacheSection)
	}
	x0, y0, z0 := x&^cacheMask, y&^cacheMask, z&^cacheMask
	// BlocksIn lays the box out x, then y, then z, which is the section's index.
	c.buf = c.BlocksIn(x0, y0, z0, x0+cacheMask, y0+cacheMask, z0+cacheMask, c.buf)
	copy(s[:], c.buf)
	c.sections[key] = s
	return s
}

// reset forgets the sections, keeping their memory for the next search.
func (c *blockCache) reset() {
	for _, s := range c.sections {
		c.free = append(c.free, s)
	}
	clear(c.sections)
	c.ways = [cacheWays]cacheWay{}
	c.World = nil
}

// push adds a cell to the heap.
func (f *frontier) push(c openCell) {
	*f = append(*f, c)
	h := *f
	i := len(h) - 1
	for i > 0 {
		parent := (i - 1) / 2
		if !h.Less(i, parent) {
			break
		}
		h.Swap(i, parent)
		i = parent
	}
}

// pop removes and returns the cheapest cell.
func (f *frontier) pop() openCell {
	h := *f
	top, n := h[0], len(h)-1
	h[0] = h[n]
	h = h[:n]
	for i := 0; ; {
		child := 2*i + 1
		if child >= n {
			break
		}
		if child+1 < n && h.Less(child+1, child) {
			child++
		}
		if !h.Less(child, i) {
			break
		}
		h.Swap(i, child)
		i = child
	}
	*f = h
	return top
}
