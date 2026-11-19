package entities

// The thrown bodies' hits on item frames: a snowball or an egg meets the
// frame's sliver ahead of the wall behind it and runs the frame's hurt
// before its own break.

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
)

// hangThrownFrame hangs an item frame on the standard wall from a creative
// hand, optionally filled with a diamond, and steps the actor out of the
// line a body flies at it.
func hangThrownFrame(t *testing.T, actor, watcher *servertest.Client, filled bool) servertest.FrameArrival {
	t.Helper()
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)
	if filled {
		actor.SetCreativeSlot(player.HotbarSlot, diamondStack(t, 2))
		actor.UseOn(hung.EntityID)
		if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); got.Empty() {
			t.Fatal("the frame's slot stayed empty after the use")
		}
	}
	actor.MoveToGround(2.5, 64, 3.5, true)
	actor.Settle()
	return hung
}

// TestASnowballPopsAFrameItem throws a snowball at a filled frame: the
// hurt pops the diamond out with the remove-item sound, the frame stays,
// and the ball breaks on it.
// vanilla: ItemFrame.hurtServer
func TestASnowballPopsAFrameItem(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	hung := hangThrownFrame(t, actor, watcher, true)

	servertest.HoldTicks(t, srv)
	ball := srv.ThrowThrown(entity.ThrownSnowball, servertest.WallFrameX, servertest.WallFrameY, 1.5, 0, 0, 0, 7)
	if ball == 0 {
		t.Fatal("the snowball never flew")
	}
	traffic := servertest.ReadThrownTraffic(watcher, srv, 80)
	popped := false
	for _, s := range traffic.Sounds {
		if s.Name == "minecraft:entity.item_frame.remove_item" {
			popped = true
		}
	}
	if !popped {
		t.Errorf("no remove_item sound played: %+v", traffic.Sounds)
	}
	drop := traffic.SpawnOfType(protocol.EntityTypeItem)
	if drop == nil {
		t.Fatal("the pop laid no item")
	}
	if n := srv.FrameCount(); n != 1 {
		t.Errorf("the server holds %d frames, want the emptied one", n)
	}
	if traffic.RemovedOnce(hung.EntityID) {
		t.Error("the snowball took the frame down, want only its item")
	}
	if !traffic.RemovedOnce(ball) {
		t.Error("the snowball was not taken out of the world exactly once")
	}
}
