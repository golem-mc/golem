package entities

// The thrown bodies' hits on item frames: a snowball or an egg meets the
// frame's sliver ahead of the wall behind it and runs the frame's hurt
// before its own break.

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
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

// TestAThrownEggBreaksAnEmptyFrame throws an egg at an empty frame: the
// hurt takes the frame down with the break sound and the egg breaks on it.
// vanilla: ItemFrame.hurtServer, BlockAttachedEntity.hurtServer
func TestAThrownEggBreaksAnEmptyFrame(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	hung := hangThrownFrame(t, actor, watcher, false)

	servertest.HoldTicks(t, srv)
	egg := srv.ThrowThrown(entity.ThrownEgg, servertest.WallFrameX, servertest.WallFrameY, 1.5, 0, 0, 0, 7)
	if egg == 0 {
		t.Fatal("the egg never flew")
	}
	traffic := servertest.ReadThrownTraffic(watcher, srv, 80)
	if !traffic.RemovedOnce(hung.EntityID) {
		t.Error("the egg's hit did not take the frame out of every view exactly once")
	}
	broke := false
	for _, s := range traffic.Sounds {
		if s.Name == "minecraft:entity.item_frame.break" {
			broke = true
		}
	}
	if !broke {
		t.Errorf("no frame break sound played: %+v", traffic.Sounds)
	}
	if n := srv.FrameCount(); n != 0 {
		t.Errorf("the server holds %d frames after the throw, want none", n)
	}
	if !traffic.RemovedOnce(egg) {
		t.Error("the egg was not taken out of the world exactly once")
	}
}

// TestASnowballLeavesAFixedFrameStanding throws a snowball at a fixed
// frame: the hurt is refused, the frame keeps its item, and the ball breaks
// on it all the same.
// vanilla: ItemFrame.hurtServer, ItemFrame.canHurtWhenFixed
func TestASnowballLeavesAFixedFrameStanding(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	actor, watcher, actorEntityID := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	hangThrownFrame(t, actor, watcher, true)

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(actorEntityID, "Actor")
	srv.FlushMobs()
	srv.Shutdown()

	editFrameEntity(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:item_frame", func(compound nbt.Compound) {
		compound["Fixed"] = nbt.Byte(1)
	})

	restarted := *cfg
	againSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	again.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypeItemFrame: "frame",
	})

	servertest.HoldTicks(t, againSrv)
	ball := againSrv.ThrowThrown(entity.ThrownSnowball, servertest.WallFrameX, servertest.WallFrameY, 1.5, 0, 0, 0, 7)
	if ball == 0 {
		t.Fatal("the snowball never flew")
	}
	traffic := servertest.ReadThrownTraffic(again, againSrv, 80)
	if !traffic.RemovedOnce(ball) {
		t.Error("the snowball was not spent on the fixed frame")
	}
	if traffic.SpawnOfType(protocol.EntityTypeItem) != nil {
		t.Error("the snowball laid an item from a fixed frame")
	}
	if n := againSrv.FrameCount(); n != 1 {
		t.Errorf("the server holds %d frames, want the fixed one", n)
	}
}
