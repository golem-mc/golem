package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
)

// The cartography table's tests: the window its block opens with the
// title the table's own, the rules its two slots take by, and the three
// recipes a map and whatever lays beside it make - the paper's zoom out,
// the empty map's clone, the glass pane's lock - with the take that
// spends one of each input and plays the table's own sound.

// The cartography table's own slots and the player's hotbar in its
// window: the map at 0, the addition at 1, the result at 2, the player's
// things from 3.
const (
	cartMapSlot    = int16(0)
	cartAddSlot    = int16(1)
	cartResultSlot = int16(2)
	cartHotbar0    = int16(3 + (player.HotbarSlot - player.MainSlot))
)

// openCartography places the table at (4,64,0) from a creative client's
// hotbar, right-clicks it, and reads the window it opens with its title.
func openCartography(t *testing.T, w *servertest.WindowedClient) ([]protocol.ItemStack, protocol.Text) {
	t.Helper()
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:cartography_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	return w.ExpectOpenTitle(protocol.MenuCartography, -1, 4, 64, 0)
}

// openCartographySurvival places the table out of a survival player's
// held stack, which the placement spends, and reads the window its
// right-click opens with its title.
func openCartographySurvival(t *testing.T, srv *server.Server, w *servertest.WindowedClient) ([]protocol.ItemStack, protocol.Text) {
	t.Helper()
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:cartography_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.SettleAndCollect(func(int32, []byte) {})
	w.UseBlock(4, 64, 0)
	return w.ExpectOpenTitle(protocol.MenuCartography, -1, 4, 64, 0)
}

// cartMapID is the map the tests lay in the table, saved by withSavedMap. A
// test world has no id counter, so the maps the table's takes make are
// numbered from 0.
const cartMapID = 7

// withSavedMap gives cfg a fresh world holding the saved maps of the given
// ids, blank, each at scale and locked as asked.
func withSavedMap(t *testing.T, cfg *config.Config, scale byte, locked bool, ids ...int32) *config.Config {
	t.Helper()
	cfg.LevelName = t.TempDir()
	for _, id := range ids {
		servertest.WriteMapSavedData(t, cfg.LevelName, id, scale, locked, make([]byte, servertest.MapSide*servertest.MapSide))
	}
	return cfg
}

// filledMap is a filled map of cartMapID: the table's map slot takes only a
// stack with an id.
func filledMap(t *testing.T, count int32) protocol.ItemStack {
	t.Helper()
	return mapOf(t, count, cartMapID)
}

// mapOf is a filled map of the given id.
func mapOf(t *testing.T, count, id int32) protocol.ItemStack {
	t.Helper()
	m := servertest.ItemStack(t, "minecraft:filled_map", count)
	m.SetMapID(id)
	return m
}

// expectMadeMap places the map a take left on the cursor into the hotbar
// slot, and reads the Map Item Data the server then sends for its id.
func expectMadeMap(t *testing.T, w *servertest.WindowedClient, hotbar int16, made protocol.ItemStack) servertest.MapDataUpdate {
	t.Helper()
	id, _ := made.MapIDOf()
	var update *servertest.MapDataUpdate
	w.Click(hotbar, 0, protocol.ClickModePickup)
	w.ExpectClickResultTap(map[int16]protocol.ItemStack{hotbar: made}, protocol.ItemStack{}, func(packetID int32, data []byte) {
		if packetID == protocol.IDPlayMapData {
			if u := servertest.ParseMapData(t, data); u.ID() == id {
				update = &u
			}
		}
	})
	if update != nil {
		return *update
	}
	return w.ExpectMapData(t, id)
}

// markedMap is a filled map marked for the table's zoom out (scale) or
// its lock, the stack its result shows for those two recipes.
func markedMap(t *testing.T, count int32, mark int32) protocol.ItemStack {
	t.Helper()
	m := filledMap(t, count)
	m.SetMapPostProcessing(mark)
	return m
}

// feedCartography lays the given stacks in the window's two input slots
// out of the first two hotbar slots, reading each move's corrections; the
// result the laid pair makes shows with the addition's own placement.
func feedCartography(t *testing.T, w *servertest.WindowedClient, maps, addition, result protocol.ItemStack) {
	t.Helper()
	w.SetCreativeSlot(36, maps)
	w.SetCreativeSlot(37, addition)
	w.Click(cartHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartHotbar0: {}}, maps)
	w.Click(cartMapSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartMapSlot: maps}, protocol.ItemStack{})
	w.Click(cartHotbar0+1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartHotbar0 + 1: {}}, addition)
	w.Click(cartAddSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		cartAddSlot:    addition,
		cartResultSlot: result,
	}, protocol.ItemStack{})
}

