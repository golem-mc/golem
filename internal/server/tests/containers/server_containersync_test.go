package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

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

// TestADropperFiresOnlyTheChangedSlot fires a dropper with its window open
// and counts what the window is told: one Set Container Slot for the slot
// the item left, whose state ID is the one step the broadcast made, not the
// whole nine-slot window.
func TestADropperFiresOnlyTheChangedSlot(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.FacePlayer(2.5, 64, 0.5, 180, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dropper", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 5)) {
		t.Fatal("the dropper has no slot to fill")
	}

	w.UseBlock(4, 64, 0)
	open := w.ExpectContainerOpenType(protocol.MenuGeneric3x3, protocol.BlockEntityDropper, 4, 64, 0)
	if len(open) < 9 || open[0].Count != 5 {
		t.Fatalf("the window opened on %+v, want five dirt in slot 0", open)
	}
	srv.StopTick()

	want := int32(5)
	for fire := 1; fire <= 2; fire++ {
		want--
		before := w.StateOf(w.Window)
		srv.Dispense(4, 64, 0, protocol.FaceSouth)
		got := w.CollectContainerSlots()
		if len(got) != 1 {
			t.Fatalf("fire %d sent %d slot corrections, want the one that changed: %+v", fire, len(got), got)
		}
		p := got[0]
		if p.Window != w.Window || p.Slot != 0 || p.Stack.Count != want {
			t.Errorf("fire %d corrected %+v, want slot 0 holding %d", fire, p, want)
		}
		if p.State != before+1 {
			t.Errorf("fire %d advanced the state to %d, want %d", fire, p.State, before+1)
		}
	}
}

// TestAHopperFeedsOnlyTheChangedSlot runs one hopper's move into an open one
// below it and counts what the open window is told: one Set Container Slot
// for the slot that took the item, not the whole five-slot window.
func TestAHopperFeedsOnlyTheChangedSlot(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The source hopper above, the open one below it.
	if !srv.PlaceContainer(4, 65, 0, "minecraft:hopper") ||
		!srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
		t.Fatal("the hoppers would not stand")
	}
	w.UseBlock(4, 64, 0)
	if open := w.ExpectContainerOpenType(protocol.MenuHopper, protocol.BlockEntityHopper, 4, 64, 0); len(open) != 5+36 {
		t.Fatalf("the hopper window has %d slots, want its 5 and the player's 36", len(open))
	}
	srv.StopTick()
	if !srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the top hopper has no slot to fill")
	}

	before := w.StateOf(w.Window)
	servertest.TickBlockEntitiesByHand(srv, 1)
	got := w.CollectContainerSlots()
	if len(got) != 1 {
		t.Fatalf("the hopper move sent %d slot corrections, want 1: %+v", len(got), got)
	}
	p := got[0]
	if p.Window != w.Window || p.Slot != 0 || p.Stack.Count != 1 {
		t.Errorf("the correction %+v, want the open window's slot 0 holding one dirt", p)
	}
	if p.State != before+1 {
		t.Errorf("the state advanced to %d, want %d", p.State, before+1)
	}
}

// TestAClickUpdatesTheViewersLastSentSlots clicks a stack into an open hopper
// and then lets the hopper above feed a different slot: the change must
// resend only the slot the new stack arrived in, which means the click's own
// correction updated this viewer's last-sent copy. Without that update the
// change would resend the clicked slot too, since its last-sent copy would
// still say empty.
func TestAClickUpdatesTheViewersLastSentSlots(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	// The source hopper above, the open one below it.
	if !srv.PlaceContainer(4, 65, 0, "minecraft:hopper") ||
		!srv.PlaceContainer(4, 64, 0, "minecraft:hopper") {
		t.Fatal("the hoppers would not stand")
	}
	w.UseBlock(4, 64, 0)
	if open := w.ExpectContainerOpenType(protocol.MenuHopper, protocol.BlockEntityHopper, 4, 64, 0); len(open) != 5+36 {
		t.Fatalf("the hopper window has %d slots, want its 5 and the player's 36", len(open))
	}
	srv.StopTick()

	// Hotbar slot 0 is window slot 32; the whole stack goes onto the cursor
	// and then into the open hopper's slot 2, which the click corrects.
	sticks := servertest.ItemStack(t, "minecraft:stick", 3)
	w.SetCreativeSlot(36, sticks)
	w.Click(32, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{32: {}}, sticks)
	w.Click(2, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{2: sticks}, protocol.ItemStack{})

	// The hopper above drops its item into the open hopper's slot 0.
	if !srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the top hopper has no slot to fill")
	}
	servertest.TickBlockEntitiesByHand(srv, 1)
	got := w.CollectContainerSlots()
	if len(got) != 1 || got[0].Slot != 0 || got[0].Stack.Count != 1 {
		t.Fatalf("the change sent %+v, want one correction for the hopper's slot 0", got)
	}
}

