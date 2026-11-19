package storage

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// blockIsSaved reports whether a world reopened from the same directory sees
// the given block state at (x, y, z). It is how the auto-save tests check that
// a change reached disk: a world loading the chunk reads it from the region
// file, so if the change is there, the save happened. Acquire forces the chunk
// to load — BlockAt alone returns air for any chunk that is not in memory,
// which would hide whether it was saved or not. The world is opened directly
// rather than through a second server, which would be stopped by the running
// server's session lock; the flat generator is the harness's, so a chunk that
// was never saved reads back as the ground it would generate.
func blockIsSaved(t *testing.T, cfg *config.Config, x, y, z int32, want uint16) bool {
	t.Helper()
	w, err := world.NewWorld(cfg.LevelName, world.NewFlatGenerator())
	if err != nil {
		t.Fatalf("reopen world: %v", err)
	}
	defer w.Close()
	pos := world.ChunkPos{X: world.ChunkOf(float64(x)), Z: world.ChunkOf(float64(z))}
	w.Acquire(pos)
	defer w.Release(pos)
	return w.BlockAt(x, y, z) == want
}

// defaultAutoSaveTicks is config.Default's auto-save interval, in ticks.
const defaultAutoSaveTicks = 6000

// TestAutoSaveWritesDirtyChunks checks that the periodic save writes a chunk a
// player has changed to its region file, so a crash after the interval — but
// before the chunk is unloaded — does not lose the change. Without the
// auto-save, the change would only exist in memory until the player walks away
// or the server is shut down cleanly.
func TestAutoSaveWritesDirtyChunks(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.AutoSaveTicks = 0 // on once the loop is held, so no save falls in the join

	srv, c := servertest.Join(t, cfg, "Testificate")

	// Break a block at spawn, which is in the chunk the player starts in.
	const x, y, z = int32(0), int32(63), int32(0)
	c.BreakBlock(x, y, z)
	c.ExpectBlockChange()
	servertest.HoldTicks(t, srv)
	servertest.AutoSaveEveryTick(srv)

	// The chunk is still loaded (the player is in it), and the change is in
	// memory only. Until the interval has run out nothing is written; the
	// save then goes out inside the tick, to the region file a new server
	// reading the same directory sees.
	servertest.RunTicks(t, srv, servertest.AutoSaveEvery-1)
	if blockIsSaved(t, cfg, x, y, z, 0) {
		t.Fatal("the change was saved before the auto-save interval ran out")
	}
	servertest.RunTicks(t, srv, 1)
	if !blockIsSaved(t, cfg, x, y, z, 0) {
		t.Fatalf("the change was not saved after %d ticks", servertest.AutoSaveEvery)
	}

	c.Disconnect()
}

// TestAutoSaveDisabledDoesNotSave checks that a server with auto-save-ticks=0
// does not write chunks periodically: a dirty chunk stays in memory until the
// player leaves or the server shuts down, the way "disable-autosave" does in
// the vanilla server.
func TestAutoSaveDisabledDoesNotSave(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.AutoSaveTicks = 0

	srv, c := servertest.Join(t, cfg, "Testificate")

	const x, y, z = int32(0), int32(63), int32(0)
	c.BreakBlock(x, y, z)
	c.ExpectBlockChange()
	servertest.HoldTicks(t, srv)
	// The client answers nothing in the minutes of game time that follow;
	// a keep-alive drop would save its chunk on departure.
	srv.SetKeepAlive(time.Hour, time.Hour)

	// Run past the default interval. The chunk is still loaded and the
	// player still in it, so nothing has been saved.
	servertest.RunTicks(t, srv, defaultAutoSaveTicks+servertest.AutoSaveEvery)
	if blockIsSaved(t, cfg, x, y, z, 0) {
		t.Error("the change was saved with the auto-save off, which should only happen on unload or shutdown")
	}

	// The shutdown save is what writes it in the end.
	c.Disconnect()
	srv.Shutdown()

	if !blockIsSaved(t, cfg, x, y, z, 0) {
		t.Error("the change was not saved on shutdown, which is the backstop the auto-save off case relies on")
	}
}

// TestAutoSaveWritesOnlinePlayers checks that the periodic save writes an
// online player's file as well as the chunks, so a crash after the interval
// does not lose what they did since they joined. The client stays connected,
// so the auto-save is the only save that can put the stack in the file.
func TestAutoSaveWritesOnlinePlayers(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.AutoSaveTicks = 0 // on once the loop is held, so no save falls in the join

	srv, c := servertest.Join(t, cfg, "Testificate")

	diamond, ok := registry.ItemID("minecraft:diamond")
	if !ok {
		t.Fatal("the item registry has no diamond")
	}
	c.SetCreativeSlot(36, protocol.ItemStack{ItemID: diamond, Count: 5})
	c.Settle()
	servertest.HoldTicks(t, srv)
	servertest.AutoSaveEveryTick(srv)

	path := servertest.PlayerFile(cfg.LevelName, "Testificate")
	var saved nbt.Compound
	servertest.UntilTicks(t, srv, servertest.AutoSaveEvery, "the auto-save writes the diamonds", func() bool {
		root, ok := readPlayerFileQuietly(path)
		if !ok {
			return false
		}
		saved = root
		return inventoryItemCount(root, "minecraft:diamond") > 0
	})
	if got := inventoryItemCount(saved, "minecraft:diamond"); got != 5 {
		t.Errorf("the auto-save wrote %d diamonds, want the 5 the player put in", got)
	}

	c.Disconnect()
}

// readPlayerFileQuietly reads a saved player file without failing the test, so
// a poll can tell a save that has not happened yet from one that has.
func readPlayerFileQuietly(path string) (nbt.Compound, bool) {
	root, _, err := servertest.ReadPlayerFileData(path)
	if err != nil {
		return nil, false
	}
	return root, true
}

// inventoryItemCount is how many of the named item a saved player's inventory
// holds.
func inventoryItemCount(root nbt.Compound, name string) int32 {
	items, ok := nbt.Get[nbt.List](root, "Inventory")
	if !ok {
		return 0
	}
	var total int32
	for _, it := range items.Items {
		entry, ok := it.(nbt.Compound)
		if !ok {
			continue
		}
		if id, _ := nbt.Get[nbt.String](entry, "id"); id != nbt.String(name) {
			continue
		}
		count, _ := nbt.Get[nbt.Int](entry, "count")
		total += int32(count)
	}
	return total
}
