package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// openWindowedOps is openWindowed with the player listed as an operator, so a
// jukebox test can lay a playing jukebox with the NBT a save would hold.
func OpenWindowedOps(t *testing.T, cfg *config.Config) (*server.Server, *servertest.WindowedClient) {
	t.Helper()
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	return srv, servertest.WindowedOf(t, "Testificate", c)
}

// awaitJukeboxEvent waits for the next world event of the given kind and
// returns where it played and the data it carried.
func awaitJukeboxEvent(t *testing.T, w *servertest.WindowedClient, event int32) (x, y, z, data int32) {
	t.Helper()
	w.AwaitWindowPackets(func(id int32, payload []byte) bool {
		if id != protocol.IDPlayWorldEvent {
			return false
		}
		r := protocol.NewReader(payload)
		if r.ReadI32() != event {
			return false
		}
		x, y, z = r.ReadPosition()
		data = r.ReadI32()
		return true
	})
	return x, y, z, data
}

// jukeboxCommand builds a /setblock argument for a jukebox holding a disc
// with the given ticks_since_song_started.
func JukeboxCommand(disc string, ticks int64) string {
	return `minecraft:jukebox[has_record=true]{RecordItem:{id:"minecraft:music_disc_` + disc +
		`",count:1},ticks_since_song_started:` + strconv.FormatInt(ticks, 10) + `L}`
}

// TestAJukeboxTakesADiscAndReturnsIt uses a disc on an empty jukebox: the disc
// goes in, one item leaves the hand, has_record stands and the song starts for
// the world (level event 1010 with the song's minecraft:jukebox_song registry
// index); the next use gives it back above the block, stops the song (1011)
// and clears has_record.
func TestAJukeboxTakesADiscAndReturnsIt(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:jukebox"])
	cat := servertest.ItemStack(t, "minecraft:music_disc_cat", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, cat)
	w.Settle()

	songs, _ := registry.Get("minecraft:jukebox_song")
	songIndex := songs.ID("minecraft:cat")
	w.UseBlock(3, 64, 0)
	if x, y, z, data := awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxPlay); x != 3 || y != 64 || z != 0 || data != songIndex {
		t.Errorf("the play event = (%d,%d,%d, %d), want (3,64,0, %d)", x, y, z, data, songIndex)
	}
	if _, props := servertest.StatePropsOf(t, srv, 3, 64, 0); props["has_record"] != "true" {
		t.Errorf("the jukebox after the insert = %v, want has_record=true", props)
	}
	if got := srv.InventoryAt(3, 64, 0); len(got) != 1 || !got[0].Equal(cat) {
		t.Errorf("the jukebox holds %+v, want the cat disc", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Empty() {
		t.Errorf("the hand after a survival insert = %+v, want the disc spent", got)
	}

	w.UseBlock(3, 64, 0)
	if x, y, z, _ := awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxStop); x != 3 || y != 64 || z != 0 {
		t.Errorf("the stop event = (%d,%d,%d), want (3,64,0)", x, y, z)
	}
	if _, props := servertest.StatePropsOf(t, srv, 3, 64, 0); props["has_record"] != "false" {
		t.Errorf("the jukebox after the eject = %v, want has_record=false", props)
	}
	if got := srv.InventoryAt(3, 64, 0); len(got) != 1 || !got[0].Empty() {
		t.Errorf("the jukebox after the eject holds %+v, want it empty", got)
	}
	w.ExpectDrop(cat)
	items := srv.ItemsInDimension("minecraft:overworld")
	raised := false
	for _, it := range items {
		if it.Item == "minecraft:music_disc_cat" && it.Y > 64 {
			raised = true
		}
	}
	if !raised {
		t.Errorf("the ejected disc is not above the jukebox: %+v", items)
	}
}

// TestACreativeJukeboxKeepsTheDisc checks that a creative hand does not spend
// the disc it inserts.
func TestACreativeJukeboxKeepsTheDisc(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:jukebox"])
	cat := servertest.ItemStack(t, "minecraft:music_disc_cat", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, cat)
	w.Settle()

	w.UseBlock(3, 64, 0)
	awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxPlay)
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Equal(cat) {
		t.Errorf("the creative hand after the insert = %+v, want the disc kept", got)
	}
}

