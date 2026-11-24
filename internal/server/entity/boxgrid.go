package entity

import (
	"math"
	"slices"
)

// denseRect is the cell-space bounding rectangle of a dense 2D backing slice,
// laid out in row-major (x, z) order. Zero value is inactive (sizeX == 0).
type denseRect struct {
	minX, minZ   int
	sizeX, sizeZ int
}

// set configures the rectangle for [minX, maxX] x [minZ, maxZ] when its area
// fits within limit, returning the total cell count.
func (r *denseRect) set(minX, maxX, minZ, maxZ, limit int) (int, bool) {
	nx := int64(maxX) - int64(minX) + 1
	nz := int64(maxZ) - int64(minZ) + 1
	// Divide before multiplying: widely scattered coordinates must not overflow.
	if nx > 0 && nz > 0 && nx <= int64(limit) && nz <= int64(limit)/nx {
		r.minX, r.minZ, r.sizeX, r.sizeZ = minX, minZ, int(nx), int(nz)
		return int(nx * nz), true
	}
	return 0, false
}

func (r *denseRect) reset() {
	r.sizeX, r.sizeZ = 0, 0
}

func (r *denseRect) active() bool {
	return r.sizeX > 0
}

// index returns the flat offset of (x, z) in the dense slice, or -1 outside it.
func (r *denseRect) index(x, z int) int {
	dx, dz := x-r.minX, z-r.minZ
	if uint(dx) < uint(r.sizeX) && uint(dz) < uint(r.sizeZ) {
		return dx*r.sizeZ + dz
	}
	return -1
}

// clip narrows [x0, x1] x [z0, z1] to the rectangle's inclusive cell bounds.
func (r *denseRect) clip(x0, x1, z0, z1 int) (loX, hiX, loZ, hiZ int) {
	return max(x0, r.minX), min(x1, r.minX+r.sizeX-1),
		max(z0, r.minZ), min(z1, r.minZ+r.sizeZ-1)
}

// row returns the flat offset of (x, minZ) for a clipped x in bounds.
func (r *denseRect) row(x int) int {
	return (x - r.minX) * r.sizeZ
}

// BoxGrid indexes boxes by the columns they overlap, for one tick. It only
// narrows by cell; the caller still does its exact box test.
type BoxGrid struct {
	// Cell is the columns' side in blocks; 0 is 16. Set it before the first Add.
	Cell  float64
	cells map[[2]int32][]int32
	// dense covers a bounded rectangle set by SetPointBounds; cells holds any
	// overflow outside it (or everything when dense is inactive).
	dense [][]int32
	rect  denseRect
	seen  []int32
	// spare holds emptied cell slices a new cell can take.
	spare [][]int32
}

func (g *BoxGrid) pointCell(x, z float64) (int32, int32) {
	switch g.Cell {
	case 0, 16:
		return int32(math.Floor(x * (1.0 / 16))), int32(math.Floor(z * (1.0 / 16)))
	case 8:
		return int32(math.Floor(x * (1.0 / 8))), int32(math.Floor(z * (1.0 / 8)))
	case 4:
		return int32(math.Floor(x * (1.0 / 4))), int32(math.Floor(z * (1.0 / 4)))
	default:
		return int32(math.Floor(x / g.Cell)), int32(math.Floor(z / g.Cell))
	}
}

// boxCells is the inclusive range of cells box overlaps in x and z.
func (g *BoxGrid) boxCells(box AABB) (x0, x1, z0, z1 int32) {
	x0, z0 = g.pointCell(box.minX, box.minZ)
	x1, z1 = g.pointCell(box.maxX, box.maxZ)
	return x0, x1, z0, z1
}

// Reset clears the cells and keeps their storage.
func (g *BoxGrid) Reset() {
	for k, c := range g.cells {
		g.spare = append(g.spare, c[:0])
		delete(g.cells, k)
	}
	for i := range g.dense {
		g.dense[i] = g.dense[i][:0]
	}
	g.dense = g.dense[:0]
	g.rect.reset()
}

// SetPointBounds enables dense-array backing for points in [minX, maxX] x
// [minZ, maxZ] when the cell rectangle is small relative to n. Call after
// Reset, before adding.
func (g *BoxGrid) SetPointBounds(minX, maxX, minZ, maxZ float64, n int) {
	if n <= 0 {
		return
	}
	cx0, cz0 := g.pointCell(minX, minZ)
	cx1, cz1 := g.pointCell(maxX, maxZ)
	if total, ok := g.rect.set(int(cx0), int(cx1), int(cz0), int(cz1), max(4*n, 64)); ok {
		g.dense = slices.Grow(g.dense[:0], total)[:total]
	}
}

