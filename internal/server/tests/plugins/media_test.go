package plugins_test

import (
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func TestPluginMediaBlockEntityKinds(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	w.SetBlock(1, 80, 1, registry.BlockJukebox.DefaultState())
	w.EnsureBlockEntity(1, 80, 1, world.KindJukebox)
	w.SetBlock(2, 80, 1, registry.BlockLectern.DefaultState())
	w.EnsureBlockEntity(2, 80, 1, world.KindLectern)
	run(func() {
		pw := h.Overworld()
		j, _ := pw.BlockEntity(plugin.BlockPos{X: 1, Y: 80, Z: 1})
		l, _ := pw.BlockEntity(plugin.BlockPos{X: 2, Y: 80, Z: 1})
		if _, ok := j.(plugin.Jukebox); !ok {
			t.Error("jukebox handle does not implement plugin.Jukebox")
		}
		if _, ok := l.(plugin.Lectern); !ok {
			t.Error("lectern handle does not implement plugin.Lectern")
		}
		chunk, _ := pw.Chunk(plugin.ChunkPos{})
		for _, b := range chunk.BlockEntities() {
			if b != j && b != l {
				t.Errorf("chunk handle differs from world handle: %v", b.Pos())
			}
		}
	})
}

func pluginBook() plugin.ItemStack {
	book := plugin.NewStack(registry.ItemWritableBook, 1)
	book.Patch.Other = map[plugin.Key]string{plugin.MinecraftKey("writable_book_content"): `{pages:["First","Second","Third"]}`}
	return book
}

// vanilla: JukeboxBlockEntity.setTheItem, LecternBlockEntity.setBook/setPage.
func TestPluginMediaEditsSaveReload(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, h, run := worldPlugin(t, cfg)
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	pos := world.ChunkPos{X: -2, Z: 3}
	w.Acquire(pos)
	defer w.Release(pos)
	w.SetBlock(-27, 80, 53, registry.BlockJukebox.DefaultState())
	w.EnsureBlockEntity(-27, 80, 53, world.KindJukebox)
	w.SetBlock(-26, 80, 53, registry.BlockLectern.DefaultState())
	w.EnsureBlockEntity(-26, 80, 53, world.KindLectern)
	var jukebox plugin.Jukebox
	var lectern plugin.Lectern
	key := plugin.Key{"test", "media"}
	record := plugin.NewStack(registry.ItemMusicDiscCat, 1)
	record.Patch.CustomName = new(plugin.Plain("Private disc"))
	record.Patch.Data = map[plugin.Key]any{key: []byte{1, 2}}
	book := pluginBook()
	run(func() {
		pw := h.Overworld()
		j, _ := pw.BlockEntity(plugin.BlockPos{X: -27, Y: 80, Z: 53})
		l, _ := pw.BlockEntity(plugin.BlockPos{X: -26, Y: 80, Z: 53})
		jukebox, _ = j.(plugin.Jukebox)
		lectern, _ = l.(plugin.Lectern)
	})
	if jukebox == nil || lectern == nil {
		t.Fatal("media handles missing")
	}
	run(func() {
		h.Overworld().Save(true)
		if !jukebox.Record().Empty() || jukebox.Playing() || !lectern.Book().Empty() || lectern.Page() != 0 {
			t.Error("fresh media state")
		}
		jukebox.SetRecord(record)
		lectern.SetBook(book)
		jukebox.Data().Set(key, int64(42))
		lectern.Data().Set(key, int64(43))
		if got := jukebox.Record(); got.Item != record.Item || got.Count != 1 || !reflect.DeepEqual(got.Patch, record.Patch) || !jukebox.Playing() {
			t.Errorf("record=%+v playing=%v", got, jukebox.Playing())
		}
		if lectern.Book().Item != book.Item || lectern.Page() != 0 {
			t.Error("book setter")
		}
		lectern.SetPage(200)
		if lectern.Page() != 2 {
			t.Errorf("page=%d, want last page 2", lectern.Page())
		}
		lectern.SetPage(-1)
		if lectern.Page() != 0 {
			t.Error("page not clamped to first page")
		}
		lectern.SetPage(1)
		lectern.SetBook(book)
		if lectern.Page() != 0 {
			t.Error("book replacement did not reset page")
		}
		lectern.SetPage(2)
		// Both directions copy mutable component values.
		record.Patch.CustomName.Literal = "Changed input"
		record.Patch.Data[key].([]byte)[0] = 9
		book.Patch.Other[plugin.MinecraftKey("writable_book_content")] = `{pages:["Changed"]}`
		copy := jukebox.Record()
		copy.Patch.CustomName.Literal = "Changed output"
		copy.Patch.Data[key].([]byte)[1] = 9
		if got := jukebox.Record(); got.Patch.CustomName.Literal != "Private disc" || !reflect.DeepEqual(got.Patch.Data[key], []byte{1, 2}) {
			t.Error("record aliases caller data")
		}
		copy = lectern.Book()
		copy.Patch.Other[plugin.MinecraftKey("writable_book_content")] = `{pages:[]}`
		if got := lectern.Book(); got.Patch.Other[plugin.MinecraftKey("writable_book_content")] == `{pages:[]}` || lectern.Page() != 2 {
			t.Error("book aliases caller data")
		}
		for _, invalid := range []plugin.ItemStack{
			plugin.NewStack(registry.Item(-1), 1),
			{Item: registry.ItemWrittenBook, Count: 1, Patch: plugin.ItemPatch{Other: map[plugin.Key]string{plugin.MinecraftKey("written_book_content"): "{"}}},
		} {
			jukebox.SetRecord(invalid)
			lectern.SetBook(invalid)
		}
		if jukebox.Record().Item != registry.ItemMusicDiscCat || lectern.Book().Item != registry.ItemWritableBook || lectern.Page() != 2 {
			t.Error("invalid stack changed media")
		}
		h.Overworld().Save(true)
	})
	// A separate disk reader proves explicit Save, before shutdown or unload.
	saved, err := world.NewWorld(cfg.LevelName, world.NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer saved.Close()
	saved.Acquire(pos)
	defer saved.Release(pos)
	for i, x := range []int32{-27, -26} {
		e := saved.BlockEntity(x, 80, 53)
		if e == nil {
			t.Fatal("saved entity missing")
		}
		e.Use(func(e *world.BlockEntity) {
			data := mustCompound(t, e.EncodeNBT(), "plugins")
			if data[key.String()] != nbt.Long(42+i) {
				t.Errorf("saved Data=%v", data)
			}
			if i == 0 && (e.Slots[0].ItemID != int32(registry.ItemMusicDiscCat) || !e.JukeboxPlaying() || e.TicksSinceSongStarted != 0) {
				t.Error("saved record or playback missing")
			}
			if i == 1 && (e.BookPages() != 3 || e.Page != 2) {
				t.Error("saved book or page missing")
			}
		})
	}
	w.Release(pos)
	w.DrainSaves()
	w.Acquire(pos)
	run(func() {
		if jukebox.IsValid() || lectern.IsValid() {
			t.Error("unloaded handles stayed valid")
		}
		jukebox.SetRecord(plugin.ItemStack{})
		lectern.SetBook(plugin.ItemStack{})
		lectern.SetPage(0)
		if !jukebox.Record().Empty() || jukebox.Playing() || !lectern.Book().Empty() || lectern.Page() != 0 {
			t.Error("stale reads returned old state")
		}
		j, _ := h.Overworld().BlockEntity(plugin.BlockPos{X: -27, Y: 80, Z: 53})
		l, _ := h.Overworld().BlockEntity(plugin.BlockPos{X: -26, Y: 80, Z: 53})
		jukebox = j.(plugin.Jukebox)
		lectern = l.(plugin.Lectern)
		if jukebox.Record().Item != registry.ItemMusicDiscCat || !jukebox.Playing() || lectern.Page() != 2 {
			t.Error("fresh handles lost saved state")
		}
		for i, b := range []plugin.BlockEntity{jukebox, lectern} {
			if value, ok := b.Data().Get(key); !ok || value != int64(42+i) {
				t.Errorf("reloaded Data=%v present=%v", value, ok)
			}
		}
		// Replacing an entity within the same loaded chunk also expires its handle.
		w.RemoveBlockEntity(-27, 80, 53)
		w.RemoveBlockEntity(-26, 80, 53)
		replacementDisc := w.EnsureBlockEntity(-27, 80, 53, world.KindJukebox)
		replacementBook := w.EnsureBlockEntity(-26, 80, 53, world.KindLectern)
		jukebox.SetRecord(plugin.NewStack(registry.ItemMusicDisc13, 1))
		lectern.SetBook(pluginBook())
		lectern.SetPage(1)
		if jukebox.IsValid() || lectern.IsValid() {
			t.Error("replaced handles stayed valid")
		}
		replacementDisc.Use(func(e *world.BlockEntity) {
			if !e.Slots[0].Empty() {
				t.Error("stale record setter edited replacement")
			}
		})
		replacementBook.Use(func(e *world.BlockEntity) {
			if !e.Slots[0].Empty() || e.Page != 0 {
				t.Error("stale book setter edited replacement")
			}
		})
	})
}

// TestPluginJukeboxRecordReplacePostsOneBlockChange replaces an occupied
// record: the change is one block_change vibration, not one per step.
// vanilla: JukeboxBlockEntity.setTheItem
func TestPluginJukeboxRecordReplacePostsOneBlockChange(t *testing.T) {
	t.Parallel()
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	srv.SetTickFrozen(true)
	w := srv.OverworldWorld()
	w.Acquire(world.ChunkPos{})
	defer w.Release(world.ChunkPos{})
	w.SetBlock(1, 80, 1, registry.BlockJukebox.DefaultState())
	w.EnsureBlockEntity(1, 80, 1, world.KindJukebox)
	var jukebox plugin.Jukebox
	run(func() {
		j, _ := h.Overworld().BlockEntity(plugin.BlockPos{X: 1, Y: 80, Z: 1})
		jukebox, _ = j.(plugin.Jukebox)
	})
	if jukebox == nil {
		t.Fatal("jukebox handle missing")
	}
	run(func() { jukebox.SetRecord(plugin.NewStack(registry.ItemMusicDiscCat, 1)) })

	var events atomic.Int32
	srv.ObserveGameEvents(func(event string, x, y, z float64) {
		if event == "minecraft:block_change" && x == 1.5 && y == 80.5 && z == 1.5 {
			events.Add(1)
		}
	})
	run(func() { jukebox.SetRecord(plugin.NewStack(registry.ItemMusicDisc13, 1)) })
	if got := events.Load(); got != 1 {
		t.Errorf("replacing the record posted %d block_change events, want 1", got)
	}
}

// jukeboxInsertScene puts an empty jukebox at the dig target for a held
// disc of cat, with listen's listeners, and returns a run for plugin tasks on
// the held loop.
func jukeboxInsertScene(t *testing.T, listen func(plugin.Server)) (*server.Server, plugin.Server, *servertest.Client, func(func())) {
	t.Helper()
	srv, host, c := blockScene(t, "minecraft:music_disc_cat", false, listen)
	d := servertest.DigTarget
	srv.SetBlock(d[0], d[1], d[2], servertest.BlockState(t, "minecraft:jukebox"))
	c.Settle()
	return srv, host, c, func(f func()) {
		t.Helper()
		ran := false
		host.Scheduler().RunTask(func() { f(); ran = true })
		servertest.RunTicks(t, srv, 1)
		if !ran {
			t.Fatal("plugin task did not complete")
		}
	}
}

// useJukeboxFrozen clicks the jukebox with the game frozen and the loop free,
// so the use and its listeners run without the world moving, and holds the
// loop again once the use is answered.
func useJukeboxFrozen(t *testing.T, srv *server.Server, c *servertest.Client) (ids []int32, payloads [][]byte) {
	t.Helper()
	srv.SetTickFrozen(true)
	servertest.ReleaseTicks(t, srv)
	d := servertest.DigTarget
	c.PlaceBlock(d[0], d[1], d[2], protocol.FaceTop)
	c.SettleAndCollect(func(id int32, data []byte) {
		ids = append(ids, id)
		payloads = append(payloads, data)
	})
	servertest.HoldTicks(t, srv)
	return ids, payloads
}

// jukeboxBlockChange reports whether e is block_change at the dig target's
// jukebox.
func jukeboxBlockChange(e *plugin.GenericGameEvent) bool {
	return e.Event == plugin.MinecraftKey("block_change") && e.Pos == (plugin.Vec3{X: 2.5, Y: 64.5, Z: 0.5})
}

// TestPluginJukeboxInsertEventsStayInJukeboxWorld moves the inserting player
// to another world from the first block_change listener; the player-attributed
// one still fires in the jukebox's world.
// vanilla: JukeboxPlayable.tryInsertIntoJukebox
func TestPluginJukeboxInsertEventsStayInJukeboxWorld(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var worlds []plugin.Key
	var host plugin.Server
	var flat plugin.World
	srv, host, c, run := jukeboxInsertScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.GenericGameEvent) {
			if !jukeboxBlockChange(e) {
				return
			}
			mu.Lock()
			worlds = append(worlds, e.World.Key())
			mu.Unlock()
			if e.Entity == nil {
				p, _ := host.PlayerNamed("Miner")
				p.Teleport(plugin.Location{World: flat, Pos: plugin.Vec3{X: 8.5, Y: 4, Z: 8.5}}, plugin.TeleportPlugin)
			}
		})
	})
	flat = createFlat(t, run, host, &flatGen{})
	useJukeboxFrozen(t, srv, c)
	run(func() {
		if p, _ := host.PlayerNamed("Miner"); p == nil || p.World().Key() != flatKey {
			t.Fatal("the listener did not move the player")
		}
	})
	mu.Lock()
	defer mu.Unlock()
	overworld := plugin.MinecraftKey("overworld")
	if !slices.Equal(worlds, []plugin.Key{overworld, overworld}) {
		t.Errorf("block_change events in %v, want both in the overworld", worlds)
	}
}

