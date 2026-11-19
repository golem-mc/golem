package items

// The empty map item: a right-click spends it and makes a fresh filled map
// centred on the area the player stands in — the item conversion, the face
// drawn from the terrain around them and the cartography table's take
// sound.

import (
	"errors"
	"fmt"
	"github.com/golem-mc/golem/internal/server/servertest"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// mapFilePath is one saved map file under a world's root: the shared
// data/minecraft/maps folder the server-wide map data lives in.
func mapFilePath(level, name string) string {
	return filepath.Join(level, "data", "minecraft", "maps", name)
}

// readSavedMap reads one saved map file's data compound, the shape the
// MapItemSavedData codec writes inside the {DataVersion, data} root.
func readSavedMap(t *testing.T, level, name string) nbt.Compound {
	t.Helper()
	root, _, err := servertest.ReadPlayerFileData(mapFilePath(level, name))
	if err != nil {
		t.Fatalf("read the saved map %s: %v", name, err)
	}
	data, ok := nbt.Get[nbt.Compound](root, "data")
	if !ok {
		t.Fatalf("the saved map %s has no data compound", name)
	}
	return data
}

// TestAnEmptyMapBecomesAFilledOneAtThePlayer: right-clicking an empty map in
// survival spends it, the cartography table's take sound plays from the
// player in the players category to the player as well, Map Item Data for
// the stone ground they stand on arrives with the holder's own marker on
// it, and the hand ends up holding the filled map that names id 0 — the
// world's first. The drawn face and the id counter reach the disk on the
// next save.
// vanilla: EmptyMapItem.use
func TestAnEmptyMapBecomesAFilledOneAtThePlayer(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, c := servertest.Join(t, cfg, "Cartographer")

	if pos := srv.PlayerPosition("Cartographer"); pos.X != 0.5 || pos.Z != 0.5 {
		t.Fatalf("the cartographer stands at (%v, %v), want the spawn (0.5, 0.5)", pos.X, pos.Z)
	}

	empty := servertest.ItemID(t, "minecraft:map")
	srv.GiveSlot("Cartographer", player.HotbarSlot, protocol.ItemStack{ItemID: empty, Count: 1})
	c.Settle()

	c.UseItemRightClicks(protocol.HandMain)

	heard := false
	gotMap := false
	var update servertest.MapDataUpdate
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		switch id {
		case protocol.IDPlayEntitySoundEffect:
			name, category, entityID, volume, pitch := servertest.ReadEntitySound(t, data)
			if name != "minecraft:ui.cartography_table.take_result" {
				continue
			}
			heard = true
			if category != protocol.SoundCategoryPlayer {
				t.Errorf("the take sound's category = %d, want the players category", category)
			}
			if entityID != c.PlayerEntityID {
				t.Errorf("the take sound follows entity %d, want the clicker's %d", entityID, c.PlayerEntityID)
			}
			if volume != 1 || pitch != 1 {
				t.Errorf("the take sound plays at volume %v pitch %v, want 1 and 1", volume, pitch)
			}
		case protocol.IDPlayMapData:
			if parsed := servertest.ParseMapData(t, data); parsed.ID() == 0 {
				update, gotMap = parsed, true
			}
		}
		if heard && gotMap {
			break
		}
	}
	if !heard {
		t.Error("the cartography table's take sound never played for the click")
	}
	if !gotMap {
		t.Fatal("no Map Item Data arrived for the map the empty map made")
	}
	if update.Scale != 0 {
		t.Errorf("the new map's scale = %d, want 0", update.Scale)
	}
	// The player stands at (0.5, 0.5), so their marker sits one pixel in from
	// the corner of the (0, 0)-centred scale-0 face, pointing the way they
	// face.
	if len(update.Icons) != 1 {
		t.Fatalf("the new map carries %d markers, want the holder's one", len(update.Icons))
	}
	if icon := update.Icons[0]; icon.TypeID != world.DecorationPlayer || icon.X != 1 || icon.Y != 1 || icon.Rot != 0 {
		t.Errorf("the holder's marker = %+v, want a player marker at (1, 1) facing 0", icon)
	}

	held := srv.InventorySlot("Cartographer", player.HotbarSlot)
	if filled := servertest.ItemID(t, "minecraft:filled_map"); held.ItemID != filled || held.Count != 1 {
		t.Errorf("the hand after the click = %+v, want one filled map", held)
	} else if id, ok := held.MapIDOf(); !ok || id != 0 {
		t.Errorf("the map in the hand names id %d (carried %v), want the first id 0", id, ok)
	}

	if !srv.SaveAll(false) {
		t.Fatal("save-all reported failure")
	}
	saved := readSavedMap(t, cfg.LevelName, "0.dat")
	if dimension, ok := nbt.Get[nbt.String](saved, "dimension"); !ok || string(dimension) != world.DimensionName {
		t.Errorf("the saved map's dimension = %q (present %v), want the overworld", dimension, ok)
	}
	if x, _ := nbt.IntValue(saved["xCenter"]); x != 0 {
		t.Errorf("the saved map's xCenter = %d, want 0", x)
	}
	if z, _ := nbt.IntValue(saved["zCenter"]); z != 0 {
		t.Errorf("the saved map's zCenter = %d, want 0", z)
	}
	if colours, ok := nbt.Get[nbt.ByteArray](saved, "colors"); !ok || len(colours) != servertest.MapSide*servertest.MapSide {
		t.Errorf("the saved map keeps %d colour bytes (present %v), want %d", len(colours), ok, servertest.MapSide*servertest.MapSide)
	} else {
		const stone = 11
		for i, colour := range colours {
			if colour>>2 != stone {
				t.Errorf("the saved map's pixel %d = %d, want stone (%d) with a brightness", i, colour, stone)
				break
			}
		}
	}
	counter := readSavedMap(t, cfg.LevelName, "last_id.dat")
	if last, ok := nbt.IntValue(counter["map"]); !ok || last != 0 {
		t.Errorf("the map id counter = %d (present %v), want the last allocated 0", last, ok)
	}
}

