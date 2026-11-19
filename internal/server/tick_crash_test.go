package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
)

// TestATickPanicStopsTheLoopAndTheServerStillSaves checks that a panic in a
// tick ends the main loop and requests a stop, and that the shutdown after it
// still saves the world.
func TestATickPanicStopsTheLoopAndTheServerStillSaves(t *testing.T) {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	// The world's creation wrote one; the shutdown must write it again.
	levelDat := filepath.Join(cfg.LevelName, "level.dat")
	if err := os.Remove(levelDat); err != nil {
		t.Fatal(err)
	}

	fault := func() { panic("tick fault") }
	s.tickFault.Store(&fault)
	s.startTick()

	select {
	case <-s.StopRequested():
	case <-time.After(5 * time.Second):
		t.Fatal("the panicking tick did not request a stop within 5s")
	}
	if !s.Crashed() {
		t.Fatal("Crashed() = false after a tick panicked")
	}

	done := make(chan struct{})
	go func() {
		s.Shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Shutdown did not return within 10s after the crash")
	}
	if _, err := os.Stat(levelDat); err != nil {
		t.Fatalf("level.dat after the shutdown: %v", err)
	}
}
