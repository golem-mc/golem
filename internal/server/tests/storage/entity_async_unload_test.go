package storage

import (
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestEntityUnloadDoesNotWaitForDiskAndReloadsOnce(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Keeper")
	servertest.HoldTicks(t, srv)
	srv.SpawnStuckArrow("allowed", 3.5, 70, 3.5)
	srv.SpawnMob("minecraft:pig", 5.5, 64, 3.5)
	identity := srv.Mobs()[0].UUID
	srv.TickMobs(1)
	c.Disconnect()

	entered, release := make(chan struct{}, 1), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	srv.SetEntityWriteGate(func() {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
	})
	done := make(chan struct{})
	go func() { srv.TickMobs(1); close(done) }()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("entity unload never reached disk")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		unblock()
		<-done
		t.Fatal("mob tick waited for the entity disk write")
	}
	if srv.ArrowCount() != 0 || srv.MobCount() != 0 {
		t.Fatal("unloaded entities stayed live")
	}
	c = connect().Join("Keeper")
	srv.TickMobs(1)
	if srv.ArrowCount() != 1 || srv.MobCount() != 1 {
		t.Fatalf("immediate reload has %d arrows and %d mobs, want one each", srv.ArrowCount(), srv.MobCount())
	}
	if srv.Mobs()[0].UUID != identity {
		t.Fatal("immediate reload changed the pig's UUID")
	}
	unblock()
	srv.SetEntityWriteGate(nil)
	srv.FlushMobs()
	srv.TickMobs(2)
	if srv.ArrowCount() != 1 || srv.MobCount() != 1 || srv.Mobs()[0].UUID != identity {
		t.Fatal("write completion lost or duplicated a reloaded entity")
	}
	c.Disconnect()
	srv.TickMobs(1)
	srv.FlushMobs()
	c = connect().Join("Keeper")
	srv.TickMobs(1)
	if srv.ArrowCount() != 1 || srv.MobCount() != 1 || srv.Mobs()[0].UUID != identity {
		t.Fatal("a later reload lost or duplicated an entity or changed the pig's UUID")
	}
	c.Disconnect()
}
