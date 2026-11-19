package plugins_test

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func TestPluginMediaBlockEntityKinds(t *testing.T) {
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

func mediaBlockChanges(t *testing.T, id int32, data []byte, states map[int32]uint16) {
	t.Helper()
	switch id {
	case protocol.IDPlayBlockUpdate:
		r := protocol.NewReader(data)
		x, y, z := r.ReadPosition()
		state := uint16(r.ReadVarInt())
		if y == 64 && z == 0 && (x == 3 || x == 4) {
			states[x] = state
		}
	case protocol.IDPlaySectionBlocks:
		section, entries := servertest.ReadSectionBlocks(t, data)
		if section == 4 { // chunk (0, 0), section Y=4
			for _, entry := range entries {
				if cell := entry & 0xfff; cell == 3<<8 || cell == 4<<8 {
					states[int32(cell>>8)] = uint16(entry >> 12)
				}
			}
		}
	}
}

// vanilla: JukeboxBlockEntity.setTheItem, LecternBlockEntity.setBook/setPage.
func TestPluginMediaEditsSaveReload(t *testing.T) {
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

// vanilla: JukeboxSongPlayer.play/stop, LecternBlock.signalPageChange.
func TestPluginMediaWorldAndClientEffects(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, h, run := worldPlugin(t, cfg)
	servertest.FreezeUntilHeld(t, srv)
	servertest.ReleaseTicks(t, srv)
	c := servertest.WindowedOf(t, "MediaObserver", servertest.DialServer(t, srv))
	servertest.HoldTicks(t, srv)
	srv.SetTickFrozen(true)
	srv.RunLine("setblock 4 64 0 jukebox")
	srv.RunLine("setblock 3 64 0 lectern")
	c.Settle()
	var jukebox plugin.Jukebox
	var lectern plugin.Lectern
	run(func() {
		j, _ := h.Overworld().BlockEntity(plugin.BlockPos{X: 4, Y: 64})
		l, _ := h.Overworld().BlockEntity(plugin.BlockPos{X: 3, Y: 64})
		jukebox, _ = j.(plugin.Jukebox)
		lectern, _ = l.(plugin.Lectern)
	})
	if jukebox == nil || lectern == nil {
		t.Fatal("media handles missing")
	}
	states := map[int32]uint16{3: registry.BlockLectern.DefaultState(), 4: registry.BlockJukebox.DefaultState()}
	customDisc := plugin.NewStack(registry.ItemStick, 1)
	customDisc.Patch.Other = map[plugin.Key]string{plugin.MinecraftKey("jukebox_playable"): `"minecraft:cat"`}
	silentDisc := plugin.NewStack(registry.ItemMusicDiscCat, 1)
	silentDisc.Patch.Other = map[plugin.Key]string{plugin.MinecraftKey("jukebox_playable"): ""}
	for _, tc := range []struct {
		stack   plugin.ItemStack
		event   int32
		data    int32
		playing bool
	}{
		{plugin.NewStack(registry.ItemMusicDiscCat, 1), protocol.WorldEventJukeboxPlay, registry.JukeboxSongID("minecraft:cat"), true},
		{plugin.NewStack(registry.ItemMusicDisc13, 1), protocol.WorldEventJukeboxPlay, registry.JukeboxSongID("minecraft:13"), true},
		{customDisc, protocol.WorldEventJukeboxPlay, registry.JukeboxSongID("minecraft:cat"), true},
		{silentDisc, protocol.WorldEventJukeboxStop, 0, false},
		{plugin.NewStack(registry.ItemMusicDisc13, 1), protocol.WorldEventJukeboxPlay, registry.JukeboxSongID("minecraft:13"), true},
		{plugin.NewStack(registry.ItemMusicDisc13, 1), protocol.WorldEventJukeboxPlay, registry.JukeboxSongID("minecraft:13"), true},
		{plugin.NewStack(registry.ItemStone, 1), protocol.WorldEventJukeboxStop, 0, false},
		{plugin.ItemStack{}, 0, 0, false},
		{plugin.NewStack(registry.ItemMusicDiscCat, 1), protocol.WorldEventJukeboxPlay, registry.JukeboxSongID("minecraft:cat"), true},
		{plugin.ItemStack{}, protocol.WorldEventJukeboxStop, 0, false},
	} {
		run(func() { jukebox.SetRecord(tc.stack) })
		events := 0
		c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
			mediaBlockChanges(t, id, data, states)
			if id == protocol.IDPlayBlockEntityData || id == protocol.IDPlaySpawnEntity {
				t.Error("record setter sent entity data or dropped an item")
			}
			if id == protocol.IDPlayWorldEvent {
				event, x, y, z, d := servertest.ReadWorldEvent(t, data)
				if event != tc.event || x != 4 || y != 64 || z != 0 || d != tc.data {
					t.Errorf("record event=%d at %d,%d,%d data=%d", event, x, y, z, d)
				}
				events++
			}
			return false
		})
		wantEvents := 0
		if tc.event != 0 {
			wantEvents = 1
		}
		if events != wantEvents {
			t.Errorf("record %+v: events=%d, want %d for event %d", tc.stack, events, wantEvents, tc.event)
		}
		run(func() {
			if jukebox.Playing() != tc.playing || registry.StateBool(uint16(h.Overworld().BlockState(jukebox.Pos())), registry.PropHasRecord) == tc.stack.Empty() {
				t.Error("record state does not match playback and slot")
			}
			if states[4] != uint16(h.Overworld().BlockState(jukebox.Pos())) {
				t.Error("client did not receive has_record change")
			}
		})
	}
	run(func() { lectern.SetBook(pluginBook()) })
	c.ReadTicks(srv, 1, func(id int32, data []byte) bool { mediaBlockChanges(t, id, data, states); return false })
	if !registry.StateBool(states[3], registry.PropHasBook) {
		t.Error("client did not receive has_book change")
	}
	c.UseBlock(3, 64, 0)
	slots := c.ExpectContainerOpen(protocol.MenuLectern, false, 3, 64, 0)
	if len(slots) != 1 || slots[0].ItemID != int32(registry.ItemWritableBook) {
		t.Fatal("client did not see plugin book")
	}
	c.AwaitWindowProperty(0, 0)
	run(func() { lectern.SetPage(200) })
	pageTurns := 0
	c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		mediaBlockChanges(t, id, data, states)
		if id == protocol.IDPlayWorldEvent {
			event, x, y, z, _ := servertest.ReadWorldEvent(t, data)
			if event == protocol.WorldEventLecternPageTurn && x == 3 && y == 64 && z == 0 {
				pageTurns++
			}
		}
		c.Record(id, data)
		return false
	})
	if pageTurns != 1 || c.Props[0] != 2 {
		t.Errorf("page turns=%d menu page=%d", pageTurns, c.Props[0])
	}
	if !registry.StateBool(srv.OverworldWorld().BlockAt(3, 64, 0), registry.PropPowered) || !registry.StateBool(states[3], registry.PropPowered) {
		t.Error("page turn did not pulse redstone")
	}
	run(func() { lectern.SetPage(999) })
	c.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlayWorldEvent, protocol.IDPlayContainerProperty))
	srv.SetTickFrozen(false)
	servertest.RunTicks(t, srv, 3)
	srv.SetTickFrozen(true)
	if registry.StateBool(srv.OverworldWorld().BlockAt(3, 64, 0), registry.PropPowered) {
		t.Error("page pulse did not end")
	}
	c.Settle()
	written := plugin.NewStack(registry.ItemWrittenBook, 1)
	written.Patch.Other = map[plugin.Key]string{plugin.MinecraftKey("written_book_content"): `{title:"Names",author:"Tester",pages:[{selector:"@a"}],resolved:false}`}
	run(func() { lectern.SetBook(written) })
	bookSent := false
	c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		if id == protocol.IDPlayContainerSlot {
			r := protocol.NewReader(data)
			window := r.ReadVarInt()
			r.ReadVarInt()
			slot := r.ReadI16()
			stack, ok := r.ReadSlot()
			book, isBook := stack.WrittenBookOf()
			if window != c.Window || slot != 0 || !ok || !isBook || !book.Resolved || len(book.Pages) != 1 || book.Pages[0].Raw.Plain() != "MediaObserver" {
				t.Errorf("replacement book not resolved and sent to menu: %+v", book)
			}
			bookSent = true
		}
		c.Record(id, data)
		return false
	})
	if !bookSent {
		t.Error("replacement book not sent to open menu")
	}
	if c.Props[0] != 0 {
		t.Error("replacement book did not reset the open menu page")
	}
	run(func() { lectern.SetBook(plugin.ItemStack{}) })
	c.ReadTicks(srv, 1, func(id int32, data []byte) bool {
		mediaBlockChanges(t, id, data, states)
		c.Record(id, data)
		return false
	})
	if registry.StateBool(srv.OverworldWorld().BlockAt(3, 64, 0), registry.PropHasBook) || registry.StateBool(states[3], registry.PropHasBook) {
		t.Error("emptying lectern left has_book set")
	}
	run(func() {
		w := srv.OverworldWorld()
		w.RemoveBlockEntity(4, 64, 0)
		w.RemoveBlockEntity(3, 64, 0)
		w.EnsureBlockEntity(4, 64, 0, world.KindJukebox)
		w.EnsureBlockEntity(3, 64, 0, world.KindLectern)
		jukebox.SetRecord(plugin.NewStack(registry.ItemMusicDiscCat, 1))
		lectern.SetBook(pluginBook())
		lectern.SetPage(1)
	})
	c.ExpectNoneWithinTicks(srv, 1, servertest.PacketIs(protocol.IDPlayWorldEvent, protocol.IDPlayBlockEntityData, protocol.IDPlayContainerSlot))
}