// expectNothingFromTheTable drains what the server queued after an
// exchange that should have changed nothing, and fails if a window
// packet - a slot correction, a property or the take's sound - says
// otherwise.
func expectNothingFromTheTable(w *servertest.WindowedClient) {
	w.T.Helper()
	w.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySoundEffect:
			r := protocol.NewReader(data)
			sound := r.ReadVarInt()
			var name string
			if sound == 0 {
				name = r.ReadString(32767)
			} else {
				name, _ = servertest.SoundEventName(sound - 1)
			}
			if name == "minecraft:ui.cartography_table.take_result" {
				w.T.Error("a take the table had nothing for still played its sound")
			}
		case protocol.IDPlayContainerProperty, protocol.IDPlayContainerSlot:
			w.T.Errorf("a dead window exchange changed the window (packet 0x%02X)", id)
		}
	})
}

// TestTheCartographyTableOpensItsThreeSlotWindow right-clicks the block
// and reads the window it opens: the table's own menu numbering its map,
// addition and result ahead of the player's things, and its title.
func TestTheCartographyTableOpensItsThreeSlotWindow(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	slots, title := openCartography(t, w)
	if len(slots) != 3+36 {
		t.Fatalf("the cartography window has %d slots, want its 3 and the player's 36", len(slots))
	}
	if title.Translate != "container.cartography_table" {
		t.Errorf("the cartography window's title = %+v, want the table's own", title)
	}

	// Away from a map the window is its empty self: nothing made, the
	// result holding nothing the empty inputs could.
	for _, slot := range slots[:3] {
		if !slot.Empty() {
			t.Errorf("the opening window's slot holds %+v, want the empty table", slot)
		}
	}
}

// TestTheCartographyTablesSlotsRefuseWhatTheyDoNotTake checks the slots'
// own gate over the wire: the map slot takes only a filled map, the
// addition the paper, the empty map and the glass pane its recipes ask,
// and the result takes nothing at all - the cursor keeping whatever the
// refused click held.
// vanilla: CartographyTableMenu's slots' mayPlace
func TestTheCartographyTablesSlotsRefuseWhatTheyDoNotTake(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	openCartography(t, w)

	// The paper round: the map slot refuses it under either button, and
	// the cursor keeps all of it; the window answers nothing.
	paper := servertest.ItemStack(t, "minecraft:paper", 1)
	w.SetCreativeSlot(36, paper)
	w.Click(cartHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartHotbar0: {}}, paper)
	for _, button := range []int8{0, 1} {
		w.Click(cartMapSlot, button, protocol.ClickModePickup)
		expectNothingFromTheTable(w)
	}
	// The paper goes home before the next round.
	w.Click(cartHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartHotbar0: paper}, protocol.ItemStack{})

	// The empty map round: the map slot refuses it too, though the
	// addition takes it - one of the held two for a right click, and
	// nothing made of it with no map to clone.
	emptyMaps := servertest.ItemStack(t, "minecraft:map", 2)
	w.SetCreativeSlot(36, emptyMaps)
	w.Click(cartHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartHotbar0: {}}, emptyMaps)
	w.Click(cartMapSlot, 0, protocol.ClickModePickup)
	expectNothingFromTheTable(w)
	w.Click(cartAddSlot, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		cartAddSlot: servertest.ItemStack(t, "minecraft:map", 1),
	}, servertest.ItemStack(t, "minecraft:map", 1))
	// The laid one and the held one merge into the addition for the
	// follow-up click carrying its twin, the cursor putting it away.
	w.Click(cartAddSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartAddSlot: emptyMaps}, protocol.ItemStack{})
	// The addition gives its two back to an empty cursor, and they go
	// home with it.
	w.Click(cartAddSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartAddSlot: {}}, emptyMaps)
	w.Click(cartHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartHotbar0: emptyMaps}, protocol.ItemStack{})

	// The filled map round: neither slot takes it but the map slot, and
	// right-clicking lays one of the held two there with nothing made
	// of it - the addition holds nothing for it to answer.
	maps := filledMap(t, 2)
	w.SetCreativeSlot(37, maps)
	w.Click(cartHotbar0+1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartHotbar0 + 1: {}}, maps)
	w.Click(cartAddSlot, 0, protocol.ClickModePickup)
	expectNothingFromTheTable(w)
	w.Click(cartMapSlot, 1, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		cartMapSlot: filledMap(t, 1),
	}, filledMap(t, 1))
	// The map goes home the way the empty maps did, the held twin
	// merging into the laid one and both going back to the hotbar.
	w.Click(cartMapSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartMapSlot: maps}, protocol.ItemStack{})
	w.Click(cartMapSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartMapSlot: {}}, maps)
	w.Click(cartHotbar0+1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartHotbar0 + 1: maps}, protocol.ItemStack{})

	// The result takes nothing at all: an empty cursor finds nothing to
	// take, and one holding the maps lays none of them in it.
	w.Click(cartResultSlot, 0, protocol.ClickModePickup)
	expectNothingFromTheTable(w)
	w.Click(cartHotbar0+1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartHotbar0 + 1: {}}, maps)
	w.Click(cartResultSlot, 1, protocol.ClickModePickup)
	expectNothingFromTheTable(w)
	w.Click(cartResultSlot, 0, protocol.ClickModePickup)
	expectNothingFromTheTable(w)
}

