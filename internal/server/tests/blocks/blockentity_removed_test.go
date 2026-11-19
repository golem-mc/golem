package blocks

import (
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// A block entity the pass read its list from can leave the world before its
// own turn comes: a player breaks the block, and the break spills what the
// entity held while its stale pointer still waits in the list. Every turn
// that moves items must find the entity gone and skip.

// TestAHopperRemovedBeforeItsTurnMovesNothing removes the hopper's block
// entity between the block-entity pass reading its list and the hopper's own
// turn, the window a break opens. The turn must find the entity gone and
// leave the chest below alone; run on the stale pointer, it hands the chest
// the item the break has already spilled.
func TestAHopperRemovedBeforeItsTurnMovesNothing(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()

	if !srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
		t.Fatal("the hopper would not stand up")
	}
	if !srv.PlaceContainer(4, 63, 0, "minecraft:chest") {
		t.Fatal("the chest would not stand up")
	}
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 10)) {
		t.Fatal("the hopper has no block entity to fill")
	}

	srv.BlockEntityTickBetween(func(be *world.BlockEntity) {
		if be.Kind == world.KindHopper && be.X == 4 && be.Y == 64 && be.Z == 0 {
			srv.RemoveBlockEntity(4, 64, 0)
		}
	})
	defer srv.BlockEntityTickBetween(nil)

	srv.TickBlockEntities()

	dirt := servertest.ItemStack(t, "minecraft:dirt", 1).ItemID
	moved := int32(0)
	for _, s := range srv.InventoryAt(4, 63, 0) {
		if s.ItemID == dirt {
			moved += s.Count
		}
	}
	if moved != 0 {
		t.Errorf("the chest below holds %d dirt, want none moved from the removed hopper", moved)
	}
}

// TestAFurnaceRemovedBeforeItsTurnDoesNotSmelt removes the furnace's block
// entity between the pass reading its list and the furnace's turn. The turn
// must find the entity gone; run on the stale pointer, it lights the fire,
// spends the coal and starts a smelt in slots the break has already read.
func TestAFurnaceRemovedBeforeItsTurnDoesNotSmelt(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()

	if !srv.PlaceContainer(4, 64, 0, "minecraft:furnace") {
		t.Fatal("the furnace would not stand up")
	}
	coal := servertest.ItemStack(t, "minecraft:coal", 1)
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:raw_iron", 1)) {
		t.Fatal("the furnace has no input slot to fill")
	}
	if !srv.SetInventoryAt(4, 64, 0, 1, coal) {
		t.Fatal("the furnace has no fuel slot to fill")
	}

	var removed *world.BlockEntity
	srv.BlockEntityTickBetween(func(be *world.BlockEntity) {
		if be.Kind == world.KindFurnace && be.X == 4 && be.Y == 64 && be.Z == 0 {
			removed = be
			srv.RemoveBlockEntity(4, 64, 0)
		}
	})
	defer srv.BlockEntityTickBetween(nil)

	srv.TickBlockEntities()

	if removed == nil {
		t.Fatal("the furnace's turn never came")
	}
	if removed.LitRemaining != 0 {
		t.Errorf("the removed furnace has %d ticks of fire left, want its turn skipped", removed.LitRemaining)
	}
	if got := removed.Slots[1]; !got.Equal(coal) {
		t.Errorf("the removed furnace's fuel slot holds %+v, want the coal unspent", got)
	}
	if got := removed.Slots[2]; !got.Empty() {
		t.Errorf("the removed furnace's output holds %+v, want nothing smelted", got)
	}
}

// TestAJukeboxRemovedBeforeItsTurnDoesNotAdvance removes the jukebox's block
// entity between the pass reading its list and the jukebox's turn. The turn
// must find the entity gone; run on the stale pointer, it advances the song
// and, at the song's end, sends a second stop event.
func TestAJukeboxRemovedBeforeItsTurnDoesNotAdvance(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	srv.StopTick()

	servertest.Setblock(t, w.Client, 3, 64, 0, servertest.JukeboxCommand("cat", 1439))

	var removed *world.BlockEntity
	srv.BlockEntityTickBetween(func(be *world.BlockEntity) {
		if be.Kind == world.KindJukebox && be.X == 3 && be.Y == 64 && be.Z == 0 {
			removed = be
			srv.RemoveBlockEntity(3, 64, 0)
		}
	})
	defer srv.BlockEntityTickBetween(nil)

	srv.TickBlockEntities()

	if removed == nil {
		t.Fatal("the jukebox's turn never came")
	}
	if removed.TicksSinceSongStarted != 1439 {
		t.Errorf("the removed jukebox's song stands at %d ticks, want its turn skipped at 1439", removed.TicksSinceSongStarted)
	}
}

// TestAFurnaceTickReadsItsFireUnderTheLock changes a furnace's fire under
// its lock from another goroutine while the pass takes the furnace's turn;
// under -race, a read of the fire outside the lock is reported.
func TestAFurnaceTickReadsItsFireUnderTheLock(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.StopTick()

	if !srv.PlaceContainer(4, 64, 0, "minecraft:furnace") {
		t.Fatal("the furnace would not stand up")
	}

	var stop atomic.Bool
	done := make(chan struct{})
	started := make(chan struct{})
	srv.BlockEntityTickBetween(func(be *world.BlockEntity) {
		if be.Kind != world.KindFurnace || be.X != 4 || be.Y != 64 || be.Z != 0 {
			return
		}
		go func() {
			defer close(done)
			first := true
			for !stop.Load() {
				be.Use(func(e *world.BlockEntity) { e.LitRemaining++ })
				if first {
					first = false
					close(started)
				}
			}
		}()
		// The turn starts only once the writer has run, so the writer is
		// live while the turn reads; the race detector sees every unlocked
		// read the turn makes.
		<-started
	})
	defer srv.BlockEntityTickBetween(nil)

	srv.TickBlockEntities()
	stop.Store(true)
	<-done
}