func TestPluginMediaComparatorSignals(t *testing.T) {
	srv, h, run := worldPlugin(t, servertest.BlockTestConfig())
	w := srv.OverworldWorld()
	defer srv.HoldChunk(0, 0)()
	for x := int32(1); x <= 3; x++ {
		for _, z := range []int32{1, 5} {
			w.SetBlock(x, 79, z, registry.BlockStone.DefaultState())
		}
	}
	srv.RunLine("setblock 1 80 1 jukebox")
	srv.RunLine("setblock 1 80 5 lectern")
	for _, z := range []int32{1, 5} {
		state, _ := registry.WithProp(registry.BlockComparator.DefaultState(), registry.PropFacing, "west")
		w.SetBlock(2, 80, z, state)
		w.EnsureBlockEntity(2, 80, z, world.KindComparator)
		w.SetBlock(3, 80, z, registry.BlockRedstoneWire.DefaultState())
	}
	var jukebox plugin.Jukebox
	var lectern plugin.Lectern
	run(func() {
		j, _ := h.Overworld().BlockEntity(plugin.BlockPos{X: 1, Y: 80, Z: 1})
		l, _ := h.Overworld().BlockEntity(plugin.BlockPos{X: 1, Y: 80, Z: 5})
		jukebox, _ = j.(plugin.Jukebox)
		lectern, _ = l.(plugin.Lectern)
	})
	if jukebox == nil || lectern == nil {
		t.Fatal("media handles missing")
	}
	for _, tc := range []struct {
		edit       func()
		disc, book int
	}{
		{func() {
			jukebox.SetRecord(plugin.NewStack(registry.ItemMusicDiscCat, 1))
			lectern.SetBook(pluginBook())
		}, 2, 1},
		{func() { jukebox.SetRecord(plugin.NewStack(registry.ItemMusicDisc13, 1)); lectern.SetPage(2) }, 1, 15},
		{func() {
			jukebox.SetRecord(plugin.NewStack(registry.ItemMusicDisc13, 1))
			w.BlockEntity(1, 80, 1).Use(func(e *world.BlockEntity) {
				if e.TicksSinceSongStarted != 0 {
					t.Error("replacing the same record did not restart playback")
				}
			})
		}, 1, 15},
		{func() { jukebox.SetRecord(plugin.ItemStack{}); lectern.SetBook(plugin.ItemStack{}) }, 0, 0},
	} {
		run(tc.edit)
		servertest.RunTicks(t, srv, 4)
		for i, z := range []int32{1, 5} {
			want := []int{tc.disc, tc.book}[i]
			if power, _ := registry.StateInt(w.BlockAt(3, 80, z), registry.PropPower); power != want {
				t.Errorf("wire at z=%d carries %d, want %d", z, power, want)
			}
		}
	}
}
