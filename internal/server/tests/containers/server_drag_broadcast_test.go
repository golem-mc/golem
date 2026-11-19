package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestDragIntoASharedChestTellsTheOtherViewer checks the paint drag ending in
// a chest another player also has open: the chest slots the drag filled are
// corrected in the other viewer's window the way a click's are, and the slot
// the others watch - the held hotbar slot a drag can land on - reaches them
// through the equipment broadcast. A drag into a shared chest is the same
// kind of change a click into one is, and hears the same broadcasts.
func TestDragIntoASharedChestTellsTheOtherViewer(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig())

	// Bob puts the chest out alone, and Alice arrives after him: her body
	// is spawned into Bob's view, which is where her entity ID comes from.
	bob := servertest.WindowedOf(t, "Bob", connect())
	bob.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:chest", 1))
	bob.PlaceBlock(4, 63, 0, protocol.FaceTop)
	bob.ExpectBlockAck()

	alice := servertest.WindowedOf(t, "Alice", connect())
	aliceEntity := bob.ExpectPlayerAdded("Alice")
	bob.ExpectAnnouncement("multiplayer.player.joined", "Alice")

	// Both open the chest, the lid counting whoever is looking in. Bob
	// opens only once the lid has told Alice's one, or both opens can
	// land before the count goes out and the lid tells two at once.
	alice.UseBlock(4, 64, 0)
	alice.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	alice.ExpectLid(4, 64, 0, 1)
	bob.ExpectLid(4, 64, 0, 1)
	bob.UseBlock(4, 64, 0)
	bob.ExpectContainerOpen(protocol.MenuGeneric9x3, true, 4, 64, 0)
	alice.ExpectLid(4, 64, 0, 2)
	bob.ExpectLid(4, 64, 0, 2)
	// The echo pair the use's tail tells the clicker trails the lid here,
	// and nothing else in the drag reads them otherwise.
	bob.ExpectUseEchoes(4, 64, 0, protocol.FaceTop)

	// Alice lifts a whole stack of sticks out of the hotbar's second slot -
	// not the one she is holding, which would broadcast on its own - and
	// paints two slots with it: one of the chest's and her own held hotbar
	// slot. Letting the button go divides the stack between them, half in
	// each and nothing left on the cursor.
	sticks64 := servertest.ItemStack(t, "minecraft:stick", 64)
	sticks32 := servertest.ItemStack(t, "minecraft:stick", 32)
	alice.SetCreativeSlot(37, sticks64)
	alice.Click(55, 0, protocol.ClickModePickup)
	alice.ExpectClickResult(map[int16]protocol.ItemStack{55: {}}, sticks64)
	alice.Click(-999, 0, protocol.ClickModeDrag)
	alice.Drain()
	alice.Click(10, 1, protocol.ClickModeDrag)
	alice.Click(54, 1, protocol.ClickModeDrag)
	alice.Drain()
	alice.Click(-999, 2, protocol.ClickModeDrag)
	alice.ExpectClickResult(map[int16]protocol.ItemStack{
		10: sticks32,
		54: sticks32,
	}, protocol.ItemStack{})

	// Alice's hand lightened, and the others hear it before the chest's
	// slot, the order the drag's packets go out in.
	entries := bob.ExpectEquipment(aliceEntity)
	if got := servertest.EquipEntry(entries, 0); !got.Equal(sticks32) {
		bob.T.Errorf("Alice's held item = %+v, want the thirty-two sticks", got)
	}

	// And the chest's slot arrives in Bob's own window, the same slot
	// number the chest carries in every window opened on it.
	bob.AwaitWindowSlot(10, sticks32)
}