func (g *BoxGrid) addSparse(x, z, i int32) {
	if g.cells == nil {
		g.cells = make(map[[2]int32][]int32)
	}
	k := [2]int32{x, z}
	c, ok := g.cells[k]
	if !ok && len(g.spare) > 0 {
		c = g.spare[len(g.spare)-1]
		g.spare = g.spare[:len(g.spare)-1]
	}
	g.cells[k] = append(c, i)
}

func (g *BoxGrid) addCell(x, z, i int32) {
	if idx := g.rect.index(int(x), int(z)); idx >= 0 {
		g.dense[idx] = append(g.dense[idx], i)
		return
	}
	g.addSparse(x, z, i)
}

func (g *BoxGrid) cell(x, z int32) []int32 {
	if idx := g.rect.index(int(x), int(z)); idx >= 0 {
		return g.dense[idx]
	}
	return g.cells[[2]int32{x, z}]
}

// AddPoint files index i under the single cell containing (x, z). Indices must
// be added in ascending order.
func (g *BoxGrid) AddPoint(i int32, x, z float64) {
	cx, cz := g.pointCell(x, z)
	g.addCell(cx, cz, i)
}

// Add files index i under every cell box overlaps in x and z. Indices must be
// added in ascending order.
func (g *BoxGrid) Add(i int32, box AABB) {
	x0, x1, z0, z1 := g.boxCells(box)
	if !g.rect.active() {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				g.addSparse(x, z, i)
			}
		}
		return
	}
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			g.addCell(x, z, i)
		}
	}
}

// forEachBucket visits each non-empty bucket in [x0, x1] x [z0, z1].
func (g *BoxGrid) forEachBucket(x0, x1, z0, z1 int32, fn func([]int32)) {
	switch {
	case !g.rect.active():
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if c := g.cells[[2]int32{x, z}]; len(c) > 0 {
					fn(c)
				}
			}
		}
	case len(g.cells) == 0:
		loX, hiX, loZ, hiZ := g.rect.clip(int(x0), int(x1), int(z0), int(z1))
		for x := loX; x <= hiX; x++ {
			row := g.rect.row(x)
			for z := loZ; z <= hiZ; z++ {
				if c := g.dense[row+z-g.rect.minZ]; len(c) > 0 {
					fn(c)
				}
			}
		}
	default:
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if c := g.cell(x, z); len(c) > 0 {
					fn(c)
				}
			}
		}
	}
}

// Query yields each index filed under a cell box overlaps, once, in ascending
// order. yield must not call Query.
func (g *BoxGrid) Query(box AABB, yield func(i int32)) {
	x0, x1, z0, z1 := g.boxCells(box)
	if x0 == x1 && z0 == z1 {
		var c []int32
		if !g.rect.active() {
			c = g.cells[[2]int32{x0, z0}]
		} else {
			c = g.cell(x0, z0)
		}
		for _, i := range c {
			yield(i)
		}
		return
	}
	g.seen = g.seen[:0]
	g.forEachBucket(x0, x1, z0, z1, func(c []int32) {
		g.seen = append(g.seen, c...)
	})
	slices.Sort(g.seen)
	g.seen = slices.Compact(g.seen)
	for _, i := range g.seen {
		yield(i)
	}
}

// QueryPoints yields each index filed under a cell box overlaps, for a grid
// whose every box is a point and so lies in one cell: no index repeats,
// and none is sorted. yield must not call Query.
func (g *BoxGrid) QueryPoints(box AABB, yield func(i int32)) {
	x0, x1, z0, z1 := g.boxCells(box)
	g.forEachBucket(x0, x1, z0, z1, func(c []int32) {
		for _, i := range c {
			yield(i)
		}
	})
}

// Union is the smallest box holding both a and b.
func (a AABB) Union(b AABB) AABB {
	return AABB{min(a.minX, b.minX), min(a.minY, b.minY), min(a.minZ, b.minZ), max(a.maxX, b.maxX), max(a.maxY, b.maxY), max(a.maxZ, b.maxZ)}
}
