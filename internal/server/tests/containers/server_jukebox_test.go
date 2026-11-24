package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
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

func TestSetblockJukeboxPlaybackTagLocksHopperBeforeFirstPull(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 4, 64, 0, "minecraft:hopper[facing=down]")
	servertest.Setblock(t, c, 4, 65, 0, `minecraft:jukebox[has_record=true]{RecordItem:{id:"minecraft:music_disc_cat",count:1}}`)
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 0); props["enabled"] != "true" {
		t.Fatalf("hopper below a silent jukebox = %v, want enabled=true", props)
	}

	servertest.Setblock(t, c, 4, 65, 0, servertest.JukeboxCommand("cat", 0))
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 0); props["enabled"] != "false" {
		t.Errorf("hopper after the playback tag and before stepping = %v, want enabled=false", props)
	}
	servertest.RunTicks(t, srv, 12)
	disc := servertest.ItemStack(t, "minecraft:music_disc_cat", 1)
	if got := srv.InventoryAt(4, 65, 0); len(got) != 1 || !got[0].Equal(disc) {
		t.Errorf("jukebox after 12 ticks holds %+v, want the cat disc", got)
	}
	for i, stack := range srv.InventoryAt(4, 64, 0) {
		if !stack.Empty() {
			t.Errorf("hopper slot %d holds %+v, want it empty", i, stack)
		}
	}
}

func TestSetblockJukeboxRecordTagWakesComparatorThroughStone(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 4, 64, 0, "minecraft:stone")
	servertest.Setblock(t, c, 4, 65, 1, "minecraft:stone")
	servertest.Setblock(t, c, 4, 65, 2, `minecraft:jukebox[has_record=true]{RecordItem:{id:"minecraft:music_disc_cat",count:1}}`)
	servertest.Setblock(t, c, 4, 65, 0, "minecraft:comparator[facing=south]")
	output := func() nbt.Int {
		tag, _ := srv.BlockEntityTagAt(4, 65, 0)
		out, _ := nbt.Get[nbt.Int](tag, "OutputSignal")
		return out
	}
	servertest.RunTicks(t, srv, 4)
	if got := output(); got != 2 {
		t.Fatalf("comparator reading cat through stone = %d, want 2", got)
	}

	servertest.Setblock(t, c, 4, 65, 2, `minecraft:jukebox[has_record=true]{RecordItem:{id:"minecraft:music_disc_13",count:1}}`)
	servertest.RunTicks(t, srv, 4)
	if got := output(); got != 1 {
		t.Errorf("comparator after the disc changes to 13 = %d, want 1", got)
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

// TestAPlayingJukeboxPlacedWithATagLocksTheHopperBelow places a jukebox whose
// tag already has a song running above a hopper: the tag loads before the
// neighbour wake, so the hopper reads the 15 and stays disabled with the disc
// above it. A stopped record leaves the hopper enabled to pull it.
func TestAPlayingJukeboxPlacedWithATagLocksTheHopperBelow(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	for _, tc := range []struct {
		name      string
		x         int32
		ticks     int64
		enabled   string
		inHopper  bool
		hasRecord string
	}{{"playing", 3, 0, "false", false, "true"}, {"stopped", 8, 99999, "true", true, "false"}} {
		servertest.Setblock(t, c, tc.x, 64, 0, "minecraft:hopper[facing=down]")
		servertest.Setblock(t, c, tc.x, 65, 0, servertest.JukeboxCommand("cat", tc.ticks))
		if _, props := servertest.StatePropsOf(t, srv, tc.x, 64, 0); props["enabled"] != tc.enabled {
			t.Errorf("%s: hopper = %v, want enabled=%s", tc.name, props, tc.enabled)
		}
		servertest.RunTicks(t, srv, 1)
		if got := srv.BlockEntityStack(tc.x, 64, 0, 0).Count > 0; got != tc.inHopper {
			t.Errorf("%s: disc in hopper = %v, want %v", tc.name, got, tc.inHopper)
		}
		if _, props := servertest.StatePropsOf(t, srv, tc.x, 65, 0); props["has_record"] != tc.hasRecord {
			t.Errorf("%s: jukebox = %v, want has_record=%s", tc.name, props, tc.hasRecord)
		}
	}
}

// TestStrictSetblockLoadsTheBlockEntityTag places a chest with items in strict
// mode, which skips the neighbour wake but still loads the tag.
func TestStrictSetblockLoadsTheBlockEntityTag(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	servertest.Setblock(t, c, 3, 64, 0, `minecraft:chest{Items:[{Slot:0b,id:"minecraft:diamond",count:1}]} strict`)
	if got := srv.BlockEntityStack(3, 64, 0, 0); got.Count != 1 || registry.Item(got.ItemID) != registry.ItemDiamond {
		t.Errorf("slot 0 = %+v, want a diamond", got)
	}
}

func TestSetblockTagSurvivesReplacingAMovingPiston(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	servertest.Setblock(t, c, 3, 64, 0, `minecraft:moving_piston[facing=east]`)
	servertest.Setblock(t, c, 3, 64, 0, `minecraft:chest{Items:[{Slot:0b,id:"minecraft:diamond",count:1}]}`)
	if got := srv.BlockEntityStack(3, 64, 0, 0); got.Count != 1 || registry.Item(got.ItemID) != registry.ItemDiamond {
		t.Errorf("slot 0 = %+v, want a diamond", got)
	}
}

// TestReplacingAPoweredLeverWithAPlayingJukeboxKeepsTheHopperLocked replaces
// the powered lever locking a hopper with a jukebox whose song is running: the
// lever's removal reactions run after the tag loads, so the hopper never
// unlocks and an observer watching it does not pulse.
func TestReplacingAPoweredLeverWithAPlayingJukeboxKeepsTheHopperLocked(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	servertest.Setblock(t, c, 3, 64, 0, "minecraft:hopper[facing=down]")
	servertest.Setblock(t, c, 3, 65, 0, "minecraft:lever[face=floor,powered=true]")
	servertest.Setblock(t, c, 4, 64, 0, "minecraft:observer[facing=west]")
	servertest.RunTicks(t, srv, 6)
	servertest.Setblock(t, c, 3, 65, 0, servertest.JukeboxCommand("cat", 0))
	servertest.RunTicks(t, srv, 2)
	if _, props := servertest.StatePropsOf(t, srv, 4, 64, 0); props["powered"] != "false" {
		t.Errorf("observer = %v, want unpowered: the hopper unlocked and relocked", props)
	}
	if _, props := servertest.StatePropsOf(t, srv, 3, 64, 0); props["enabled"] != "false" {
		t.Errorf("hopper = %v, want enabled=false", props)
	}
}