// TestAJukeboxStopsAtTheSongsEnd lays a jukebox at the tick its "11" record
// has finished - its length plus vanilla's twenty-tick end delay - and ticks
// it: the song stops (1011), the disc stays in and has_record keeps standing.
func TestAJukeboxStopsAtTheSongsEnd(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	srv.StopTick()

	song, ok := registry.JukeboxSongOf("minecraft:11")
	if !ok {
		t.Fatal("the registry has no 11 song")
	}
	servertest.Setblock(t, w.Client, 3, 64, 0, servertest.JukeboxCommand("11", song.LengthTicks+19))

	servertest.TickBlockEntitiesByHand(srv, 2)
	if x, y, z, _ := awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxStop); x != 3 || y != 64 || z != 0 {
		t.Errorf("the stop event = (%d,%d,%d), want (3,64,0)", x, y, z)
	}
	if _, props := servertest.StatePropsOf(t, srv, 3, 64, 0); props["has_record"] != "true" {
		t.Errorf("the jukebox after the song ended = %v, want has_record=true (the disc stays in)", props)
	}
	got := srv.InventoryAt(3, 64, 0)
	if len(got) != 1 || !got[0].Equal(servertest.ItemStack(t, "minecraft:music_disc_11", 1)) {
		t.Errorf("the jukebox after the song ended holds %+v, want the 11 disc", got)
	}
}

// TestAJukeboxComparatorReadsTheStoredDisc lays a jukebox whose song has
// already finished and a comparator beside it: the comparator reads the
// stored disc's song output, which does not fall when the song ends.
func TestAJukeboxComparatorReadsTheStoredDisc(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())

	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:jukebox"])
	servertest.PlaceFrom(w.Client, "minecraft:comparator", 2, 0, 180, 3, 64, 0, protocol.FaceNorth)
	if name, props := servertest.StatePropsOf(t, srv, 3, 64, -1); name != "minecraft:comparator" || props["facing"] != "south" {
		t.Fatalf("the comparator stands as %s %v, want one facing south into the jukebox", name, props)
	}
	// Lay the finished disc, then nudge the comparator so it reads again
	// after the tag has loaded the block entity.
	servertest.Setblock(t, w.Client, 3, 64, 0, servertest.JukeboxCommand("cat", 99999))
	servertest.Setblock(t, w.Client, 3, 65, -1, "minecraft:stone")

	servertest.WaitForTicks(t, srv, 40, func() bool {
		tag, ok := srv.BlockEntityTagAt(3, 64, -1)
		if !ok {
			return false
		}
		out, _ := nbt.Get[nbt.Int](tag, "OutputSignal")
		return out == 2
	})
}

// TestAJukeboxPowersRedstoneWhilePlaying places a lamp beside a playing
// jukebox: the lamp lights while the song runs and goes out when the disc is
// ejected.
func TestAJukeboxPowersRedstoneWhilePlaying(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())

	servertest.Setblock(t, w.Client, 5, 64, 0, servertest.JukeboxCommand("cat", 0))
	servertest.Setblock(t, w.Client, 5, 64, 1, "minecraft:redstone_lamp")
	servertest.Setblock(t, w.Client, 6, 64, 1, "minecraft:stone")

	servertest.ExpectBlockNamed(t, srv, 5, 64, 1, "minecraft:redstone_lamp", map[string]string{"lit": "true"})
	w.UseBlock(5, 64, 0)
	awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxStop)
	servertest.WaitForTicks(t, srv, 20, func() bool {
		_, props := servertest.StatePropsOf(t, srv, 5, 64, 1)
		return props["lit"] == "false"
	})
}

