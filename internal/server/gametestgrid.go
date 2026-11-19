package server

import (
	"github.com/golem-mc/golem/internal/server/world"
)

// gameTestGrid is one dimension's next test corner and current row bounds.
// vanilla: StructureGridSpawner.DimensionGridState
type gameTestGrid struct {
	first, next   [3]int32
	rowZ, rowMaxZ int32
	columns       int32
}

// spawnGameTest advances by padded structure bounds, not the template's size.
// vanilla: StructureGridSpawner.spawnStructure
func (s *Server) spawnGameTest(t *gameTest) *gameTest {
	ticker := &s.gameTests
	if ticker.grids == nil {
		ticker.grids = map[*world.World]*gameTestGrid{}
	}
	g := ticker.grids[t.w]
	if g == nil {
		ws := s.gameTestScope(t.w)
		x, z := ticker.request.Origin[0], ticker.request.Origin[2]
		release := ws.HoldChunk(x, z)
		y := ws.HeightmapHeight("world_surface", x, z)
		release()
		g = &gameTestGrid{first: [3]int32{x, y, z + 3}, next: [3]int32{x, y, z + 3}, rowZ: z + 3, rowMaxZ: z + 4}
		ticker.grids[t.w] = g
	}
	t.pos = g.next
	t = s.rerunGameTest(t)
	_, lo, hi := testStructureBox(t.pos[0], t.pos[1], t.pos[2], t.data, t.test)
	g.rowMaxZ = max(g.rowMaxZ, hi[2]+t.test.Padding+1)
	g.next[0] += hi[0] - lo[0] + 1 + 2*t.test.Padding + 5 // vanilla: SPACE_BETWEEN_COLUMNS
	if g.columns++; g.columns >= ticker.request.PerRow {
		g.columns = 0
		g.next[0], g.next[2] = g.first[0], g.next[2]+g.rowMaxZ-g.rowZ+6 // vanilla: SPACE_BETWEEN_ROWS
		g.rowZ, g.rowMaxZ = g.next[2], g.next[2]+1
	}
	return t
}
