package server

import (
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// reconnectShapes runs the world's reconnect pass at (x, y, z) and tells
// everyone the writes.
func (s *Server) reconnectShapes(w *world.World, x, y, z int32) {
	// The box holds the cells the pass first writes; a wall or stair reads
	// farther once a write is found.
	if !w.ShapeBlocksIn(x-1, y-1, z-1, x+1, y+1, z+1) {
		return
	}
	s.tellShapeChanges(w, rules.ReconnectShapes(w, x, y, z))
}

// updateNeighborShapes runs the neighbour shape pass around (x, y, z) and
// tells everyone the writes.
func (s *Server) updateNeighborShapes(w *world.World, x, y, z int32) {
	if !w.ShapeBlocksIn(x-1, y-1, z-1, x+1, y+1, z+1) {
		return
	}
	s.tellShapeChanges(w, rules.UpdateNeighborShapes(w, x, y, z))
}

// tellShapeChanges sends a block update for each write, one relight per
// chunk, then runs each write's physics pass.
func (s *Server) tellShapeChanges(w *world.World, changes []world.BlockChange) {
	for _, c := range changes {
		s.markChanged(w, c.X, c.Y, c.Z)
	}
	s.relightChanges(w, changes)
	for _, c := range changes {
		s.physicsShapePass(w, c.X, c.Y, c.Z)
	}
}
