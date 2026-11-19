package containers

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestAHopperChainMarksItsChunksWhileSendsAndTagReadsRace drives the hopper's
// container writes against two readers of a block entity and a writer of the
// world lock: a chunk send, which reads the entities with the world lock down;
// a command's block entity tag read; and a mark of an unrelated chunk, which
// queues a world write lock. The hopper's tick writes its containers and its
// target's under their locks and marks the chunks dirty. A tag read that holds
// the world's read lock while it waits on the entity's circles with the tick's
// mark once the writer is queued: the reader holds the lock the writer waits
// for and waits on the entity the tick holds. The chunk is held open for the
// whole test. The watchdog turns the deadlock into a failure with every
// goroutine's stack after about 20 seconds.
func TestAHopperChainMarksItsChunksWhileSendsAndTagReadsRace(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = "minecraft:flat"
	srv, err := server.NewSeeded(cfg, 1)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	w := srv.OverworldWorld()

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

	// A chain above the ground: a full chest feeds the top hopper, it feeds
	// the bottom one, which feeds an empty chest below. The top hopper always
	// has an item to push and the bottom is never full.
	const hx, hz = int32(4), int32(-1)
	const base = int32(65)
	pos := world.ChunkPos{X: world.ChunkOf(float64(hx)), Z: world.ChunkOf(float64(hz))}
	// Held for the whole test, so the chunk never drops out from under the
	// senders and they need no Acquire of their own.
	chunk, _ := w.Acquire(pos)
	kindAt := []world.BlockEntityKind{world.KindChest, world.KindHopper, world.KindHopper, world.KindChest}
	for i, at := range []int32{base, base + 1, base + 2, base + 3} {
		state := chest
		if kindAt[i] == world.KindHopper {
			state = hopper
		}
		if !w.SetBlock(hx, at, hz, state) {
			t.Fatalf("the flat world did not take the block at (%d, %d, %d)", hx, at, hz)
		}
		be := w.EnsureBlockEntity(hx, at, hz, kindAt[i])
		if be == nil {
			t.Fatalf("no block entity came with the container at (%d, %d, %d)", hx, at, hz)
		}
		if at != base+3 {
			continue
		}
		// The source chest starts full, so the chain always has something to
		// move; the destination starts empty.
		be.Use(func(b *world.BlockEntity) {
			for slot := range b.Slots {
				b.Put(slot, protocol.ItemStack{ItemID: dirt, Count: 64})
			}
		})
	}

	// The tick loop, on its own goroutine the way the main loop runs it.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 600 {
			srv.TickBlockEntities()
		}
	}()

	stop := make(chan struct{})
	var workers sync.WaitGroup
	for range 2 {
		workers.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = w.ChunkPacket(chunk)
			}
		})
	}
	for _, y := range []int32{base + 1, base + 2} {
		workers.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				_, _ = w.BlockEntityTag(hx, y, hz)
			}
		})
	}
	// A world write lock, so the tick's read lock under its entity lock queues
	// behind it and a tag read must not hold the world's lock while it waits
	// on the entity. The chunk is not loaded, so the mark itself does nothing.
	workers.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			w.Touch(hx+1000, 70, hz)
		}
	})

	select {
	case <-done:
	case <-time.After(20 * time.Second):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Fatalf("a chunk mark under a block entity's lock deadlocked:\n%s", buf[:n])
	}
	close(stop)
	workers.Wait()
	w.Release(pos)
	srv.Shutdown()
}