// TestAStaleDragPacketIsAnsweredWithTheWholeWindow sends the drag packets
// that change no slot - a start, a mark, and an end with no drag in progress -
// each echoing a state the server has moved past. Vanilla answers every stale
// Click Container with the full state, so each must come back as the whole
// window and the cursor even though the packet itself moved nothing.
func TestAStaleDragPacketIsAnsweredWithTheWholeWindow(t *testing.T) {
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

	// A stack on the cursor, and the state the client last saw before the
	// hopper above moves the open window on.
	sticks := servertest.ItemStack(t, "minecraft:stick", 4)
	w.SetCreativeSlot(36, sticks)
	w.Click(32, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{32: {}}, sticks)
	stale := w.StateOf(w.Window)

	if !srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the top hopper has no slot to fill")
	}
	servertest.TickBlockEntitiesByHand(srv, 1)
	w.CollectContainerSlots()

	// The end first, while no drag is in progress; then a start, which does
	// begin one, and a mark on the way. Every echo is the stale state.
	for _, drag := range []struct {
		name   string
		slot   int16
		button int8
	}{
		{"an end with no drag in progress", -999, 2},
		{"a start", -999, 0},
		{"a mark", 0, 1},
	} {
		w.ClickWithState(drag.slot, drag.button, protocol.ClickModeDrag, stale)
		answer := w.CollectClickResponse()
		if len(answer.Contents) != 1 {
			t.Errorf("the stale %s sent %d full contents, want one: %+v", drag.name, len(answer.Contents), answer.Contents)
			continue
		}
		if p := answer.Contents[0]; p.Window != w.Window {
			t.Errorf("the stale %s answered window %d, want %d", drag.name, p.Window, w.Window)
		}
		if !answer.Contents[0].Carried.Equal(sticks) {
			t.Errorf("the stale %s answered with the cursor holding %+v, want %+v",
				drag.name, answer.Contents[0].Carried, sticks)
		}
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

// TestClosingAContainerDoesNotMoveThePlayerWindowState closes a container
// whose state moved and then clicks the player's own window on the state the
// join left it on. The client's window 0 only follows window-0 packets, so a
// close must not hand it the container's state: the click has to echo the
// join's state and go through. A close that moved the container's state onto
// window 0 would fail the state check and resync the whole inventory instead
// of acting on the click.
func TestClosingAContainerDoesNotMoveThePlayerWindowState(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:dropper", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 5)) {
		t.Fatal("the dropper has no slot to fill")
	}
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpenType(protocol.MenuGeneric3x3, protocol.BlockEntityDropper, 4, 64, 0)
	srv.StopTick()

	// The state the player's own window was left on at the join.
	joined := w.StateOf(0)
	for range 3 {
		srv.Dispense(4, 64, 0, protocol.FaceSouth)
		w.CollectContainerSlots()
	}
	if w.StateOf(0) != joined {
		t.Fatalf("the container's packets moved the player's own window state to %d, want %d",
			w.StateOf(0), joined)
	}

	w.CloseWindow()
	w.ExpectWindowZero()
	if got := w.StateOf(0); got != joined {
		t.Fatalf("the close moved the player's own window state to %d, want %d", got, joined)
	}

	// The click on the inventory goes through on the join's state: the
	// server corrects the clicked slot and the cursor rather than
	// resynchronizing the whole window.
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:stick", 5))
	w.Click(36, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{36: {}}, servertest.ItemStack(t, "minecraft:stick", 5))
}