// TestPaperZoomsTheCartographyTableMap lays a filled map and paper in the
// window's two slots, and takes the answer the two make: the result
// holds a copy of the map marked for the zoom out, the take spends one of
// each input and plays the table's sound, and the result keeps offering
// the same answer until the last piece of either input takes it away.
func TestPaperZoomsTheCartographyTableMap(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, withSavedMap(t, servertest.BlockTestConfig(), 0, false, cartMapID))

	openCartography(t, w)

	// Two maps over two paper, laid in the window's own slots.
	maps := filledMap(t, 2)
	paper := servertest.ItemStack(t, "minecraft:paper", 2)
	zoomed := markedMap(t, 1, protocol.MapPostProcessingScale)
	feedCartography(t, w, maps, paper, zoomed)

	// The take: a new map onto the cursor - the table's mark stripped, a
	// fresh id for the zoomed copy - one of each input spent, and the result
	// still offering the same marked answer, no correction for a slot that
	// did not move.
	w.Click(cartResultSlot, 0, protocol.ClickModePickup)
	expectCartographyTake(w, map[int16]protocol.ItemStack{
		cartMapSlot: filledMap(t, 1),
		cartAddSlot: servertest.ItemStack(t, "minecraft:paper", 1),
	}, mapOf(t, 1, 0))

	// The map goes home before the second take leaves room for it, and the
	// client is sent its data: one scale out from the input's.
	if update := expectMadeMap(t, w, cartHotbar0+2, mapOf(t, 1, 0)); update.Scale != 1 || update.Locked {
		t.Errorf("the zoomed map's data has scale %d locked %v, want scale 1 unlocked", update.Scale, update.Locked)
	}

	// The last of both inputs goes with the second take, the result slot
	// emptied with them; each take zooms under its own id.
	w.Click(cartResultSlot, 0, protocol.ClickModePickup)
	expectCartographyTake(w, map[int16]protocol.ItemStack{
		cartMapSlot:    protocol.ItemStack{},
		cartAddSlot:    protocol.ItemStack{},
		cartResultSlot: protocol.ItemStack{},
	}, mapOf(t, 1, 1))

	// The emptied window gives no more takes.
	w.Click(cartResultSlot, 0, protocol.ClickModePickup)
	expectNothingFromTheTable(w)
}

// TestAnEmptyMapClonesTheCartographyTableMap lays a filled map beside the
// empty map the clone asks for, and takes the answer: two maps - the
// original gone into the answer, the empty one that copied it spent - on
// the cursor, no mark asked of either, the map's last piece taking the
// result with it.
func TestAnEmptyMapClonesTheCartographyTableMap(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, withSavedMap(t, servertest.BlockTestConfig(), 0, false, cartMapID))

	openCartography(t, w)

	maps := filledMap(t, 1)
	emptyMaps := servertest.ItemStack(t, "minecraft:map", 2)
	feedCartography(t, w, maps, emptyMaps, filledMap(t, 2))

	w.Click(cartResultSlot, 0, protocol.ClickModePickup)
	expectCartographyTake(w, map[int16]protocol.ItemStack{
		cartMapSlot:    protocol.ItemStack{},
		cartAddSlot:    servertest.ItemStack(t, "minecraft:map", 1),
		cartResultSlot: protocol.ItemStack{},
	}, filledMap(t, 2))
}