// TestPluginJukeboxInsertClearedByListenerSendsNoPlay clears the inserted
// disc from the first block_change listener: the client is told to stop the
// song and never to play it.
// vanilla: JukeboxSongPlayer.play, stop
func TestPluginJukeboxInsertClearedByListenerSendsNoPlay(t *testing.T) {
	t.Parallel()
	var cleared atomic.Bool
	srv, host, c, run := jukeboxInsertScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.GenericGameEvent) {
			if !jukeboxBlockChange(e) || e.Entity != nil || !cleared.CompareAndSwap(false, true) {
				return
			}
			b, _ := e.World.BlockEntity(plugin.BlockPos{X: 2, Y: 64, Z: 0})
			b.(plugin.Jukebox).SetRecord(plugin.ItemStack{})
		})
	})
	ids, payloads := useJukeboxFrozen(t, srv, c)
	if !cleared.Load() {
		t.Fatal("the listener did not clear the record")
	}
	var events []int32
	for i, id := range ids {
		if id != protocol.IDPlayWorldEvent {
			continue
		}
		if event := protocol.NewReader(payloads[i]).ReadI32(); event == protocol.WorldEventJukeboxPlay || event == protocol.WorldEventJukeboxStop {
			events = append(events, event)
		}
	}
	if !slices.Equal(events, []int32{protocol.WorldEventJukeboxStop}) {
		t.Errorf("jukebox level events %v, want only stop (%d)", events, protocol.WorldEventJukeboxStop)
	}
	run(func() {
		b, _ := host.Overworld().BlockEntity(plugin.BlockPos{X: 2, Y: 64, Z: 0})
		if j := b.(plugin.Jukebox); !j.Record().Empty() || j.Playing() {
			t.Error("the cleared jukebox holds a record or plays")
		}
	})
}
