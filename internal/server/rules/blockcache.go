package rules

import (
	"math"
	"sync"

	"github.com/golem-mc/golem/registry"
)

// CachedBlocks memoises the cells around one explosion for the many
// exposure rays its bodies cast. It reads like the reader it wraps; one
// blast uses it from one goroutine.
type CachedBlocks struct {
	r          BlockReader
	ox, oy, oz int32
	side       int32
	// states holds state+1 for a cell read, 0 for one not read yet.
	states []uint32
}

var cachedBlocksPool = sync.Pool{New: func() any { return new(CachedBlocks) }}

// NewCachedBlocks caches the cells within reach blocks of (cx, cy, cz).
// Release it when the blast is done.
func NewCachedBlocks(r BlockReader, cx, cy, cz, reach float64) *CachedBlocks {
	c := cachedBlocksPool.Get().(*CachedBlocks)
	half := int32(math.Ceil(reach)) + 2
	c.r, c.side = r, 2*half+1
	c.ox, c.oy, c.oz = int32(math.Floor(cx))-half, int32(math.Floor(cy))-half, int32(math.Floor(cz))-half
	n := int(c.side * c.side * c.side)
	if cap(c.states) < n {
		c.states = make([]uint32, n)
	} else {
		c.states = c.states[:n]
		clear(c.states)
	}
	return c
}

// Release returns the cache for reuse; it must not be read afterwards.
func (c *CachedBlocks) Release() {
	c.r = nil
	cachedBlocksPool.Put(c)
}

// BlockAt reads a cell, from memory when it is read before.
func (c *CachedBlocks) BlockAt(x, y, z int32) uint16 {
	dx, dy, dz := x-c.ox, y-c.oy, z-c.oz
	if uint32(dx) >= uint32(c.side) || uint32(dy) >= uint32(c.side) || uint32(dz) >= uint32(c.side) {
		return c.r.BlockAt(x, y, z)
	}
	i := (dy*c.side+dz)*c.side + dx
	if v := c.states[i]; v != 0 {
		return uint16(v - 1)
	}
	s := c.r.BlockAt(x, y, z)
	c.states[i] = uint32(s) + 1
	return s
}

// PrefilledBlocks holds a read-only snapshot of every cell in a box, safe to
// read from several goroutines during one explosion.
type PrefilledBlocks struct {
	ox, oy, oz int32
	sx, sy, sz int32
	states     []uint16
	solid      []bool
}

var prefilledBlocksPool = sync.Pool{New: func() any { return new(PrefilledBlocks) }}

// NewPrefilledBlocks reads every cell in [minX..maxX] x [minY..maxY] x [minZ..maxZ]
// from r into a pooled read-only grid. Release it when the blast is done.
func NewPrefilledBlocks(r BlockReader, minX, minY, minZ, maxX, maxY, maxZ int32) *PrefilledBlocks {
	p := prefilledBlocksPool.Get().(*PrefilledBlocks)
	sx, sy, sz := maxX-minX+1, maxY-minY+1, maxZ-minZ+1
	p.ox, p.oy, p.oz = minX, minY, minZ
	p.sx, p.sy, p.sz = sx, sy, sz
	n := int(sx) * int(sy) * int(sz)
	if cap(p.states) < n {
		p.states = make([]uint16, n)
		p.solid = make([]bool, n)
	} else {
		p.states = p.states[:n]
		p.solid = p.solid[:n]
	}
	i := 0
	for dy := range sy {
		y := minY + dy
		for dz := range sz {
			z := minZ + dz
			for dx := range sx {
				state := r.BlockAt(minX+dx, y, z)
				p.states[i] = state
				p.solid[i] = sightSolid(state)
				i++
			}
		}
	}
	return p
}

// Release returns the grid for reuse; it must not be read afterwards.
func (p *PrefilledBlocks) Release() {
	if cap(p.states) <= 1<<18 {
		prefilledBlocksPool.Put(p)
	}
}

// BlockAt reads a cell from the prefilled box, returning air outside it.
func (p *PrefilledBlocks) BlockAt(x, y, z int32) uint16 {
	dx, dy, dz := x-p.ox, y-p.oy, z-p.oz
	if uint32(dx) >= uint32(p.sx) || uint32(dy) >= uint32(p.sy) || uint32(dz) >= uint32(p.sz) {
		return 0
	}
	return p.states[(dy*p.sz+dz)*p.sx+dx]
}

func (p *PrefilledBlocks) solidAt(x, y, z int32) bool {
	dx, dy, dz := x-p.ox, y-p.oy, z-p.oz
	if uint32(dx) >= uint32(p.sx) || uint32(dy) >= uint32(p.sy) || uint32(dz) >= uint32(p.sz) {
		return false
	}
	return p.solid[(dy*p.sz+dz)*p.sx+dx]
}

// sightSolid reports whether state blocks an explosion exposure ray.
func sightSolid(state uint16) bool {
	if state == 0 {
		return false
	}
	b := registry.BlockOf(state)
	return registry.CollisionTop(state) >= 0.5 && b != registry.BlockWater && b != registry.BlockLava
}