// TestACreativePlayerKeepsTheEmptyMapAndGainsAFilledOne: a creative player's
// empty map is not spent — their materials are infinite — and the fresh
// filled map lands in their inventory beside it, naming the same new id.
// vanilla: EmptyMapItem.use
func TestACreativePlayerKeepsTheEmptyMapAndGainsAFilledOne(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Cartographer")

	empty := servertest.ItemID(t, "minecraft:map")
	srv.GiveSlot("Cartographer", player.HotbarSlot, protocol.ItemStack{ItemID: empty, Count: 1})
	c.Settle()

	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()

	if held := srv.InventorySlot("Cartographer", player.HotbarSlot); held.ItemID != empty || held.Count != 1 {
		t.Errorf("the hand after the click = %+v, want the empty map still there", held)
	}
	filled := servertest.ItemID(t, "minecraft:filled_map")
	found := false
	for slot := player.MainSlot; slot <= player.OffHandSlot; slot++ {
		stack := srv.InventorySlot("Cartographer", slot)
		if stack.ItemID != filled {
			continue
		}
		found = true
		if id, ok := stack.MapIDOf(); !ok || id != 0 {
			t.Errorf("the filled map in slot %d names id %d (carried %v), want 0", slot, id, ok)
		}
	}
	if !found {
		t.Error("no filled map landed in the creative player's inventory")
	}
}

// TestASpectatorMakesNoMap: an empty map in a spectator's hand does nothing —
// no sound, no Map Item Data, the hand unchanged and no map file written.
// vanilla: ServerPlayerGameMode.useItem
func TestASpectatorMakesNoMap(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, ghost := servertest.Join(t, cfg, "Ghost", servertest.OpsListing(t, "Ghost"))

	ghost.ChatCommand("gamemode spectator")
	ghost.Settle()
	if srv.PlayerGameMode("Ghost") != protocol.GameModeSpectator {
		t.Fatal("the switch to spectator applies")
	}

	empty := servertest.ItemID(t, "minecraft:map")
	srv.GiveSlot("Ghost", player.HotbarSlot, protocol.ItemStack{ItemID: empty, Count: 1})
	ghost.Settle()

	ghost.UseItemRightClicks(protocol.HandMain)
	ghost.SettleAndCollect(func(id int32, _ []byte) {
		switch id {
		case protocol.IDPlayEntitySoundEffect:
			t.Error("a spectator's empty map played a sound")
		case protocol.IDPlayMapData:
			t.Error("a spectator's empty map sent Map Item Data")
		}
	})

	if held := srv.InventorySlot("Ghost", player.HotbarSlot); held.ItemID != empty || held.Count != 1 {
		t.Errorf("the hand after the click = %+v, want the empty map untouched", held)
	}
	if !srv.SaveAll(false) {
		t.Fatal("save-all reported failure")
	}
	if _, err := os.Stat(mapFilePath(cfg.LevelName, "0.dat")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("a spectator's map was saved: stat error %v, want no file", err)
	}
}

