package storage

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAutoSaveWritesBesideTheTick holds an auto-save's chunk write open and
// checks the main loop keeps ticking meanwhile, then that the change is on
// disk once the write goes through. The ticks are stepped; the save goroutine
// is the one thing running beside them.
func TestAutoSaveWritesBesideTheTick(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.AutoSaveTicks = 0

	srv, c := servertest.Join(t, cfg, "Testificate")
	const x, y, z = int32(0), int32(63), int32(0)
	c.BreakBlock(x, y, z)
	c.ExpectBlockChange()
	servertest.HoldTicks(t, srv)
	srv.SetAsyncAutoSave()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	srv.SetChunkWriteGate(func() {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
	})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	servertest.AutoSaveEveryTick(srv)
	servertest.RunTicks(t, srv, servertest.AutoSaveEvery)

	select {
	case <-entered:
	case <-time.After(10 * time.Second): // a hang guard, not a wait
		t.Fatal("the auto-save never reached the chunk write")
	}
	// The save is parked in its write: a loop saving inline would never get
	// back from the tick that started it.
	servertest.RunTicks(t, srv, 3*servertest.AutoSaveEvery)
	if blockIsSaved(t, cfg, x, y, z, 0) {
		t.Fatal("the chunk was on disk before its write was let through")
	}
	srv.SetAutoSave(0)
	servertest.RunTicks(t, srv, 2) // a tick that read the interval before it was off has ended
	close(release)
	srv.SetChunkWriteGate(nil)
	srv.WaitAutoSave()

	if !blockIsSaved(t, cfg, x, y, z, 0) {
		t.Fatal("the change was not saved once the write went through")
	}
	c.Disconnect()
}
