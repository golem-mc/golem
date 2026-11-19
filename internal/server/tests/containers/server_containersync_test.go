package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// What the server tells a viewer of a container that changed: only the slots
// that differ from the last copy it sent that viewer, so a move of one item
// is one Set Container Slot, not a resend of the whole container. The state
// ID advances once per packet, the way the client's next click echoes it.
// vanilla: AbstractContainerMenu.broadcastChanges

// TestAClickOnAStaleStateRunsAndAnswersTheWholeWindow moves a slot on the
// server so the window's state moves past the one the client last saw, then
// clicks the moved slot echoing the old state. The click must still take the
// stack against the server's slots, and the answer must be the whole window
// rather than a correction to a prediction about a state that is gone.
func TestAClickOnAStaleStateRunsAndAnswersTheWholeWindow(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// A hopper fed by one above it, with the lower hopper's window open.
	if !srv.PlaceContainer(4, 65, 0, "minecraft:hopper") ||
		!srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
		t.Fatal("the hoppers would not stand")
	}
	w.UseBlock(4, 64, 0)
	if open := w.ExpectContainerOpenType(protocol.MenuHopper, protocol.BlockEntityHopper, 4, 64, 0); len(open) != 5+36 {
		t.Fatalf("the hopper window has %d slots, want its 5 and the player's 36", len(open))
	}
	srv.StopTick()

	// The state the client last saw, before the server moves the window on.
	stale := w.StateOf(w.Window)

	// The hopper above feeds the open one. The correction is read so the
	// wire is clear, but the client's next click still echoes the state
	// from before the move.
	if !srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the top hopper has no slot to fill")
	}
	servertest.TickBlockEntitiesByHand(srv, 1)
	if got := w.CollectContainerSlots(); len(got) != 1 || got[0].Slot != 0 || got[0].Stack.Count != 1 {
		t.Fatalf("the hopper move sent %+v, want one correction for slot 0", got)
	}

	// The click carries the stale state: the server must run it against its
	// own slots, taking the dirt, and answer with the whole window.
	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	w.ClickWithState(0, 0, protocol.ClickModePickup, stale)
	answer := w.CollectClickResponse()
	if len(answer.Contents) != 1 {
		t.Fatalf("the stale click sent %d full contents, want one: %+v", len(answer.Contents), answer.Contents)
	}
	p := answer.Contents[0]
	if p.Window != w.Window {
		t.Errorf("content window = %d, want %d", p.Window, w.Window)
	}
	if !p.Carried.Equal(dirt) {
		t.Errorf("after the stale click the cursor holds %+v, want %+v", p.Carried, dirt)
	}
	if len(p.Slots) <= 0 || !p.Slots[0].Empty() {
		t.Errorf("after the stale click slot 0 holds %+v, want empty", p.Slots[0])
	}
}

// TestAClickOnTheCurrentStateAnswersOnlyTheChangedSlots clicks a hopper's own
// slot echoing the state the server last sent: the click's answer is the slot
// it changed and the cursor, not the whole window.
func TestAClickOnTheCurrentStateAnswersOnlyTheChangedSlots(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	if !srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
		t.Fatal("the hopper would not stand")
	}
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the hopper has no slot to fill")
	}
	w.UseBlock(4, 64, 0)
	if open := w.ExpectContainerOpenType(protocol.MenuHopper, protocol.BlockEntityHopper, 4, 64, 0); len(open) != 5+36 {
		t.Fatalf("the hopper window has %d slots, want its 5 and the player's 36", len(open))
	}
	srv.StopTick()

	// The click echoes the state the open left the window on. Its answer is
	// the one slot it changed and the cursor; a whole window would mean the
	// state was taken for stale.
	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	w.Click(0, 0, protocol.ClickModePickup)
	answer := w.CollectClickResponse()
	if len(answer.Contents) != 0 {
		t.Errorf("the fresh click sent %d full contents, want none: %+v", len(answer.Contents), answer.Contents)
	}
	if got, ok := answer.Slots[0]; !ok {
		t.Errorf("slot 0 was not corrected; corrected were %v", answer.Slots)
	} else if !got.Empty() {
		t.Errorf("slot 0 = %+v, want empty", got)
	}
	if answer.Carried == nil {
		t.Error("the cursor was never told")
	} else if !answer.Carried.Equal(dirt) {
		t.Errorf("cursor = %+v, want %+v", *answer.Carried, dirt)
	}
}

// TestClosingAContainerSendsNoWindowContent closes a chest's window and
// checks the server sends nothing at all: vanilla's doCloseContainer moves
// to the player's own window without filling it in, and the close handed
// nothing back, so there is no player-window content to send.
func TestClosingAContainerSendsNoWindowContent(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the chest has no slot to fill")
	}
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	w.ExpectLid(4, 64, 0, 1)

	w.CloseWindow()
	contents := 0
	w.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayContainerContent {
			return
		}
		if protocol.NewReader(data).ReadVarInt() == 0 {
			contents++
		}
	})
	if contents != 0 {
		t.Errorf("closing the chest sent %d player-window contents, want none", contents)
	}
}

