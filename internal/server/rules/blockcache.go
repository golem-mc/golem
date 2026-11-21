package rules

import (
	"math"
	"sync"
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
