package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
)

// A blast that destroys a container must take it out of the world before it
// reads its slots. Anything that changes the container after the read but
// before the removal — a hopper move, a dispense — leaves the stack the
// spill already took duplicated on the ground, because the block entity the
// change went to is still standing when the blast removes it.

// itemTotal counts one item across a container's slots and the item
// entities lying in the overworld, so a test can follow the stack it seeded
// through a blast, past the chest block's own drop.
func itemTotal(srv *server.Server, name string, id int32, slots []protocol.ItemStack) int32 {
	var n int32
	for _, s := range slots {
		if s.ItemID == id {
			n += s.Count
		}
	}
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == name {
			n += it.Count
		}
	}
	return n
}

// TestABlastRemovesAContainerBeforeItsContentsSpill blows a chest holding
// one dirt out of the world with the blast seam armed: the moment the blast
// has read the chest's slots, a hopper's turn moves them into a hopper
// beside the blast — but only if the chest is still in the world. With the
// block entity removed before it is read, the seam finds it gone and moves
// nothing, and the one dirt comes out once. Read before it is removed, the
// move lands and the blast drops the dirt it had already taken, leaving the
// one dirt in the hopper and on the ground together.
func TestABlastRemovesAContainerBeforeItsContentsSpill(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, servertest.BlockTestConfig())
	srv.StopTick()
	release := srv.HoldChunk(4, 0)
	defer release()

	chest := [3]int32{4, 65, 0}
	hopper := [3]int32{4, 64, 8}
	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	if !srv.PlaceContainer(chest[0], chest[1], chest[2], "minecraft:chest") {
		t.Fatal("the chest would not stand up")
	}
	if !srv.PlaceContainer(hopper[0], hopper[1], hopper[2], "minecraft:hopper") {
		t.Fatal("the hopper would not stand up")
	}
	srv.SetInventoryAt(chest[0], chest[1], chest[2], 0, dirt)
	srv.SetInventoryAt(hopper[0], hopper[1], hopper[2], 0, protocol.ItemStack{})

	srv.BlastSpillBetween(func(b *world.BlockEntity) {
		if !srv.HasBlockEntity(chest[0], chest[1], chest[2]) {
			return // the chest is already gone: there is nothing to move
		}
		srv.SetInventoryAt(hopper[0], hopper[1], hopper[2], 0, b.Slots[0])
		b.Slots[0] = protocol.ItemStack{}
	})

	srv.BlastAt(4.5, 65.5, 0.5, 2)

	total := itemTotal(srv, "minecraft:dirt", dirt.ItemID,
		srv.InventoryAt(hopper[0], hopper[1], hopper[2]))
	if total != 1 {
		t.Fatalf("the blast and the hopper move came out with %d dirt, want exactly the one that went in", total)
	}
}

// TestABlastRefusesAHoppersStaleContainerPointer takes the chest's block
// entity pointer as a hopper's lookup would before the blast, then runs the
// hopper's move on that same pointer once the blast has taken the chest out
// of the world. The move must find the container gone and leave the dirt to
// the spill; on a pointer that still works, the move and the spill each hand
// the one dirt out and the test sees two.
func TestABlastRefusesAHoppersStaleContainerPointer(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, servertest.BlockTestConfig())
	srv.StopTick()
	release := srv.HoldChunk(4, 0)
	defer release()

	chest := [3]int32{4, 65, 0}
	hopper := [3]int32{4, 64, 8}
	dirt := servertest.ItemStack(t, "minecraft:dirt", 1)
	if !srv.PlaceContainer(chest[0], chest[1], chest[2], "minecraft:chest") {
		t.Fatal("the chest would not stand up")
	}
	if !srv.PlaceContainer(hopper[0], hopper[1], hopper[2], "minecraft:hopper") {
		t.Fatal("the hopper would not stand up")
	}
	srv.SetInventoryAt(chest[0], chest[1], chest[2], 0, dirt)

	srv.BlastSpillBetween(func(b *world.BlockEntity) {
		// The chest's pointer, held from before the blast, is what a hopper
		// took; the move must refuse it rather than empty a container the
		// blast has read and is about to empty again.
		if srv.MoveFromTo(b, hopper) {
			t.Error("the hopper moved the chest the blast had taken out of the world")
		}
	})

	srv.BlastAt(4.5, 65.5, 0.5, 2)

	total := itemTotal(srv, "minecraft:dirt", dirt.ItemID,
		srv.InventoryAt(hopper[0], hopper[1], hopper[2]))
	if total != 1 {
		t.Fatalf("the blast and the hopper move came out with %d dirt, want exactly the one that went in", total)
	}
}
