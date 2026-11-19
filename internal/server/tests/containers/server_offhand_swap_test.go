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