// TestAHopperFeedsAJukebox checks that a hopper above a jukebox pushes a disc
// into it, which starts the song.
func TestAHopperFeedsAJukebox(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())

	servertest.Setblock(t, w.Client, 4, 63, 0, "minecraft:jukebox")
	placeAHopperOnTheGround(t, srv, w, 4, 64, 0)
	srv.StopTick()

	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:music_disc_cat", 1)) {
		t.Fatal("the hopper above has no block entity to fill")
	}
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := srv.InventoryAt(4, 63, 0); len(got) != 1 || !got[0].Equal(servertest.ItemStack(t, "minecraft:music_disc_cat", 1)) {
		t.Errorf("the fed jukebox holds %+v, want the cat disc", got)
	}
	if _, props := servertest.StatePropsOf(t, srv, 4, 63, 0); props["has_record"] != "true" {
		t.Errorf("the fed jukebox = %v, want has_record=true", props)
	}
	if x, y, z, _ := awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxPlay); x != 4 || y != 63 || z != 0 {
		t.Errorf("the fed song's play event = (%d,%d,%d), want (4,63,0)", x, y, z)
	}
}

// TestAJukeboxKeepsItsDiscAcrossARestart lays a jukebox holding a disc partway
// through its song, saves the world and restarts: the disc and the ticks the
// song has run come back out of the region file.
func TestAJukeboxKeepsItsDiscAcrossARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, w := servertest.OpenWindowedOps(t, cfg)
	servertest.Setblock(t, w.Client, 3, 64, 0, servertest.JukeboxCommand("cat", 500))
	w.Disconnect()
	srv.Shutdown()

	back, w2 := servertest.OpenWindowedOps(t, cfg)
	defer w2.Disconnect()
	tag, ok := back.BlockEntityTagAt(3, 64, 0)
	if !ok {
		t.Fatal("the jukebox's block entity did not come back with the chunk")
	}
	if got, _ := nbt.Get[nbt.Long](tag, "ticks_since_song_started"); got < 500 {
		t.Errorf("the reloaded song ticks = %d, want at least 500", got)
	}
	if got := back.InventoryAt(3, 64, 0); len(got) != 1 || !got[0].Equal(servertest.ItemStack(t, "minecraft:music_disc_cat", 1)) {
		t.Errorf("the reloaded jukebox holds %+v, want the cat disc", got)
	}
	if _, props := servertest.StatePropsOf(t, back, 3, 64, 0); props["has_record"] != "true" {
		t.Errorf("the reloaded jukebox = %v, want has_record=true", props)
	}
}

// TestAHopperDrainsAJukebox checks that a hopper below pulls the disc out of a
// playing jukebox, which stops the song and clears has_record.
func TestAHopperDrainsAJukebox(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	srv.StopTick()

	servertest.Setblock(t, w.Client, 4, 64, 0, "minecraft:hopper[facing=down]")
	servertest.Setblock(t, w.Client, 4, 65, 0, servertest.JukeboxCommand("cat", 0))
	if _, props := servertest.StatePropsOf(t, srv, 4, 65, 0); props["has_record"] != "true" {
		t.Fatalf("the laid jukebox = %v, want has_record=true", props)
	}

	servertest.TickBlockEntitiesByHand(srv, 1)
	hopper := srv.InventoryAt(4, 64, 0)
	if len(hopper) != 5 || !hopper[0].Equal(servertest.ItemStack(t, "minecraft:music_disc_cat", 1)) {
		t.Errorf("the hopper below holds %+v, want the drained disc", hopper)
	}
	if _, props := servertest.StatePropsOf(t, srv, 4, 65, 0); props["has_record"] != "false" {
		t.Errorf("the drained jukebox = %v, want has_record=false", props)
	}
	if x, y, z, _ := awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxStop); x != 4 || y != 65 || z != 0 {
		t.Errorf("the drained song's stop event = (%d,%d,%d), want (4,65,0)", x, y, z)
	}
}

// jukeboxTicks reads a jukebox's saved ticks_since_song_started tag, present
// only while a song is under way, and reports whether it was.
func jukeboxTicks(t *testing.T, srv *server.Server, x, y, z int32) (int64, bool) {
	t.Helper()
	tag, ok := srv.BlockEntityTagAt(x, y, z)
	if !ok {
		t.Fatalf("no block entity at (%d,%d,%d)", x, y, z)
	}
	ticks, ok := nbt.Get[nbt.Long](tag, "ticks_since_song_started")
	return int64(ticks), ok
}

