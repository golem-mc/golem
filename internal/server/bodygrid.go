package server

import (
	"math"
	"slices"
	"sync"
)

// bodyGridSide is a cell's side in blocks.
const bodyGridSide = 2

// bodyCell is a cell's XZ coordinates: floor(x/2), floor(z/2).
type bodyCell struct{ x, z int }

func bodyCellOf(x, z float64) bodyCell {
	return bodyCell{int(math.Floor(x / bodyGridSide)), int(math.Floor(z / bodyGridSide))}
}

// bodyGrid files one world's bodies for one tick by their index in the
// tick's list, standing in for vanilla's section lookup in a merge scan.
type bodyGrid struct {
	cells map[bodyCell][]int
	// at is each index's cell and its slot in that cell's slice.
	at []bodySlot
	// spare holds emptied cell slices a new cell can take.
	spare [][]int
}

type bodySlot struct {
	cell bodyCell
	slot int
}

var bodyGrids sync.Pool

// takeBodyGrid is an empty grid, its map and cell slices kept from an
// earlier tick.
func takeBodyGrid() *bodyGrid {
	g, _ := bodyGrids.Get().(*bodyGrid)
	if g == nil {
		g = &bodyGrid{cells: make(map[bodyCell][]int)}
	}
	return g
}

// release empties the grid and keeps its cell slices for the next tick.
func (g *bodyGrid) release() {
	for _, c := range g.cells {
		g.spare = append(g.spare, c[:0])
	}
	clear(g.cells)
	g.at = g.at[:0]
	bodyGrids.Put(g)
}

// file appends index i to cell, starting an empty cell on a spare slice.
func (g *bodyGrid) file(i int, cell bodyCell) {
	c, ok := g.cells[cell]
	if !ok && len(g.spare) > 0 {
		c = g.spare[len(g.spare)-1]
		g.spare = g.spare[:len(g.spare)-1]
	}
	g.at[i] = bodySlot{cell, len(c)}
	g.cells[cell] = append(c, i)
}

// add files body i at (x, z); indices are added in order from 0.
func (g *bodyGrid) add(i int, x, z float64) {
	g.at = append(g.at, bodySlot{})
	g.file(i, bodyCellOf(x, z))
}

// moved re-files body i now at (x, z) when it left its cell.
func (g *bodyGrid) moved(i int, x, z float64) {
	cell, from := bodyCellOf(x, z), g.at[i]
	if cell == from.cell {
		return
	}
	c := g.cells[from.cell]
	last := len(c) - 1
	c[from.slot] = c[last]
	g.at[c[from.slot]].slot = from.slot
	g.cells[from.cell] = c[:last]
	g.file(i, cell)
}

// near appends to buf the indices filed in the cells the box reaches,
// sorted ascending so a caller sees them in tick order.
func (g *bodyGrid) near(minX, minZ, maxX, maxZ float64, buf []int) []int {
	lo, hi := bodyCellOf(minX, minZ), bodyCellOf(maxX, maxZ)
	start := len(buf)
	for x := lo.x; x <= hi.x; x++ {
		for z := lo.z; z <= hi.z; z++ {
			buf = append(buf, g.cells[bodyCell{x, z}]...)
		}
	}
	slices.Sort(buf[start:])
	return buf
}

// bodyNear is one world's bodies for one tick in a grid, answering a merge
// scan with the bodies whose centres lie within reach on X and Z (and some
// beyond), in tick order. The grid is built on the first query, from the
// positions the bodies hold then, so a tick with no scan due files nothing.
type bodyNear[T any] struct {
	grid   *bodyGrid // nil until the first query
	bodies []T
	pos    func(T) (x, z float64)
	reach  float64
	idx    []int
	out    []T
	// query is near as a func value, made once.
	query func(T) []T
}

func newBodyNear[T any](reach float64, pos func(T) (x, z float64)) *bodyNear[T] {
	n := &bodyNear[T]{pos: pos, reach: reach}
	n.query = n.near
	return n
}

// add lists b and returns its index.
func (n *bodyNear[T]) add(b T) int {
	n.bodies = append(n.bodies, b)
	return len(n.bodies) - 1
}

// moved re-files body i after its step.
func (n *bodyNear[T]) moved(i int) {
	if n.grid == nil {
		return
	}
	x, z := n.pos(n.bodies[i])
	n.grid.moved(i, x, z)
}

// near is the candidates for b's scan; the result is scratch valid until
// the next query.
func (n *bodyNear[T]) near(b T) []T {
	if n.grid == nil {
		n.grid = takeBodyGrid()
		for i, o := range n.bodies {
			x, z := n.pos(o)
			n.grid.add(i, x, z)
		}
	}
	x, z := n.pos(b)
	// The slack covers rounding in the caller's own box test at a cell edge.
	r := n.reach + 1e-6
	n.idx = n.grid.near(x-r, z-r, x+r, z+r, n.idx[:0])
	n.out = n.out[:0]
	for _, i := range n.idx {
		n.out = append(n.out, n.bodies[i])
	}
	return n.out
}

func (n *bodyNear[T]) release() {
	if n.grid != nil {
		n.grid.release()
		n.grid = nil
	}
}
