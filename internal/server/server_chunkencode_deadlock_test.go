package server

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// newHopperColumn starts a server with a hopper facing down between a full
// chest above and an empty chest below, so every eighth block-entity tick
// moves an item and marks the chunk dirty under the hopper's lock. The
// hopper's chunk is held until the test releases it.
func newHopperColumn(t *testing.T) (srv *Server, pos world.ChunkPos, hx, hy, hz int32) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = "minecraft:flat"
	srv, err := newServer(cfg, 1)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	w := srv.world

	hopper, ok := registry.FindState("minecraft:hopper", map[string]string{"facing": "down"})
	if !ok {
		t.Fatal("no hopper state in the registry")
	}
	chest, ok := registry.FindState("minecraft:chest", nil)
	if !ok {
		t.Fatal("no chest state in the registry")
	}
	dirt, ok := registry.ItemID("minecraft:dirt")
	if !ok {
		t.Fatal("no dirt in the registry")
	}

	hx, hy, hz = 4, 65, -1
	pos = world.ChunkPos{X: world.ChunkOf(float64(hx)), Z: world.ChunkOf(float64(hz))}
	// Held for the whole test, so the chunk never drops out from under the
	// goroutines racing the tick.
	w.Acquire(pos)
	for _, at := range []struct {
		y     int32
		state uint16
	}{
		{hy - 1, chest},
		{hy, hopper},
		{hy + 1, chest},
	} {
		if !w.SetBlock(hx, at.y, hz, at.state) {
			t.Fatalf("the flat world did not take the block at (%d, %d, %d)", hx, at.y, hz)
		}
	}
	kindAt := []world.BlockEntityKind{world.KindChest, world.KindHopper, world.KindChest}
	for i, at := range []int32{hy - 1, hy, hy + 1} {
		be := w.EnsureBlockEntity(hx, at, hz, kindAt[i])
		if be == nil {
			t.Fatalf("no block entity came with the container at (%d, %d, %d)", hx, at, hz)
		}
		if kindAt[i] != world.KindChest {
			continue
		}
		be.Use(func(b *world.BlockEntity) {
			if b.Y == hy+1 {
				// The chest above starts full, so the hopper always has
				// something to take; the chest below starts empty and
				// collects what comes down.
				for slot := range b.Slots {
					b.Put(slot, protocol.ItemStack{ItemID: dirt, Count: 64})
				}
			}
		})
	}
	return srv, pos, hx, hy, hz
}

// TestAChunkSendDoesNotDeadlockAgainstTheHopperTick drives the two ends of
// the lock order against each other: two goroutines send the hopper's chunk
// the way a join or a walk does (Acquire, ChunkPacket, Release), while the
// block-entity tick moves items from a full chest above the hopper into the
// chest below it. The tick holds the hopper's lock while it marks the chunk
// dirty through the world's write lock, so the send must read what the block
// entities hold with the world's lock down - the chunk packet that takes an
// entity's lock under the world's read lock waits on the tick, and the tick
// waits on the packet, for good. The watchdog turns that deadlock into a
// failure with every goroutine's stack after about 20 seconds instead of a
// hang; the ticks outlast the moves, which the chest below fills up around
// its two hundredth.
func TestAChunkSendDoesNotDeadlockAgainstTheHopperTick(t *testing.T) {
	t.Parallel()
	srv, pos, _, _, _ := newHopperColumn(t)
	w := srv.world

	// The tick loop, on its own goroutine the way the main loop runs it.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 600 {
			srv.tickBlockEntities()
		}
	}()

	stop := make(chan struct{})
	var senders sync.WaitGroup
	for range 2 {
		senders.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				chunk, _ := w.Acquire(pos)
				_ = w.ChunkPacket(chunk)
				w.Release(pos)
			}
		})
	}

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Fatalf("the chunk send deadlocked against the hopper tick:\n%s", buf[:n])
	}
	close(stop)
	senders.Wait()
	w.Release(pos)
	srv.Shutdown()
}

// TestABlockEntityTagReadAndWriteDoNotDeadlockAgainstTheHopperTick races
// the command paths that read and write a block entity's data (/data get,
// /data merge, /setblock with a tag) against the hopper tick, which holds the
// hopper's lock while it marks the chunk dirty through the world's write
// lock. A read or write that waits on the hopper's lock with the world's
// read lock still held deadlocks against it; the watchdog turns that into a
// failure with every goroutine's stack.
func TestABlockEntityTagReadAndWriteDoNotDeadlockAgainstTheHopperTick(t *testing.T) {
	t.Parallel()
	srv, pos, hx, hy, hz := newHopperColumn(t)
	w := srv.world

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 600 {
			srv.tickBlockEntities()
		}
	}()

	stop := make(chan struct{})
	var callers sync.WaitGroup
	for _, y := range []int32{hy, hy - 1} {
		callers.Add(2)
		// A reader of the entity's data and a writer giving the same data
		// back, so the column keeps moving items.
		go func() {
			defer callers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, _ = w.BlockEntityTag(hx, y, hz)
			}
		}()
		go func() {
			defer callers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				tag, ok := w.BlockEntityTag(hx, y, hz)
				if !ok {
					continue
				}
				id, _ := nbt.Get[nbt.String](tag, "id")
				w.SetBlockEntityTag(hx, y, hz, string(id), tag)
			}
		}()
	}

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Fatalf("a block-entity tag read or write deadlocked against the hopper tick:\n%s", buf[:n])
	}
	close(stop)
	callers.Wait()
	w.Release(pos)
	srv.Shutdown()
}