// TestASpectatorsClickIsRefusedWithTheWholeWindow opens a chest, switches its
// viewer to spectator, and clicks a slot: the click must move nothing and be
// answered with the whole window.
func TestASpectatorsClickIsRefusedWithTheWholeWindow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Dial(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Testificate"))
	w := servertest.WindowedOf(t, "Testificate", c)

	if !srv.PlaceContainer(4, 64, 0, "minecraft:chest") {
		t.Fatal("the chest would not stand")
	}
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the chest has no slot to fill")
	}
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	srv.StopTick()

	w.ChatCommand("gamemode spectator")
	w.Settle()
	if srv.PlayerGameMode("Testificate") != protocol.GameModeSpectator {
		t.Fatal("the switch to spectator applies")
	}
	w.Drain()

	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	w.Click(0, 0, protocol.ClickModePickup)
	answer := w.CollectClickResponse()
	if len(answer.Contents) != 1 {
		t.Fatalf("the spectator's click sent %d full contents, want one: %+v",
			len(answer.Contents), answer.Contents)
	}
	if got := answer.Contents[0].Slots[0]; !got.Equal(dirt) {
		t.Errorf("the chest slot after the spectator's click = %+v, want the dirt unmoved", got)
	}
	if !answer.Contents[0].Carried.Empty() {
		t.Errorf("the cursor after the spectator's click = %+v, want empty", answer.Contents[0].Carried)
	}
	if got := srv.InventoryAt(4, 64, 0); len(got) == 0 || !got[0].Equal(dirt) {
		t.Errorf("the chest holds %+v after the spectator's click, want the dirt", got)
	}
}

// TestAStaleClickOnALitFurnaceResendsTheDataSlots moves a lit furnace's window
// on past the state its viewer last saw, then clicks on the old state: the
// full resync must carry the fire and progress properties along with the
// contents.
func TestAStaleClickOnALitFurnaceResendsTheDataSlots(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:furnace", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.SettleKeeping()
	srv.TickSurvival()
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuFurnace, true, 4, 64, 0)

	// The input and the coal go in through the open window, so the
	// furnace's numbers keep coming to it.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:raw_iron", 1))
	w.Click(30, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0:  servertest.ItemStack(t, "minecraft:raw_iron", 1),
		30: protocol.ItemStack{},
	}, protocol.ItemStack{})
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:coal", 1))
	w.Click(31, 0, protocol.ClickModeQuickMove)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1:  servertest.ItemStack(t, "minecraft:coal", 1),
		31: protocol.ItemStack{},
	}, protocol.ItemStack{})

	// One tick lights the fuel, and the numbers it told are read past.
	runTheSmelts(t, srv, 1)
	if _, _, lit, _, ok := srv.FurnaceAt(4, 64, 0); !ok || lit <= 0 {
		t.Fatal("the furnace did not light")
	}
	w.CollectContainerSlots()

	// The click's answer moves the server on; the click after it echoes the
	// state from before, so the server answers the whole window.
	stale := w.StateOf(w.Window)
	iron := servertest.ItemStack(t, "minecraft:raw_iron", 1)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}}, iron)

	spent, total, lit, litTotal, ok := srv.FurnaceAt(4, 64, 0)
	if !ok {
		t.Fatal("the furnace left the world")
	}
	w.ClickWithState(0, 0, protocol.ClickModePickup, stale)
	wire := w.CollectWindowWire()
	var contents int
	contentAt, firstProperty := -1, -1
	got := map[int32]int32{}
	for i, packet := range wire {
		if packet.Content {
			contents++
			if contentAt < 0 {
				contentAt = i
			}
			continue
		}
		if firstProperty < 0 {
			firstProperty = i
		}
		got[packet.Number] = packet.Value
	}
	if contents != 1 {
		t.Fatalf("the stale click sent %d full contents, want one", contents)
	}
	if firstProperty < 0 || contentAt < 0 || contentAt > firstProperty {
		t.Errorf("the data slots came at %d, the content at %d, want the content first",
			firstProperty, contentAt)
	}
	want := map[int32]int32{
		protocol.FurnacePropertyFuelLeft:      lit,
		protocol.FurnacePropertyFuelTotal:     litTotal,
		protocol.FurnacePropertyProgress:      spent,
		protocol.FurnacePropertyProgressTotal: total,
	}
	if len(got) != len(want) {
		t.Errorf("the resync sent %d furnace properties, want %d: %v", len(got), len(want), wire)
	}
	for number, value := range want {
		if seen, ok := got[number]; !ok || seen != value {
			t.Errorf("furnace property %d = %d, want %d", number, seen, value)
		}
	}
}