// TestAMapMadeInTheNetherNamesThatDimension: the map an empty map makes keeps
// the dimension it was made in, so the face it draws and the markers it
// tracks are the nether's own.
// vanilla: EmptyMapItem.use, MapItemSavedData.createFresh
func TestAMapMadeInTheNetherNamesThatDimension(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, c := servertest.Join(t, cfg, "Cartographer")

	srv.MovePlayerToDimension("Cartographer", world.NetherName, 8.5, 64, 8.5)
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.PlayerDimension("Cartographer") == world.NetherName
	})
	c.Settle()

	empty := servertest.ItemID(t, "minecraft:map")
	srv.GiveSlot("Cartographer", player.HotbarSlot, protocol.ItemStack{ItemID: empty, Count: 1})
	c.Settle()

	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()

	if held := srv.InventorySlot("Cartographer", player.HotbarSlot); held.ItemID != servertest.ItemID(t, "minecraft:filled_map") {
		t.Fatalf("the hand after the click = %+v, want a filled map", held)
	}
	if !srv.SaveAll(false) {
		t.Fatal("save-all reported failure")
	}
	saved := readSavedMap(t, cfg.LevelName, "0.dat")
	if dimension, ok := nbt.Get[nbt.String](saved, "dimension"); !ok || string(dimension) != world.NetherName {
		t.Errorf("the saved map's dimension = %q (present %v), want the nether", dimension, ok)
	}
	if x, _ := nbt.IntValue(saved["xCenter"]); x != 0 {
		t.Errorf("the saved map's xCenter = %d, want 0 for the (8, 8) area", x)
	}
	if z, _ := nbt.IntValue(saved["zCenter"]); z != 0 {
		t.Errorf("the saved map's zCenter = %d, want 0 for the (8, 8) area", z)
	}
}

// TestAStackOfEmptyMapsMakesOneMapPerUse: a survival stack of two spends one
// map per click, each map counting on from the last, and each takes the centre
// of the area the player's block position falls in.
// vanilla: EmptyMapItem.use, MapItemSavedData.createFresh
func TestAStackOfEmptyMapsMakesOneMapPerUse(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, c := servertest.Join(t, cfg, "Cartographer")

	c.MoveTo(100.5, 64, 100.5, 0, 0)
	c.Settle()
	if pos := srv.PlayerPosition("Cartographer"); pos.X != 100.5 || pos.Z != 100.5 {
		t.Fatalf("the cartographer stands at (%v, %v), want (100.5, 100.5)", pos.X, pos.Z)
	}

	empty := servertest.ItemID(t, "minecraft:map")
	filled := servertest.ItemID(t, "minecraft:filled_map")
	srv.GiveSlot("Cartographer", player.HotbarSlot, protocol.ItemStack{ItemID: empty, Count: 2})
	c.Settle()

	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	if held := srv.InventorySlot("Cartographer", player.HotbarSlot); held.ItemID != empty || held.Count != 1 {
		t.Fatalf("the hand after the first click = %+v, want one empty map left", held)
	}

	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	held := srv.InventorySlot("Cartographer", player.HotbarSlot)
	if held.ItemID != filled {
		t.Fatalf("the hand after the second click = %+v, want a filled map", held)
	}
	if id, ok := held.MapIDOf(); !ok || id != 1 {
		t.Errorf("the map in the hand names id %d (carried %v), want the second id 1", id, ok)
	}

	if !srv.SaveAll(false) {
		t.Fatal("save-all reported failure")
	}
	for id, want := range map[int32]int32{0: 128, 1: 128} {
		saved := readSavedMap(t, cfg.LevelName, fmt.Sprintf("%d.dat", id))
		if x, _ := nbt.IntValue(saved["xCenter"]); x != want {
			t.Errorf("map %d's xCenter = %d, want %d", id, x, want)
		}
		if z, _ := nbt.IntValue(saved["zCenter"]); z != want {
			t.Errorf("map %d's zCenter = %d, want %d", id, z, want)
		}
	}
}

// TestTheMapCounterSurvivesARestart: the id counter a save writes is where a
// restarted server carries on, so the next empty map does not reuse an id.
// vanilla: MapIndex
func TestTheMapCounterSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	srv, c := servertest.Join(t, cfg, "Cartographer")

	empty := servertest.ItemID(t, "minecraft:map")
	srv.GiveSlot("Cartographer", player.HotbarSlot, protocol.ItemStack{ItemID: empty, Count: 1})
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	if !srv.SaveAll(false) {
		t.Fatal("save-all reported failure")
	}
	srv.Shutdown()

	restarted := *cfg
	againSrv, again := servertest.Join(t, &restarted, "Cartographer")
	againSrv.GiveSlot("Cartographer", player.HotbarSlot, protocol.ItemStack{ItemID: empty, Count: 1})
	again.Settle()
	again.UseItemRightClicks(protocol.HandMain)
	again.Settle()

	held := againSrv.InventorySlot("Cartographer", player.HotbarSlot)
	if id, ok := held.MapIDOf(); held.ItemID != servertest.ItemID(t, "minecraft:filled_map") || !ok || id != 1 {
		t.Errorf("the map after the restart = %+v (id %d, carried %v), want a filled map naming id 1", held, id, ok)
	}
}
