package items

// The explorer maps (woodland_mansion_map and the rest) are MapItems like
// filled_map: held or framed they send their map's data, they clone in a
// crafting grid into more of themselves, and a cartography table locks them
// but, outside #extendable_maps, does not zoom them.
// vanilla: Items.WOODLAND_MANSION_MAP (MapItem::new), CartographyTableMenu

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// mansionMap is a woodland mansion map of the given map id.
func mansionMap(t *testing.T, id int32, count int32) protocol.ItemStack {
	t.Helper()
	stack := servertest.ItemStack(t, "minecraft:woodland_mansion_map", count)
	stack.SetMapID(id)
	return stack
}

func TestAGivenExplorerMapShowsItsSavedData(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	colours := mapColours()
	servertest.WriteMapSavedData(t, cfg.LevelName, 0, 1, true, colours)
	_, c, _ := servertest.JoinAs(t, cfg, "Explorer", servertest.OpsListing(t, "Explorer"))

	c.ChatCommand("give @s minecraft:woodland_mansion_map[map_id=0]")
	update := c.ExpectMapData(t, 0)
	if update.Scale != 1 || !update.Locked {
		t.Errorf("the map's scale = %d, locked %v; want the file's 1, locked", update.Scale, update.Locked)
	}
}

func TestAnItemFrameShowsTheExplorerMapItHolds(t *testing.T) {
	t.Parallel()
	frameShowsMap(t, "minecraft:woodland_mansion_map")
}

func TestEmptyMapsCloneAnExplorerMap(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, mansionMap(t, 7, 1))
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:map", 1))
	w.PutInGrid(36, 1)
	w.PutInGrid(37, 2)

	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, 1: {}, 2: {}}, mansionMap(t, 7, 2))
}

// TestTheCartographyTableLocksButDoesNotZoomAnExplorerMap lays an explorer
// map beside paper, which offers nothing as the map is not
// #extendable_maps, then beside a glass pane, which offers it locked.
// vanilla: CartographyTableMenu.setupResultSlot
func TestTheCartographyTableLocksButDoesNotZoomAnExplorerMap(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WriteMapSavedData(t, cfg.LevelName, 0, 1, false, mapColours())
	_, w := servertest.OpenWindowed(t, cfg)
	// The table's window: the map at 0, the addition at 1, the result at 2,
	// the hotbar from 30.
	const mapSlot, addSlot, resultSlot, hotbar0 = int16(0), int16(1), int16(2), int16(30)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:cartography_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectOpenTitle(protocol.MenuCartography, -1, 4, 64, 0)

	explorer := mansionMap(t, 0, 1)
	paper := servertest.ItemStack(t, "minecraft:paper", 1)
	pane := servertest.ItemStack(t, "minecraft:glass_pane", 1)
	w.SetCreativeSlot(36, explorer)
	w.SetCreativeSlot(37, paper)
	w.SetCreativeSlot(38, pane)

	w.Click(hotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{hotbar0: {}}, explorer)
	w.Click(mapSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{mapSlot: explorer}, protocol.ItemStack{})

	// Paper: the map slot took the explorer map, but no zoom is offered.
	w.Click(hotbar0+1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{hotbar0 + 1: {}}, paper)
	w.Click(addSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResultTap(map[int16]protocol.ItemStack{addSlot: paper}, protocol.ItemStack{}, func(id int32, data []byte) {
		if id != protocol.IDPlayContainerSlot {
			return
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadVarInt()
		if slot := r.ReadI16(); slot == resultSlot {
			if stack, _ := r.ReadSlot(); !stack.Empty() {
				t.Errorf("paper beside an explorer map offered %+v, want nothing", stack)
			}
		}
	})
	w.Click(addSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{addSlot: {}}, paper)
	w.Click(hotbar0+1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{hotbar0 + 1: paper}, protocol.ItemStack{})

	// A glass pane: the explorer map, marked for the lock.
	locked := explorer
	locked.SetMapPostProcessing(protocol.MapPostProcessingLock)
	w.Click(hotbar0+2, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{hotbar0 + 2: {}}, pane)
	w.Click(addSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{addSlot: pane, resultSlot: locked}, protocol.ItemStack{})
}
