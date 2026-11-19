package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// TestFOverAChestSlotSwapsWithTheOffHand presses F over a chest slot: the
// stack there trades places with the off hand, which the chest's window has
// no slot for, so only the chest slot is corrected.
// vanilla: AbstractContainerMenu.doClick SWAP
func TestFOverAChestSlotSwapsWithTheOffHand(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)

	apples := servertest.ItemStack(t, "minecraft:apple", 10)
	coal := servertest.ItemStack(t, "minecraft:coal", 7)
	w.SetCreativeSlot(36, apples)
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, apples)
	w.Click(5, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{5: apples}, protocol.ItemStack{})
	srv.GiveSlot("Testificate", player.OffHandSlot, coal)
	w.Drain()

	w.Click(5, 40, protocol.ClickModeSwap)
	w.ExpectClickResult(map[int16]protocol.ItemStack{5: coal}, protocol.ItemStack{})
	if got := srv.InventorySlot("Testificate", player.OffHandSlot); !got.Equal(apples) {
		t.Errorf("off hand = %+v, want the apples from the chest", got)
	}

	// An empty off hand takes the stack and leaves the slot empty.
	w.Click(5, 40, protocol.ClickModeSwap)
	w.ExpectClickResult(map[int16]protocol.ItemStack{5: apples}, protocol.ItemStack{})
	w.Click(5, 40, protocol.ClickModeSwap)
	w.ExpectClickResult(map[int16]protocol.ItemStack{5: coal}, protocol.ItemStack{})
}

// TestFOverAStonecutterResultTakesTheCutIntoAnEmptyOffHand presses F on the
// stonecutter's result: the cut goes to the empty off hand and the input is
// spent, as for a number key.
// vanilla: AbstractContainerMenu.doClick SWAP
func TestFOverAStonecutterResultTakesTheCutIntoAnEmptyOffHand(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	openStonecutterCreative(t, w)
	servertest.ForgetWindowProps(w)
	stone := servertest.ItemStack(t, "minecraft:stone", 2)
	w.SetCreativeSlot(36, stone)
	w.Click(stoneHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneHotbar0: {}}, stone)
	w.Click(stoneInputSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneInputSlot: stone}, protocol.ItemStack{})
	servertest.ForgetWindowProps(w)
	w.ClickButton(2)
	expectCorrectedSlots(w, map[int16]protocol.ItemStack{
		stoneResultSlot: servertest.ItemStack(t, "minecraft:cobblestone_slab", 2),
	})
	servertest.ForgetWindowProps(w)

	w.Click(stoneResultSlot, 40, protocol.ClickModeSwap)
	expectStonecutterSound(w, 4, 64, 0)
	if got := srv.InventorySlot("Testificate", player.OffHandSlot); !got.Equal(servertest.ItemStack(t, "minecraft:cobblestone_slab", 2)) {
		t.Errorf("off hand = %+v, want the two slabs", got)
	}
}

// TestFOverAFuelSlotKeepsAnOffHandStackItRefuses leaves both stacks where
// they were when the furnace's fuel slot will not take the off hand's dirt.
func TestFOverAFuelSlotKeepsAnOffHandStackItRefuses(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:furnace", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpenType(protocol.MenuFurnace, protocol.BlockEntityFurnace, 4, 64, 0)
	dirt := servertest.ItemStack(t, "minecraft:dirt", 5)
	srv.GiveSlot("Testificate", player.OffHandSlot, dirt)
	w.Drain()

	w.Click(1, 40, protocol.ClickModeSwap)
	w.ExpectClickResult(map[int16]protocol.ItemStack{}, protocol.ItemStack{})
	if got := srv.InventorySlot("Testificate", player.OffHandSlot); !got.Equal(dirt) {
		t.Errorf("off hand = %+v, want the dirt kept", got)
	}
}

// TestFOverAFuelSlotCapsAnOffHandBucketStack puts three empty buckets in the
// off hand: the fuel slot takes one, the off hand keeps two.
// vanilla: AbstractContainerMenu.doClick SWAP, AbstractFurnaceMenu.FurnaceFuelSlot.getMaxStackSize
func TestFOverAFuelSlotCapsAnOffHandBucketStack(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:furnace", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpenType(protocol.MenuFurnace, protocol.BlockEntityFurnace, 4, 64, 0)
	srv.GiveSlot("Testificate", player.OffHandSlot, servertest.ItemStack(t, "minecraft:bucket", 3))
	w.Drain()

	w.Click(1, 40, protocol.ClickModeSwap)
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		1: servertest.ItemStack(t, "minecraft:bucket", 1),
	}, protocol.ItemStack{})
	if got := srv.InventorySlot("Testificate", player.OffHandSlot); !got.Equal(servertest.ItemStack(t, "minecraft:bucket", 2)) {
		t.Errorf("off hand = %+v, want two buckets", got)
	}
}

// TestFOverAChestSlotTellsWatchersTheOffHand shows the other players the
// stack that arrives in the off hand.
func TestFOverAChestSlotTellsWatchersTheOffHand(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig())
	watcher := connect().Join("Watcher")
	w := servertest.WindowedOf(t, "Testificate", connect())
	entityID := watcher.ExpectPlayerAdded("Testificate")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Testificate")

	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	apples := servertest.ItemStack(t, "minecraft:apple", 10)
	w.SetCreativeSlot(36, apples)
	w.Click(54, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{54: {}}, apples)
	w.Click(5, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{5: apples}, protocol.ItemStack{})

	w.Click(5, 40, protocol.ClickModeSwap)
	w.ExpectClickResult(map[int16]protocol.ItemStack{5: {}}, protocol.ItemStack{})
	// Earlier packets announce the main hand the setup put the stack in.
	for range 4 {
		entries := watcher.ExpectEquipment(entityID)
		if got := servertest.EquipEntry(entries, protocol.EquipOffHand); got.Equal(apples) {
			return
		}
	}
	t.Error("watcher was never shown the apples in the off hand")
}

// expectStonecutterSound reads the take's sound off the wire: the
// stonecutter's own, at the block's centre, once however many takes the
// click made.
// vanilla: StonecutterMenu$2.onTake, one per game tick
func expectStonecutterSound(w *servertest.WindowedClient, x, y, z int32) {
	w.T.Helper()
	w.AwaitWindowPackets(func(id int32, data []byte) bool {
		if id != protocol.IDPlaySoundEffect {
			return false
		}
		r := protocol.NewReader(data)
		sound := r.ReadVarInt()
		var name string
		if sound == 0 {
			name = r.ReadString(32767)
		} else {
			name, _ = servertest.SoundEventName(sound - 1)
		}
		if name != "minecraft:ui.stonecutter.take_result" {
			w.T.Errorf("the take played %q, want the stonecutter's own", name)
			return true
		}
		r.ReadVarInt() // the category
		gotX, gotY, gotZ := int32(r.ReadI32()), int32(r.ReadI32()), int32(r.ReadI32())
		if gotX != x*8+4 || gotY != y*8+4 || gotZ != z*8+4 {
			w.T.Errorf("the take played at (%d,%d,%d), want (%d,%d,%d)",
				gotX/8, gotY/8, gotZ/8, x, y, z)
			return true
		}
		if volume, pitch := r.ReadF32(), r.ReadF32(); volume != 1.0 || pitch != 1.0 {
			w.T.Errorf("the take sounded at volume %v pitch %v, want both 1", volume, pitch)
		}
		return true
	})
}
