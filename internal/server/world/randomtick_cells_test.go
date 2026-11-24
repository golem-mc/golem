package world

import (
	"testing"
)

// checkTickMask compares every bit of a built mask with live storage.
func checkTickMask(t *testing.T, s *section) {
	t.Helper()
	m := s.tickMask.Load()
	if m == nil {
		return
	}
	for i := range blocksPerSection {
		if m.has(i) != randomlyTicking(s.at(i)) {
			t.Fatalf("cell %d: mask=%v state=%d", i, m.has(i), s.at(i))
		}
	}
}

// builtMask builds the mask under the section's read-lock contract.
func builtMask(t *testing.T, s *section) *cellMask {
	t.Helper()
	m := s.tickCells(s.blocks.Load())
	checkTickMask(t, s)
	return m
}
