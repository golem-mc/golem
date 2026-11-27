package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// A frame that passed a poll is checked against a body that joins later.
func TestPollFramesSeesALateNeighbour(t *testing.T) {
	s, w := newTrackerBenchServer(t)
	a := newFrame(s.nextEntity(), uuid.New(), w, 0, world.GroundLevel, 0, 1, false)
	s.registerFrame(a)
	a.checkInterval = 100
	s.pollFrames()
	if len(s.frames) != 1 {
		t.Fatalf("%d frames after a clean poll, want 1", len(s.frames))
	}
	b := newFrame(s.nextEntity(), uuid.New(), w, 0, world.GroundLevel, 0, 1, false)
	s.registerFrame(b)
	a.checkInterval, b.checkInterval = 100, 100
	s.pollFrames()
	if len(s.frames) != 0 {
		t.Fatalf("%d frames after two overlapped, want 0", len(s.frames))
	}
}