// TestAJukeboxPlaysPastTheSongsLength lays a jukebox at the tick its "11"
// record's own length ends and ticks it: the song keeps running through the
// twenty ticks vanilla's hasFinished waits past the length, and only the tick
// after that stops it.
func TestAJukeboxPlaysPastTheSongsLength(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	srv.StopTick()

	song, ok := registry.JukeboxSongOf("minecraft:11")
	if !ok {
		t.Fatal("the registry has no 11 song")
	}
	servertest.Setblock(t, w.Client, 5, 64, 0, servertest.JukeboxCommand("11", song.LengthTicks))
	if _, ok := jukeboxTicks(t, srv, 5, 64, 0); !ok {
		t.Fatal("the song stopped at its own length, want the twenty-tick end delay to run first")
	}

	servertest.TickBlockEntitiesByHand(srv, 20)
	if _, ok := jukeboxTicks(t, srv, 5, 64, 0); !ok {
		t.Fatal("the song stopped before the end delay ran out")
	}

	servertest.TickBlockEntitiesByHand(srv, 1)
	if x, y, z, _ := awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxStop); x != 5 || y != 64 || z != 0 {
		t.Errorf("the stop event = (%d,%d,%d), want (5,64,0)", x, y, z)
	}
	if _, ok := jukeboxTicks(t, srv, 5, 64, 0); ok {
		t.Error("the stopped song still carries its ticks tag")
	}
}

// TestAJukeboxLoadedWithoutTicksDoesNotPlay lays a jukebox holding a disc but
// no ticks_since_song_started, the shape a vanilla save leaves for a finished
// song: it powers nothing.
func TestAJukeboxLoadedWithoutTicksDoesNotPlay(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())

	servertest.Setblock(t, w.Client, 5, 64, 0,
		`minecraft:jukebox[has_record=true]{RecordItem:{id:"minecraft:music_disc_cat",count:1}}`)
	servertest.Setblock(t, w.Client, 5, 64, 1, "minecraft:redstone_lamp")
	servertest.ExpectBlockNamed(t, srv, 5, 64, 1, "minecraft:redstone_lamp", map[string]string{"lit": "false"})
}

// TestBreakingAJukeboxDropsItsDisc breaks a playing jukebox: the song stops
// (1011) and the disc is popped out above the block.
func TestBreakingAJukeboxDropsItsDisc(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	servertest.Setblock(t, w.Client, 4, 64, 0, servertest.JukeboxCommand("cat", 0))

	w.BreakBlock(4, 64, 0)
	if x, y, z, _ := awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxStop); x != 4 || y != 64 || z != 0 {
		t.Errorf("the stop event = (%d,%d,%d), want (4,64,0)", x, y, z)
	}
	w.ExpectDrop(servertest.ItemStack(t, "minecraft:music_disc_cat", 1))
	if _, ok := srv.BlockEntityKindAt(4, 64, 0); ok {
		t.Error("the broken jukebox still has a block entity")
	}
}

// TestADropperStartsAJukebox fills a dropper facing down into a jukebox and
// fires it: the disc lands in the slot and the song starts with its registry
// index.
func TestADropperStartsAJukebox(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())

	servertest.Setblock(t, w.Client, 4, 63, 0, "minecraft:jukebox")
	servertest.Setblock(t, w.Client, 4, 64, 0, "minecraft:dropper[facing=down]")
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:music_disc_cat", 1)) {
		t.Fatal("the dropper has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceBottom)

	if got := srv.InventoryAt(4, 63, 0); len(got) != 1 || !got[0].Equal(servertest.ItemStack(t, "minecraft:music_disc_cat", 1)) {
		t.Errorf("the fed jukebox holds %+v, want the cat disc", got)
	}
	if _, props := servertest.StatePropsOf(t, srv, 4, 63, 0); props["has_record"] != "true" {
		t.Errorf("the fed jukebox = %v, want has_record=true", props)
	}
	songs, _ := registry.Get("minecraft:jukebox_song")
	if x, y, z, data := awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxPlay); x != 4 || y != 63 || z != 0 || data != songs.ID("minecraft:cat") {
		t.Errorf("the fed song's play event = (%d,%d,%d, %d), want (4,63,0, %d)", x, y, z, data, songs.ID("minecraft:cat"))
	}
}

