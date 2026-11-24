package entity

import (
	"math"
	"slices"
)

// BoxGrid indexes boxes by the columns they overlap, for one tick. It only
// narrows by cell; the caller still does its exact box test.
type BoxGrid struct {
	// Cell is the columns' side in blocks; 0 is 16. Set it before the first Add.
	Cell  float64
	cells map[[2]int32][]int32
	// dense covers a bounded rectangle set by SetPointBounds; cells holds any
	// overflow outside it (or everything when dense is inactive).
	dense                    [][]int32
	minX, minZ, sizeX, sizeZ int32
	seen                     []int32
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
	g.sizeX, g.sizeZ = 0, 0
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
	nx := int64(cx1) - int64(cx0) + 1
	nz := int64(cz1) - int64(cz0) + 1
	limit := int64(max(4*n, 64))
	// Divide before multiplying: widely scattered coordinates must not overflow.
	if nx > 0 && nz > 0 && nx <= limit && nz <= limit/nx {
		g.minX, g.minZ, g.sizeX, g.sizeZ = cx0, cz0, int32(nx), int32(nz)
		total := int(nx * nz)
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

func (g *BoxGrid) cell(x, z int32) []int32 {
	if dx, dz := uint64(int64(x)-int64(g.minX)), uint64(int64(z)-int64(g.minZ)); dx < uint64(g.sizeX) && dz < uint64(g.sizeZ) {
		return g.dense[int(dx)*int(g.sizeZ)+int(dz)]
	}
	return g.cells[[2]int32{x, z}]
}

// AddPoint files index i under the single cell containing (x, z). Indices must
// be added in ascending order.
func (g *BoxGrid) AddPoint(i int32, x, z float64) {
	cx, cz := g.pointCell(x, z)
	if dx, dz := uint64(int64(cx)-int64(g.minX)), uint64(int64(cz)-int64(g.minZ)); dx < uint64(g.sizeX) && dz < uint64(g.sizeZ) {
		idx := int(dx)*int(g.sizeZ) + int(dz)
		g.dense[idx] = append(g.dense[idx], i)
		return
	}
	g.addSparse(cx, cz, i)
}

// Add files index i under every cell box overlaps in x and z. Indices must be
// added in ascending order.
func (g *BoxGrid) Add(i int32, box AABB) {
	x0, x1, z0, z1 := g.boxCells(box)
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			if dx, dz := uint64(int64(x)-int64(g.minX)), uint64(int64(z)-int64(g.minZ)); dx < uint64(g.sizeX) && dz < uint64(g.sizeZ) {
				idx := int(dx)*int(g.sizeZ) + int(dz)
				g.dense[idx] = append(g.dense[idx], i)
			} else {
				g.addSparse(x, z, i)
			}
		}
	}
}

// Query yields each index filed under a cell box overlaps, once, in ascending
// order. yield must not call Query.
func (g *BoxGrid) Query(box AABB, yield func(i int32)) {
	x0, x1, z0, z1 := g.boxCells(box)
	if x0 == x1 && z0 == z1 {
		for _, i := range g.cell(x0, z0) {
			yield(i)
		}
		return
	}
	g.seen = g.seen[:0]
	if g.sizeX > 0 && len(g.cells) == 0 {
		loX := max(x0, g.minX)
		hiX := min(x1, g.minX+g.sizeX-1)
		loZ := max(z0, g.minZ)
		hiZ := min(z1, g.minZ+g.sizeZ-1)
		for x := loX; x <= hiX; x++ {
			row := int(x-g.minX) * int(g.sizeZ)
			for z := loZ; z <= hiZ; z++ {
				g.seen = append(g.seen, g.dense[row+int(z-g.minZ)]...)
			}
		}
	} else {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				g.seen = append(g.seen, g.cell(x, z)...)
			}
		}
	}
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
	if g.sizeX > 0 && len(g.cells) == 0 {
		loX := max(x0, g.minX)
		hiX := min(x1, g.minX+g.sizeX-1)
		loZ := max(z0, g.minZ)
		hiZ := min(z1, g.minZ+g.sizeZ-1)
		for x := loX; x <= hiX; x++ {
			row := int(x-g.minX) * int(g.sizeZ)
			for z := loZ; z <= hiZ; z++ {
				for _, i := range g.dense[row+int(z-g.minZ)] {
					yield(i)
				}
			}
		}
		return
	}
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			for _, i := range g.cell(x, z) {
				yield(i)
			}
		}
	}
}

// Union is the smallest box holding both a and b.
func (a AABB) Union(b AABB) AABB {
	return AABB{min(a.minX, b.minX), min(a.minY, b.minY), min(a.minZ, b.minZ), max(a.maxX, b.maxX), max(a.maxY, b.maxY), max(a.maxZ, b.maxZ)}
}