// TestAGlassPaneLocksTheCartographyTableMap lays a scale-2 filled map
// beside the glass pane the lock asks for, and takes the answer: a locked
// copy of the map under a new id at the same scale, the take spending one
// of each input behind it.
func TestAGlassPaneLocksTheCartographyTableMap(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, withSavedMap(t, servertest.BlockTestConfig(), 2, false, cartMapID))

	openCartography(t, w)

	maps := filledMap(t, 1)
	panes := servertest.ItemStack(t, "minecraft:glass_pane", 2)
	locked := markedMap(t, 1, protocol.MapPostProcessingLock)
	feedCartography(t, w, maps, panes, locked)

	w.Click(cartResultSlot, 0, protocol.ClickModePickup)
	expectCartographyTake(w, map[int16]protocol.ItemStack{
		cartMapSlot:    protocol.ItemStack{},
		cartAddSlot:    servertest.ItemStack(t, "minecraft:glass_pane", 1),
		cartResultSlot: protocol.ItemStack{},
	}, mapOf(t, 1, 0))
	if update := expectMadeMap(t, w, cartHotbar0+2, mapOf(t, 1, 0)); update.Scale != 2 || !update.Locked {
		t.Errorf("the locked map's data has scale %d locked %v, want scale 2 locked", update.Scale, update.Locked)
	}
}

// TestShiftClicksFeedTheCartographyTableAndZoomAllItsPaper drives the
// shift-click's routing through the window: a filled map out of the
// player's things lays in the map slot and paper in the addition, and the
// shift-click on the filled result keeps zooming for as long as the paper
// has a piece to spend - both takes of the one click into the hotbar's
// far end, both inputs spent, the sound once.
func TestShiftClicksFeedTheCartographyTableAndZoomAllItsPaper(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, withSavedMap(t, servertest.BlockTestConfig(), 0, false, cartMapID))

	openCartography(t, w)

	// The map lays in the map slot whole out of the hotbar.
	maps := filledMap(t, 2)
	paper := servertest.ItemStack(t, "minecraft:paper", 2)
	zoomed := markedMap(t, 1, protocol.MapPostProcessingScale)
	w.SetCreativeSlot(36, maps)
	w.Click(cartHotbar0, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartHotbar0: {}, cartMapSlot: maps},
		protocol.ItemStack{})

	// The paper lays in the addition, and the result shows with it.
	w.SetCreativeSlot(37, paper)
	w.Click(cartHotbar0+1, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		cartHotbar0 + 1: {},
		cartAddSlot:     paper,
		cartResultSlot:  zoomed,
	}, protocol.ItemStack{})

	// Both pieces of the paper behind one shift-click on the result: the
	// two zoomed maps - the take's mark stripped, each under its own new id
	// - land hotbar-end first like every result's move, the inputs and the
	// result emptied.
	w.Click(cartResultSlot, 0, protocol.ClickModeQuickMove)
	expectCartographyTake(w, map[int16]protocol.ItemStack{
		cartMapSlot:     protocol.ItemStack{},
		cartAddSlot:     protocol.ItemStack{},
		cartResultSlot:  protocol.ItemStack{},
		cartHotbar0 + 8: mapOf(t, 1, 0),
		cartHotbar0 + 7: mapOf(t, 1, 1),
	}, protocol.ItemStack{})
}

// TestCtrlQThrowsTheCartographyTableZoomsUntilThePaperRunsOut lays a
// filled map over two paper and sends Ctrl+Q on the filled result: the
// whole offer leaves once per paper, two marked maps in two drops, and
// the window is emptied behind them.
func TestCtrlQThrowsTheCartographyTableZoomsUntilThePaperRunsOut(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, withSavedMap(t, servertest.BlockTestConfig(), 0, false, cartMapID))

	openCartography(t, w)

	maps := filledMap(t, 2)
	paper := servertest.ItemStack(t, "minecraft:paper", 2)
	zoomed := markedMap(t, 1, protocol.MapPostProcessingScale)
	feedCartography(t, w, maps, paper, zoomed)

	w.Click(cartResultSlot, 1, protocol.ClickModeThrow)
	expectCartographyTake(w, map[int16]protocol.ItemStack{
		cartMapSlot:    protocol.ItemStack{},
		cartAddSlot:    protocol.ItemStack{},
		cartResultSlot: protocol.ItemStack{},
	}, protocol.ItemStack{})
	w.ExpectDrop(mapOf(t, 1, 0))
	w.ExpectDrop(mapOf(t, 1, 1))
}

