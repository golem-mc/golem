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
	seen  []int32
	// spare holds emptied cell slices a new cell can take.
	spare [][]int32
}

// boxCells is the inclusive range of cells box overlaps in x and z.
func (g *BoxGrid) boxCells(box AABB) (x0, x1, z0, z1 int32) {
	side := g.Cell
	if side == 0 {
		side = 16
	}
	return int32(math.Floor(box.minX / side)), int32(math.Floor(box.maxX / side)),
		int32(math.Floor(box.minZ / side)), int32(math.Floor(box.maxZ / side))
}

// Reset clears the cells and keeps their storage.
func (g *BoxGrid) Reset() {
	for k, c := range g.cells {
		g.spare = append(g.spare, c[:0])
		delete(g.cells, k)
	}
}

// Add files index i under every cell box overlaps in x and z. Indices must be
// added in ascending order.
func (g *BoxGrid) Add(i int32, box AABB) {
	if g.cells == nil {
		g.cells = make(map[[2]int32][]int32)
	}
	x0, x1, z0, z1 := g.boxCells(box)
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			k := [2]int32{x, z}
			c, ok := g.cells[k]
			if !ok && len(g.spare) > 0 {
				c = g.spare[len(g.spare)-1]
				g.spare = g.spare[:len(g.spare)-1]
			}
			g.cells[k] = append(c, i)
		}
	}
}

// Query yields each index filed under a cell box overlaps, once, in ascending
// order. yield must not call Query.
func (g *BoxGrid) Query(box AABB, yield func(i int32)) {
	x0, x1, z0, z1 := g.boxCells(box)
	if x0 == x1 && z0 == z1 {
		for _, i := range g.cells[[2]int32{x0, z0}] {
			yield(i)
		}
		return
	}
	g.seen = g.seen[:0]
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			g.seen = append(g.seen, g.cells[[2]int32{x, z}]...)
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
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			for _, i := range g.cells[[2]int32{x, z}] {
				yield(i)
			}
		}
	}
}

// Union is the smallest box holding both a and b.
func (a AABB) Union(b AABB) AABB {
	return AABB{min(a.minX, b.minX), min(a.minY, b.minY), min(a.minZ, b.minZ), max(a.maxX, b.maxX), max(a.maxY, b.maxY), max(a.maxZ, b.maxZ)}
}
