package movement

// The ladder tests: a ladder's placement reads the wall it hangs on, not
// the face it was clicked through - the vanilla LadderBlock's
// getStateForPlacement probing the four horizontals against the survival
// its canSurvive asks for - and the ladder it stands stays only as long as
// that wall does.

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// TestALadderClimbsAWallAtHeadHeight puts a ladder against a wall at the
// height of the placer's own eyes: the ladder takes the cell the body
// stands in, which a full block could not, because a ladder's collision is
// the thin box against the wall and the body clears it. The facing comes
// from the way the placer looks - standing south of the wall looking north,
// the ladder's face turns south - and the place sound reaches the watcher,
// not the placer whose client predicted it.
func TestALadderClimbsAWallAtHeadHeight(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	// The wall: two blocks of stone going up at (4, 64, 5) and (4, 65, 5).
	servertest.LadderScenario(t, srv, actor, watcher, entityID, 4, 64, 5)

	// The ladder in hand, and the actor standing a block south of the wall
	// looking north at it.
	ladder, _ := registry.ItemID("minecraft:ladder")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: ladder, Count: 1})
	watcher.ExpectEquipment(entityID)

	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.FacePlayer(4.5, 64, 6.5, 180, 0)
	actor.Settle()

	// The click lands on the lower wall block's south face, at the height
	// of the actor's eyes. The ladder goes into (4, 65, 6) - the cell the
	// actor's own body stands in - hanging on the wall behind it.
	actor.PlaceBlockAt(4, 65, 5, protocol.FaceSouth, 0.62)

	want := servertest.LadderState(t, "south")
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("ladder ack sequence = %d, want 1", seq)
	}
	// The place sound reaches the watcher, whose client did not predict
	// the placement, at the cell the ladder took, at once; the ladder
	// follows with the tick's flush.
	watcher.ExpectPlaceSound(t, rules.BlockPlaceSound(registry.BlockLadder).Name(), 4, 65, 6)
	got := watcher.ExpectBlockUpdate()
	if got.X != 4 || got.Y != 65 || got.Z != 6 {
		t.Errorf("ladder update at (%d,%d,%d), want (4,65,6)", got.X, got.Y, got.Z)
	}
	if got.State != want {
		t.Errorf("ladder state = %d, want %d (facing south)", got.State, want)
	}
	if world := srv.BlockAt(4, 65, 6); world != want {
		t.Errorf("world ladder = %d, want %d", world, want)
	}

	// The tail's echoes: the wall block the click landed on, unchanged,
	// and the face's cell, which is the ladder the placement put there.
	if echoes := actor.ExpectUseEchoes(4, 65, 5, protocol.FaceSouth); echoes[0] != 1 || echoes[1] != want {
		t.Errorf("tail echoes = %v, want the stone (%d) and the ladder (%d)",
			echoes, 1, want)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}

// TestALadderPopsWhenItsWallGoes takes the wall out from under a standing
// ladder: the ladder goes with it, the block update the shape update's
// answer broadcasts telling both players, and the ladder's own item drops
// where it hung.
func TestALadderPopsWhenItsWallGoes(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())

	watcher := connect().Join("Watcher")

	actor := connect().Join("Actor")

	entityID := watcher.ExpectPlayerAdded("Actor")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Actor")

	servertest.LadderScenario(t, srv, actor, watcher, entityID, 4, 64, 5)

	ladder, _ := registry.ItemID("minecraft:ladder")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: ladder, Count: 1})
	watcher.ExpectEquipment(entityID)

	actor.MoveToGround(4.5, 64, 6.5, true)
	actor.FacePlayer(4.5, 64, 6.5, 180, 0)
	actor.Settle()

	// The scenario's placements leave an arm swing in progress, which
	// swallows a new use swing until it is half over.
	servertest.RunTicks(t, srv, 4)
	actor.PlaceBlockAt(4, 65, 5, protocol.FaceSouth, 0.62)
	want := servertest.LadderState(t, "south")
	actor.ExpectBlockAck()
	got := watcher.ExpectBlockUpdate()
	if got.X != 4 || got.Y != 65 || got.Z != 6 || got.State != want {
		t.Fatalf("ladder update = (%d,%d,%d) state %d, want (4,65,6) facing south",
			got.X, got.Y, got.Z, got.State)
	}
	actor.ExpectUseEchoes(4, 65, 5, protocol.FaceSouth)

	// vanilla: BlockItem.place, ServerGamePacketListenerImpl.handleUseItemOn
	swing := protocol.NewReader(watcher.ExpectPlay(protocol.IDPlaySwingAnimation))
	if got := swing.ReadVarInt(); got != entityID {
		t.Errorf("placement swing entity = %d, want %d", got, entityID)
	}
	if got := swing.ReadVarInt(); got != protocol.HandMain {
		t.Errorf("placement swing hand = %d, want main hand", got)
	}
	if err := swing.Err(); err != nil {
		t.Fatalf("placement swing: %v", err)
	}

	// The wall block goes; the ladder hanging on it pops with it.
	actor.BreakBlock(4, 65, 5)

	// The drop the pop laid goes out at once, ahead of the flush that
	// tells both cells.
	watcher.ExpectDroppedItem()
	got = watcher.ExpectBlockUpdate()
	if got.X != 4 || got.Y != 65 || got.Z != 5 || got.State != 0 {
		t.Errorf("break update = (%d,%d,%d) state %d, want (4,65,5) air",
			got.X, got.Y, got.Z, got.State)
	}
	got = watcher.ExpectBlockUpdate()
	if got.X != 4 || got.Y != 65 || got.Z != 6 || got.State != 0 {
		t.Errorf("pop update = (%d,%d,%d) state %d, want (4,65,6) air",
			got.X, got.Y, got.Z, got.State)
	}
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("break ack sequence = %d, want 1", seq)
	}
	if a, b := srv.BlockAt(4, 65, 5), srv.BlockAt(4, 65, 6); a != 0 || b != 0 {
		t.Errorf("world after the pop: wall %d ladder %d, want both air", a, b)
	}

	// The popped ladder's item drops where it hung.
	drop := actor.ExpectDroppedItem()
	if id, _ := registry.ItemID("minecraft:ladder"); drop.Stack.ItemID != id {
		t.Errorf("dropped item = %d, want the ladder (%d)", drop.Stack.ItemID, id)
	}

	// The ladder fell in the actor's own cell, so the actor collects it a
	// few ticks later: the held stack grows from one ladder to two, which
	// reaches the watcher as the collection's Pickup Item and Remove
	// Entities, then one tick later as one more Set Equipment.
	watcher.ExpectPickup(drop.EntityID, entityID)
	entries := watcher.ExpectEquipment(entityID)
	if main := servertest.EquipEntry(entries, 0); main.ItemID != ladder || main.Count != 2 {
		t.Errorf("held stack after the pickup = %d x%d, want the ladder (%d) x2",
			main.ItemID, main.Count, ladder)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(entityID, "Actor")
}