// TestAnExplosionStopsAJukebox destroys a playing jukebox with a blast: the
// song stops (1011) and the block entity is gone.
func TestAnExplosionStopsAJukebox(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	servertest.Setblock(t, w.Client, 4, 64, 0, servertest.JukeboxCommand("cat", 0))

	srv.RunExplosion(4.5, 64.5, 0.5, 4)
	if x, y, z, _ := awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxStop); x != 4 || y != 64 || z != 0 {
		t.Errorf("the stop event = (%d,%d,%d), want (4,64,0)", x, y, z)
	}
	if _, ok := srv.BlockEntityKindAt(4, 64, 0); ok {
		t.Error("the exploded jukebox still has a block entity")
	}
}

// TestAnExplosionPopsTheDiscAbove destroys a playing jukebox with a blast and
// reads where the disc landed: popped out above the block, not spilled.
// vanilla: JukeboxBlockEntity.popOutTheItem
func TestAnExplosionPopsTheDiscAbove(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	srv.StopTick()
	servertest.Setblock(t, w.Client, 4, 64, 0, servertest.JukeboxCommand("cat", 0))

	srv.RunExplosion(4.5, 64.5, 0.5, 4)

	var found bool
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item != "minecraft:music_disc_cat" {
			continue
		}
		found = true
		if math.Abs(it.Y-65.135) > 1e-6 {
			t.Errorf("the exploded disc stands at Y=%.4f, want it popped above the jukebox", it.Y)
		}
	}
	if !found {
		t.Fatal("the explosion dropped no disc")
	}
}

// TestAJukeboxEjectsAboveItself ejects a playing jukebox's disc and reads
// where it landed: just above the block's top face, jittered across it.
// vanilla: JukeboxBlockEntity.popOutTheItem
func TestAJukeboxEjectsAboveItself(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	srv.StopTick()
	servertest.Setblock(t, w.Client, 3, 64, 0, servertest.JukeboxCommand("cat", 0))

	w.UseBlock(3, 64, 0)
	awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxStop)

	var found bool
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item != "minecraft:music_disc_cat" {
			continue
		}
		found = true
		if math.Abs(it.X-3.5) > 0.36 || math.Abs(it.Z-0.5) > 0.36 || math.Abs(it.Y-65.135) > 1e-6 {
			t.Errorf("the ejected disc stands at (%.4f, %.4f, %.4f), want above the jukebox's centre", it.X, it.Y, it.Z)
		}
		if it.VY != 0.2 || math.Abs(it.VX) > 0.1 || math.Abs(it.VZ) > 0.1 {
			t.Errorf("the ejected disc moves at (%.4f, %.4f, %.4f), want the ItemEntity constructor's hop", it.VX, it.VY, it.VZ)
		}
	}
	if !found {
		t.Fatal("no cat disc was ejected")
	}
}

// TestAJukeboxEjectsADiscWithUnmodelledComponents lays a jukebox whose disc
// carries a component the server cannot show and right-clicks it: the disc
// still comes out, its base item having stayed in the slot all along.
func TestAJukeboxEjectsADiscWithUnmodelledComponents(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowedOps(t, servertest.BlockTestConfig())
	servertest.Setblock(t, w.Client, 3, 64, 0,
		`minecraft:jukebox[has_record=true]{RecordItem:{id:"minecraft:music_disc_cat",count:1,components:{"minecraft:written_book_content":{pages:[{raw:"hello"}]}}},ticks_since_song_started:0}`)

	w.UseBlock(3, 64, 0)
	awaitJukeboxEvent(t, w, protocol.WorldEventJukeboxStop)
	w.ExpectDrop(servertest.ItemStack(t, "minecraft:music_disc_cat", 1))
}