// swapCartographyInput lays stack, set in the hotbar slot, in the table's
// slot, puts what the slot held (was) back in the hotbar slot, and checks
// the result offers nothing to take.
func swapCartographyInput(t *testing.T, w *servertest.WindowedClient, slot, hotbar int16, stack, was protocol.ItemStack) {
	t.Helper()
	w.SetCreativeSlot(36+hotbar, stack)
	w.Click(cartHotbar0+hotbar, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartHotbar0 + hotbar: {}}, stack)
	w.Click(slot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{slot: stack}, was)
	if !was.Empty() {
		w.Click(cartHotbar0+hotbar, 0, protocol.ClickModePickup)
		w.ExpectClickResult(map[int16]protocol.ItemStack{cartHotbar0 + hotbar: was}, protocol.ItemStack{})
	}
	w.Click(cartResultSlot, 0, protocol.ClickModePickup)
	expectNothingFromTheTable(w)
}

// TestTheCartographyTableOffersNothingForALockedOrFullyZoomedMap swaps the
// inputs through the cases the map's saved data refuses: a locked map
// neither locks nor zooms, a scale-4 map does not zoom, and a map whose id
// has no saved data makes nothing with any addition.
// vanilla: CartographyTableMenu.setupResultSlot
func TestTheCartographyTableOffersNothingForALockedOrFullyZoomedMap(t *testing.T) {
	t.Parallel()
	const lockedID, fullID, missingID = cartMapID, cartMapID + 1, cartMapID + 2
	cfg := withSavedMap(t, servertest.BlockTestConfig(), 0, true, lockedID)
	servertest.WriteMapSavedData(t, cfg.LevelName, fullID, 4, false, make([]byte, servertest.MapSide*servertest.MapSide))
	_, w := servertest.OpenWindowed(t, cfg)

	openCartography(t, w)

	pane := servertest.ItemStack(t, "minecraft:glass_pane", 1)
	paper := servertest.ItemStack(t, "minecraft:paper", 1)
	locked, full, missing := mapOf(t, 1, lockedID), mapOf(t, 1, fullID), mapOf(t, 1, missingID)

	swapCartographyInput(t, w, cartMapSlot, 0, locked, protocol.ItemStack{})
	swapCartographyInput(t, w, cartAddSlot, 1, pane, protocol.ItemStack{})
	swapCartographyInput(t, w, cartAddSlot, 1, paper, pane)
	swapCartographyInput(t, w, cartMapSlot, 0, full, locked)
	swapCartographyInput(t, w, cartMapSlot, 0, missing, full)
	swapCartographyInput(t, w, cartAddSlot, 1, pane, paper)
	swapCartographyInput(t, w, cartAddSlot, 1, servertest.ItemStack(t, "minecraft:map", 1), pane)
}

// TestClosingTheCartographyTableWindowHandsBackItsInputs checks the
// close: the map and the paper go home to the player's things, and the
// answer the result was showing goes with the window rather than into
// them.
func TestClosingTheCartographyTableWindowHandsBackItsInputs(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, withSavedMap(t, servertest.SurvivalTestConfig(), 0, false, cartMapID))

	openCartographySurvival(t, srv, w)

	// The map over the paper, laid in the window's own slots; the result
	// holds the zoomed answer the closing must not hand back.
	maps := filledMap(t, 1)
	paper := servertest.ItemStack(t, "minecraft:paper", 3)
	srv.GiveSlot("Testificate", player.HotbarSlot, maps)
	srv.GiveSlot("Testificate", player.HotbarSlot+1, paper)
	w.Drain()
	w.Click(cartHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartHotbar0: {}}, maps)
	w.Click(cartMapSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartMapSlot: maps}, protocol.ItemStack{})
	w.Click(cartHotbar0+1, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{cartHotbar0 + 1: {}}, paper)
	w.Click(cartAddSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		cartAddSlot:    paper,
		cartResultSlot: markedMap(t, 1, protocol.MapPostProcessingScale),
	}, protocol.ItemStack{})

	// Closing hands the inputs back; the result's answer is not theirs to
	// keep, going with the window.
	w.CloseWindow()
	w.ExpectWindowZero()
	if got := srv.InventorySlot("Testificate", player.HotbarSlot); !got.Equal(maps) {
		t.Errorf("after close the hotbar holds %+v, want the map the input handed back", got)
	}
	if got := srv.InventorySlot("Testificate", player.HotbarSlot+1); !got.Equal(paper) {
		t.Errorf("after close the second hotbar slot holds %+v, want the paper the input handed back", got)
	}
}