// TestOpeningADropperWhileAHopperFeedsIt opens a dropper's window again and
// again while the hopper above it feeds it. The open reads the dropper's
// slots to fill the window; under the race detector an open that reads them
// without the block entity's lock races the hopper's write, so this is the
// scenario `go test -race` has to finish clean.
func TestOpeningADropperWhileAHopperFeedsIt(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	w := servertest.WindowedOf(t, "Testificate", connect())

	// A dropper with a hopper above it, kept fed so its moves keep writing
	// the dropper's slots while the window opens over them.
	if !srv.PlaceContainer(4, 64, 0, "minecraft:dropper") ||
		!srv.PlaceContainer(4, 65, 0, "minecraft:hopper") {
		t.Fatal("the hopper and dropper would not stand")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		srv.SetInventoryAt(4, 65, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 8))
		// The open's own packets are dropped: the race is the server's
		// read of the slots, not what the client reads back.
		w.UseBlock(4, 64, 0)
		w.CloseWindow()
		w.Settle()
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

// TestADeadPlayersClickIsRefusedWithTheWholeWindow opens a chest, kills its
// viewer on the survival death screen with the window still open, and clicks
// a slot: the click must move nothing and be answered with the whole window.
func TestADeadPlayersClickIsRefusedWithTheWholeWindow(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.SurvivalTestConfig())

	if !srv.PlaceContainer(4, 64, 0, "minecraft:chest") {
		t.Fatal("the chest would not stand")
	}
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the chest has no slot to fill")
	}
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	srv.StopTick()

	if !srv.Hurt("Testificate", 100) {
		t.Fatal("the killing blow did not land")
	}
	w.Drain()

	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	w.Click(0, 0, protocol.ClickModePickup)
	answer := w.CollectClickResponse()
	if len(answer.Contents) != 1 {
		t.Fatalf("the dead player's click sent %d full contents, want one: %+v",
			len(answer.Contents), answer.Contents)
	}
	if got := answer.Contents[0].Slots[0]; !got.Equal(dirt) {
		t.Errorf("the chest slot after the dead player's click = %+v, want the dirt unmoved", got)
	}
	if !answer.Contents[0].Carried.Empty() {
		t.Errorf("the cursor after the dead player's click = %+v, want empty", answer.Contents[0].Carried)
	}
	if got := srv.InventoryAt(4, 64, 0); len(got) == 0 || !got[0].Equal(dirt) {
		t.Errorf("the chest holds %+v after the dead player's click, want the dirt", got)
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

// TestAStaleClickOnTheEnchantingTableRecomputesTheOffers opens an enchanting
// table in its ring of shelves and shift-clicks a sword onto it carrying a
// state the open has moved the window past. The resync must carry the offers
// the sword draws - ten data slots, content first - not the window's opening
// zeros and then the new values.
func TestAStaleClickOnTheEnchantingTableRecomputesTheOffers(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	srv.SetXpSeed("Testificate", testSeed)

	fillShelves(t, srv, offerRing()...)
	if got := srv.CountBookshelves(4, 64, 0); got != 30 {
		t.Fatalf("the ring counts %d shelves, want the 30 the offers are pinned to", got)
	}
	openTable(t, srv, w, servertest.ItemStack(t, "minecraft:enchanting_table", 1))
	servertest.CollectWindowProps(w)
	servertest.ForgetWindowProps(w)

	// The sword in hotbar 1. The shift-click carries state 0, which the
	// open has moved the window past, so the server must run it against
	// its own state and answer the whole window.
	if w.StateOf(w.Window) == 0 {
		t.Fatal("the open left the window on state 0, so a state-0 click could not be stale")
	}
	w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:diamond_sword", 1))
	w.ClickWithState(enchHotbar1, 0, protocol.ClickModeQuickMove, 0)
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
	if len(wire) != int(protocol.EnchantPropertyCount)+1 {
		t.Errorf("the resync sent %d packets, want the content and the table's %d data slots: %v",
			len(wire), protocol.EnchantPropertyCount, wire)
	}

	// The offers seed 42 and thirty shelves draw for a diamond sword.
	want := map[int32]int32{
		protocol.EnchantPropertyCost0:    4,
		protocol.EnchantPropertyCost1:    9,
		protocol.EnchantPropertyCost2:    30,
		protocol.EnchantPropertySeed:     testSeed,
		protocol.EnchantPropertyEnchant0: 33, // minecraft:sharpness
		protocol.EnchantPropertyEnchant1: 33,
		protocol.EnchantPropertyEnchant2: 37, // minecraft:sweeping_edge
		protocol.EnchantPropertyLevel0:   1,
		protocol.EnchantPropertyLevel1:   2,
		protocol.EnchantPropertyLevel2:   3,
	}
	for number, value := range want {
		if seen, ok := got[number]; !ok || seen != value {
			t.Errorf("data slot %d = %d, want %d; sent were %v", number, seen, value, wire)
		}
	}
}

// TestAClickWithStateZeroIsAnsweredInFull opens a window the server has moved
// past state 0, then clicks carrying 0: the mismatch must be taken as stale
// and answered with the whole window.
func TestAClickWithStateZeroIsAnsweredInFull(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	if !srv.PlaceContainer(4, 64, 0, "minecraft:chest") {
		t.Fatal("the chest would not stand")
	}
	if !srv.SetInventoryAt(4, 64, 0, 0, servertest.ItemStack(t, "minecraft:dirt", 1)) {
		t.Fatal("the chest has no slot to fill")
	}
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	srv.StopTick()
	if w.StateOf(w.Window) == 0 {
		t.Fatal("the open left the window on state 0, so a state-0 click could not be stale")
	}

	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	w.ClickWithState(0, 0, protocol.ClickModePickup, 0)
	answer := w.CollectClickResponse()
	if len(answer.Contents) != 1 {
		t.Fatalf("the state-0 click sent %d full contents, want one: %+v",
			len(answer.Contents), answer.Contents)
	}
	if got := answer.Contents[0].Carried; !got.Equal(dirt) {
		t.Errorf("the state-0 click's cursor = %+v, want the dirt", got)
	}
}
