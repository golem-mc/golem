package storage

import (
	"errors"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
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

// TestSaveOffBesideAutoSaves toggles /save-off while auto-saves finish off the
// tick; it is the test of the flag's locking, so it reports under -race.
func TestSaveOffBesideAutoSaves(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.AutoSaveTicks = 0

	srv, c := servertest.Join(t, cfg, "Testificate")
	servertest.HoldTicks(t, srv)
	srv.SetAsyncAutoSave()
	srv.SetAutoSave(time.Second / 20)

	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			srv.SetSaving(i%2 == 0)
		}
	}()
	servertest.RunTicks(t, srv, 300)
	close(stop)
	<-done
	srv.SetAutoSave(0)
	servertest.RunTicks(t, srv, 2)
	srv.WaitAutoSave()
	c.Disconnect()
}

// TestStopTickWaitsForAnAutoSaveWriting checks stopping the main loop, the
// first step of a shutdown, returns only once an auto-save's chunk write is
// done, so the shutdown's saves and the closing of the world come after it,
// and that the change is on disk when the world reopens.
func TestStopTickWaitsForAnAutoSaveWriting(t *testing.T) {
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
	srv.SetAutoSave(0)
	servertest.RunTicks(t, srv, 2) // a tick that read the interval before it was off has ended

	// The write is let through only once the stop is asleep on the auto-save: a
	// stop that skipped the wait never reports it and the guard fails.
	parked := make(chan struct{}, 1)
	srv.OnStopWaitsForAutoSave(func() {
		select {
		case parked <- struct{}{}:
		default:
		}
	})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		srv.StopTick()
	}()
	select {
	case <-parked:
	case <-time.After(10 * time.Second): // a hang guard, not a wait
		t.Fatal("the main loop did not wait for the auto-save's write")
	}
	select {
	case <-stopped:
		t.Fatal("the main loop stopped with an auto-save's write still in flight")
	default:
	}
	close(release)
	select {
	case <-stopped:
	case <-time.After(10 * time.Second): // a hang guard, not a wait
		t.Fatal("the main loop did not stop once the write was let through")
	}
	srv.SetChunkWriteGate(nil)
	c.Disconnect()
	srv.Shutdown()
	if !blockIsSaved(t, cfg, x, y, z, 0) {
		t.Fatal("the change was not on disk after the shutdown")
	}
}

// TestAFailedMobWriteIsRetriedByTheNextSave kills the only mob of a chunk whose
// file names it, fails the save that would drop it, and checks the next save
// still rewrites the file without it, so the dead mob is not back after a restart.
func TestAFailedMobWriteIsRetriedByTheNextSave(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.AutoSaveTicks = 0
	srv, c := servertest.Join(t, cfg, "Keeper")
	servertest.HoldTicks(t, srv)

	id := srv.SpawnMob("minecraft:silverfish", 3.5, 100, 3.5)
	pos := world.ChunkPos{X: 0, Z: 0}
	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, pos, "minecraft:silverfish", func(nbt.Compound) {})
	if !srv.KillMob(id) {
		t.Fatal("the mob was not found to kill")
	}
	servertest.UntilTicks(t, srv, 40, "the dead mob left the herd", func() bool { return srv.MobCount() == 0 })

	srv.SetEntityWriteFault(errors.New("disk full"))
	srv.FlushMobs()
	srv.SetEntityWriteFault(nil)
	srv.FlushMobs()
	// A mob with no drops, so nothing else keeps the chunk's file being written:
	// only the listed flag does.
	if servertest.EntityChunkStored(t, cfg.LevelName, pos) {
		t.Error("the chunk's file still names the dead mob after the retry")
	}
	c.Disconnect()
}
