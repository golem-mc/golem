package entities

// The item frame tests: the HangingEntityItem's placement on any face
// including the flat ones, the item and the eight positions a use puts in
// and turns through, the drop a hit pops out ahead of the frame's own on
// the second, the comparator's read of the rotation behind a conducting
// block, and the save that carries a frame across a restart.

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"github.com/golem-mc/golem/internal/server/servertest"
	"io"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// diamondStack is the filled-frame tests' held stack.
func diamondStack(t *testing.T, count int32) protocol.ItemStack {
	return servertest.ItemStack(t, "minecraft:diamond", count)
}

// frameRotation reads until the connection hears a metadata change for the
// frame, and answers the rotation it carries.
func frameRotation(t *testing.T, c *servertest.Client, entityID int32) int32 {
	t.Helper()
	return servertest.FrameMetadataField(t, c, entityID, protocol.MetadataIndexFrameRotation).VarintVal
}

// TestItemFramesHangOnAnyFace places frames on a wall, a floor and a
// ceiling: unlike a painting's item, the frame's accepts the flat faces
// too, hanging in the cell facing the clicked face whatever way it runs.
// The spawn names the box's centre and the body angles vanilla's
// setDirection leaves: the horizontal face's own yaw, and for the flat
// ones ninety degrees of pitch.
func TestItemFramesHangOnAnyFace(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, actorEntityID := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	// A lone overhang whose underside carries the ceiling's frame.
	if !srv.SetBlock(6, 66, 5, servertest.PaintingWallState) {
		t.Fatal("the overhang's chunk is not loaded")
	}

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	// A block north of the wall, looking at its north face, in reach of the
	// wall, the wall's top and the overhang's underside.
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()

	// The north face of the lower wall block: the frame hangs in the cell
	// in front of it, facing north, whose body yaw is a hundred and eighty.
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	if n := srv.FrameCount(); n != 1 {
		t.Fatalf("the server holds %d frames, want the hung one", n)
	}
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("frame ack sequence = %d, want 1", seq)
	}
	if echoes := actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth); echoes[0] != servertest.PaintingWallState || echoes[1] != 0 {
		t.Errorf("wall echoes = %v, want the stone and the air", echoes)
	}

	// The top face of the wall answers with a frame hanging in the cell
	// above it, near the floor surface, facing up: a bottom-of-cell centre
	// and ninety degrees of pitch, read back as the 270 the byte carries.
	actor.PlaceBlock(4, 65, 5, protocol.FaceTop)
	watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, 4.5, 66.03125, 5.5, 0, 270, protocol.FaceTop)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", 4.5, 66.03125, 5.5)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 65, 5, protocol.FaceTop)
	if n := srv.FrameCount(); n != 2 {
		t.Fatalf("the server holds %d frames, want the floor's too", n)
	}

	// The overhang's bottom face hangs its frame under it, facing down.
	actor.MoveToGround(6.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(6, 66, 5, protocol.FaceBottom)
	watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, 6.5, 65.96875, 5.5, 0, 90, protocol.FaceBottom)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", 6.5, 65.96875, 5.5)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(6, 66, 5, protocol.FaceBottom)
	if n := srv.FrameCount(); n != 3 {
		t.Errorf("the server holds %d frames, want the ceiling's too", n)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(actorEntityID, "Actor")
}

// TestASecondFrameOrAPaintingOnTheSameWallIsRefused covers the coexistence
// half of the survives check: no painting and no other frame runs across a
// hung one, so a second frame on the same face, a painting over a frame
// and a frame over a painting all fail, each consuming the click without
// taking the item.
// vanilla: HangingEntity.canCoexist, HangingEntityItem.useOn
func TestASecondFrameOrAPaintingOnTheSameWallIsRefused(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	// The same face again: the second frame's box runs across the first's,
	// and the click is answered with nothing at all.
	servertest.HoldTicks(t, srv)
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	actor.SettleKeeping()
	if servertest.ASpawnOfWithinTicks(watcher, srv, 60, protocol.EntityTypeItemFrame) {
		t.Error("a second frame was hung over the first")
	}
	if seq := actor.ExpectBlockAck(); seq != 1 {
		t.Errorf("second use ack sequence = %d, want 1", seq)
	}
	if n := srv.FrameCount(); n != 1 {
		t.Errorf("the server holds %d frames, want the one", n)
	}

	// A kebab over the frame is just as refused: the painting's box runs
	// across the frame's, so survives fails and useOn hangs nothing.
	kebab := servertest.PaintingVariantID(t, "minecraft:kebab")
	if dims := registry.PaintingVariants[kebab]; dims.Width != 1 || dims.Height != 1 {
		t.Fatalf("kebab is %dx%d, want the 1x1 this test relies on", dims.Width, dims.Height)
	}
	actor.SetCreativeSlot(player.HotbarSlot, servertest.PaintingStack(t, kebab))
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	actor.SettleKeeping()
	if servertest.ASpawnOfWithinTicks(watcher, srv, 60, protocol.EntityTypePainting) {
		t.Error("a painting was hung over the frame")
	}
	actor.ExpectBlockAck()

	// And the frame over a hung painting, the other direction of the same
	// check, on the wall's second block.
	actor.PlaceBlock(4, 65, 5, protocol.FaceNorth)
	actor.SettleKeeping()
	servertest.WaitFor(t, srv, 60, func() bool { return srv.PaintingCount() == 1 })
	servertest.RunTicks(t, srv, 2)
	pained := watcher.ExpectPaintingSpawn(t, protocol.FaceNorth, 4, 65, 4)
	if pained.Variant != kebab {
		t.Fatalf("the wall's second block was hung with variant %d, want the preset kebab", pained.Variant)
	}
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 65, 5, protocol.FaceNorth)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.PlaceBlock(4, 65, 5, protocol.FaceNorth)
	actor.SettleKeeping()
	if servertest.ASpawnOfWithinTicks(watcher, srv, 60, protocol.EntityTypeItemFrame) {
		t.Error("a frame was hung over the painting")
	}
	actor.ExpectBlockAck()
	if n := srv.FrameCount(); n != 1 {
		t.Errorf("the server holds %d frames after the refusals, want the one", n)
	}
}

// TestGlowItemFramesHoldAndRotateTheirItem puts a stack in a glow
// frame — its slot kept to one of the stack — and turns it through the
// eight positions with further uses, the hand held or empty: each turn
// plays the glow frame's own sound. A creative blow then empties the
// frame without dropping anything, and the second takes the frame the
// same way.
func TestGlowItemFramesHoldAndRotateTheirItem(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:glow_item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeGlowItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.glow_item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)
	if held := hung.Item; !held.Empty() {
		t.Fatalf("the fresh frame holds %+v, want nothing", held)
	}

	// A stack put in: the frame keeps one of it, whatever the hand holds.
	actor.SetCreativeSlot(player.HotbarSlot, diamondStack(t, 10))
	actor.UseOn(hung.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); got.ItemID != diamondStack(t, 1).ItemID || got.Count != 1 {
		t.Fatalf("the frame holds %+v after the use, want one diamond", got)
	}
	watcher.ExpectPaintingSound(t, "minecraft:entity.glow_item_frame.add_item", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)

	// Each further use turns the item one position over, eight in all, the
	// fourth turn back to the first position's successor.
	for turn := int32(1); turn <= 8; turn++ {
		actor.UseOn(hung.EntityID)
		if got := frameRotation(t, watcher, hung.EntityID); got != turn%8 {
			t.Fatalf("the frame's rotation = %d after %d uses, want %d", got, turn, turn%8)
		}
		watcher.ExpectPaintingSound(t, "minecraft:entity.glow_item_frame.rotate_item", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	}

	// A creative blow takes the frame's item only: nothing drops, the
	// frame hangs on, and the second blow takes the frame the same way.
	actor.Attack(hung.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); !got.Empty() {
		t.Fatalf("the creative blow left the frame holding %+v, want nothing", got)
	}
	watcher.ExpectPaintingSound(t, "minecraft:entity.glow_item_frame.remove_item", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	if n := srv.FrameCount(); n != 1 {
		t.Fatalf("the server holds %d frames after the blow, want the emptied one", n)
	}
	actor.Attack(hung.EntityID)
	removed, sound, dropped := false, false, false
	servertest.HoldTicks(t, srv)
	watcher.ReadTicks(srv, 60, func(packetID int32, data []byte) bool {
		switch packetID {
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			if r.ReadVarInt() == 1 && r.ReadVarInt() == hung.EntityID {
				removed = true
			}
		case protocol.IDPlaySoundEffect:
			r := protocol.NewReader(data)
			if id, ok := registry.SoundEventID("minecraft:entity.glow_item_frame.break"); ok && r.ReadVarInt() == id+1 {
				sound = true
			}
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeItem {
				dropped = true
			}
		}
		return false
	})
	if !removed {
		t.Error("the second blow left the frame in the world")
	}
	if !sound {
		t.Error("the frame came down without its break sound")
	}
	if dropped {
		t.Error("a creative blow laid something behind the frame")
	}
	if n := srv.FrameCount(); n != 0 {
		t.Errorf("the server holds %d frames after the second blow, want none", n)
	}
}

// TestASurvivalBlowPopsTheItemAndTheNextBreaksTheFrame is the survival
// side of the hit: one blow drops the held item with the frame still
// hanging, and the blow after breaks the frame and lays the frame item
// behind. The use that filled the frame spends one of the held stack.
func TestASurvivalBlowPopsTheItemAndTheNextBreaksTheFrame(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	actor, watcher, actorEntityID := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	actor.ExpectBlockAck()
	hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)
	if held := servertest.EquipEntry(watcher.ExpectEquipment(actorEntityID), 0); !held.Empty() {
		t.Fatalf("the survival placement left the hand holding %+v, want it spent", held)
	}

	// Two diamonds in hand: one goes in the frame and the other stays
	// held, the use's cost of the fill visible where the hand is.
	srv.GiveSlot("Actor", player.HotbarSlot, diamondStack(t, 2))
	actor.UseOn(hung.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); got.ItemID != diamondStack(t, 1).ItemID || got.Count != 1 {
		t.Fatalf("the frame holds %+v after the use, want one diamond", got)
	}
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.add_item", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	if held := servertest.EquipEntry(watcher.ExpectEquipment(actorEntityID), 0); held.ItemID != diamondStack(t, 1).ItemID || held.Count != 1 {
		t.Fatalf("the hand holds %+v after the use, want the one diamond left of two", held)
	}

	// The blow pops the diamond out and leaves the frame hung.
	actor.Attack(hung.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); !got.Empty() {
		t.Fatalf("the blow left the frame holding %+v, want nothing", got)
	}
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.remove_item", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	if dropped := servertest.ExpectDroppedStack(t, watcher); dropped.ItemID != diamondStack(t, 1).ItemID || dropped.Count != 1 {
		t.Fatalf("the blow dropped %+v, want the frame's diamond", dropped)
	}
	if n := srv.FrameCount(); n != 1 {
		t.Fatalf("the server holds %d frames after the first blow, want the emptied one", n)
	}

	// The second blow breaks the frame and lays its own item behind.
	actor.Attack(hung.EntityID)
	watcher.ExpectPaintingRemoval(t, hung.EntityID)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.break", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	if dropped := servertest.ExpectDroppedStack(t, watcher); dropped.ItemID != servertest.FrameItemID(t, "minecraft:item_frame") || dropped.Count != 1 {
		t.Fatalf("the broken frame dropped %+v, want the frame item", dropped)
	}
	if n := srv.FrameCount(); n != 0 {
		t.Errorf("the server holds %d frames after the second blow, want none", n)
	}
}

// TestAnAdventureHandPaysForAFrameFill fills a frame from an adventure
// hand: the use spends one of the held stack, the cost vanilla's consume
// takes from every hand without infinite materials. The frame is hung from
// a creative hand first, since mayBuild refuses the placement itself.
// vanilla: ItemFrame.interact, ItemStack.consume, Player.hasInfiniteMaterials
func TestAnAdventureHandPaysForAFrameFill(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Actor"))
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	actor.ExpectBlockAck()
	hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	actor.ChatCommand("gamemode adventure")
	servertest.PollUntil(t, 10*time.Second, func() bool {
		return srv.PlayerGameMode("Actor") == protocol.GameModeAdventure
	})
	actor.Settle()
	watcher.Settle()

	// Two diamonds in an adventure hand: one goes in the frame and the
	// other stays held.
	srv.GiveSlot("Actor", player.HotbarSlot, diamondStack(t, 2))
	actor.UseOn(hung.EntityID)
	actor.Settle()
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); got.ItemID != diamondStack(t, 1).ItemID || got.Count != 1 {
		t.Fatalf("the frame holds %+v after the adventure use, want one diamond", got)
	}
	if held := srv.InventorySlot("Actor", player.HotbarSlot); held.ItemID != diamondStack(t, 1).ItemID || held.Count != 1 {
		t.Fatalf("the adventure hand holds %+v after the fill, want the one diamond left of two", held)
	}
}

// TestACreativeHandKeepsItsStackWhenFillingAFrame fills a frame from a
// creative hand: infinite materials leave the held stack whole.
// vanilla: ItemFrame.interact, ItemStack.consume, Player.hasInfiniteMaterials
func TestACreativeHandKeepsItsStackWhenFillingAFrame(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	actor.ExpectBlockAck()
	hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	// Two diamonds in a creative hand fill the frame and stay two.
	actor.SetCreativeSlot(player.HotbarSlot, diamondStack(t, 2))
	actor.UseOn(hung.EntityID)
	actor.Settle()
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); got.ItemID != diamondStack(t, 1).ItemID || got.Count != 1 {
		t.Fatalf("the frame holds %+v after the creative use, want one diamond", got)
	}
	if held := srv.InventorySlot("Actor", player.HotbarSlot); held.ItemID != diamondStack(t, 1).ItemID || held.Count != 2 {
		t.Fatalf("the creative hand holds %+v after the fill, want both diamonds kept", held)
	}
}

// TestASurvivalBlowGivesAFramesDropItsToss is the frame's half of the
// hanging drop the painting lays: the item a blow pops out hops with the
// ItemEntity constructor's 0.2 upward and a random nudge sideways, not laid
// still at the frame's centre.
// vanilla: HangingEntity.spawnAtLocation, ItemEntity.<init>
func TestASurvivalBlowGivesAFramesDropItsToss(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	actor.ExpectBlockAck()
	hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	srv.GiveSlot("Actor", player.HotbarSlot, diamondStack(t, 1))
	actor.UseOn(hung.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); got.ItemID != diamondStack(t, 1).ItemID {
		t.Fatalf("the frame holds %+v after the use, want the diamond", got)
	}
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.add_item", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)

	actor.Attack(hung.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); !got.Empty() {
		t.Fatalf("the blow left the frame holding %+v, want nothing", got)
	}
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.remove_item", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	dropped := watcher.AwaitSpawn(protocol.EntityTypeItem)
	if math.Abs(dropped.Vy-0.2) > 0.01 {
		t.Errorf("the frame's drop upward velocity = %v, want 0.2", dropped.Vy)
	}
	if math.Abs(dropped.Vx) > 0.11 || math.Abs(dropped.Vz) > 0.11 {
		t.Errorf("the frame's drop sideways velocity = (%v, %v), want each within ±0.1", dropped.Vx, dropped.Vz)
	}
}

// blinkFrameTestSource drops the redstone block feeding the conductor's
// dust and puts it back, so the comparator reads its input again: hanging a
// frame wakes nothing, in vanilla or here.
func blinkFrameTestSource(t *testing.T, c *servertest.Client) {
	t.Helper()
	servertest.Setblock(t, c, 4, 65, 2, "minecraft:air")
	c.Settle()
	servertest.Setblock(t, c, 4, 65, 2, "minecraft:redstone_block")
	c.Settle()
}

// TestAComparatorReadsAFrameBehindAConductor hangs the frame a comparator
// reads: on the far side of a conducting block, facing the comparator's
// way, the cell beyond the block being the one the frame's box lives in.
// The empty frame reads nothing, a held item reads the rotation plus one,
// and every turn reaches the comparator through the block.
// vanilla: ComparatorBlock.getInputSignal, ItemFrame.getAnalogOutput
func TestAComparatorReadsAFrameBehindAConductor(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 0)
	// The comparator's input cell is a stone, which the frame hangs on
	// from its far side: the frame's cell is two ahead of the comparator.
	servertest.Setblock(t, c, 4, 64, 0, "minecraft:stone")
	c.Settle()

	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	c.MoveToGround(2.5, 64, 0.5, true)
	c.FacePlayer(4.5, 64, 0.5, -90, 0)
	c.Settle()
	c.PlaceBlock(4, 64, 0, protocol.FaceWest)
	const frameX = 3.96875 // the box centre, off the stone's west face
	hung := c.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, frameX, 64.5, 0.5, 90, 0, protocol.FaceWest)
	c.ExpectBlockAck()
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "0")

	// The empty frame is worth nothing; the diamond in it, at its first
	// position, is worth one.
	c.SetCreativeSlot(player.HotbarSlot, diamondStack(t, 2))
	c.UseOn(hung.EntityID)
	if got := servertest.FrameItemSlot(t, c, hung.EntityID); got.ItemID != diamondStack(t, 1).ItemID {
		t.Fatal("the frame's slot stayed empty after the use")
	}
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "1")

	// Each turn raises the wire one, and the eighth wraps it back to one.
	for turn := 1; turn <= 7; turn++ {
		c.UseOn(hung.EntityID)
		if got := frameRotation(t, c, hung.EntityID); got != int32(turn) {
			t.Fatalf("the frame's rotation = %d after %d turns, want %d", got, turn, turn)
		}
		servertest.ExpectWirePower(t, srv, 6, 64, 0, strconv.Itoa(turn+1))
	}
	c.UseOn(hung.EntityID)
	if got := frameRotation(t, c, hung.EntityID); got != 0 {
		t.Fatalf("the frame's rotation = %d after the eighth turn, want the wrapped 0", got)
	}
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "1")

	// The blow that pops the diamond takes the wire back to nothing, the
	// change reaching the comparator the way the turns did.
	c.Attack(hung.EntityID)
	if got := servertest.FrameItemSlot(t, c, hung.EntityID); !got.Empty() {
		t.Fatal("the blow left the frame holding the diamond")
	}
	c.ExpectPaintingSound(t, "minecraft:entity.item_frame.remove_item", frameX, 64.5, 0.5)
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "0")
}

// TestAnItemFrameSurvivesARestart hangs one, fills it and turns the item,
// saves the world, and stands a server back up on it: the entity file
// holds the anchor cell, the direction, the item and its rotation — all
// the vanilla ItemFrame.addAdditionalSaveData writes — and the frame
// comes back hung, filled and turned to the same place.
func TestAnItemFrameSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	actor, watcher, actorEntityID := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	actor.SetCreativeSlot(player.HotbarSlot, diamondStack(t, 2))
	actor.UseOn(hung.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); got.ItemID != diamondStack(t, 1).ItemID {
		t.Fatal("the frame's slot stayed empty after the use")
	}
	actor.UseOn(hung.EntityID)
	if got := frameRotation(t, watcher, hung.EntityID); got != 1 {
		t.Fatalf("the frame's rotation = %d, want the one turn", got)
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(actorEntityID, "Actor")
	srv.FlushMobs()
	srv.Shutdown()

	// On disk the compound is the vanilla one.
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:item_frame", func(compound nbt.Compound) {
		identity, ok := nbt.Get[nbt.IntArray](compound, "UUID")
		if !ok || len(identity) != 4 {
			t.Fatal("the saved frame has no four-word UUID")
		}
		want := servertest.UuidWords(hung.Identity)
		for i := range want {
			if identity[i] != want[i] {
				t.Errorf("saved UUID = %v, want %v (the hung frame's)", identity, want)
				break
			}
		}
		if facing, ok := nbt.Get[nbt.Byte](compound, "Facing"); !ok || facing != 2 {
			t.Errorf("Facing = %v, %v; want 2 (north's 3D data value)", facing, ok)
		}
		pos, ok := nbt.Get[nbt.IntArray](compound, "block_pos")
		if !ok || len(pos) != 3 || pos[0] != 4 || pos[1] != 64 || pos[2] != 4 {
			t.Errorf("block_pos = %v, %v; want the hanging cell (4, 64, 4)", pos, ok)
		}
		item, ok := nbt.Get[nbt.Compound](compound, "Item")
		if !ok {
			t.Fatal("the saved frame has no Item")
		}
		if id, _ := nbt.Get[nbt.String](item, "id"); string(id) != "minecraft:diamond" {
			t.Errorf("Item id = %q, want a diamond", string(id))
		}
		if count, ok := nbt.Get[nbt.Int](item, "count"); !ok || count != 1 {
			t.Errorf("Item count = %v, %v; want 1", count, ok)
		}
		if rotation, ok := nbt.Get[nbt.Byte](compound, "ItemRotation"); !ok || rotation != 1 {
			t.Errorf("ItemRotation = %v, %v; want 1 (one turn)", rotation, ok)
		}
		if v, ok := nbt.Get[nbt.Byte](compound, "Invisible"); ok && v != 0 {
			t.Errorf("Invisible = %v, want unset", v)
		}
		if v, ok := nbt.Get[nbt.Byte](compound, "Fixed"); ok && v != 0 {
			t.Errorf("Fixed = %v, want unset", v)
		}
	})

	// A restarted server hangs the frame back where it was, with the item
	// and the turn the save carried.
	restarted := *cfg
	againSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	shown := again.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypeItemFrame: "frame",
	})
	back := servertest.CapturedOr(t, again, shown, "frame", protocol.EntityTypeItemFrame)
	servertest.ExpectUUID(t, back.Uuid, hung.Identity, "frame")
	if f := servertest.MetaOf(back.Meta, protocol.MetadataIndexHangingDirection); f == nil || f.VarintVal != protocol.FaceNorth {
		t.Errorf("the restored frame's direction = %v, want north", f)
	}
	if f := servertest.MetaOf(back.Meta, protocol.MetadataIndexFrameItem); f == nil || f.SlotVal.ItemID != diamondStack(t, 1).ItemID || f.SlotVal.Count != 1 {
		t.Errorf("the restored frame's item = %v, want one diamond", f)
	}
	if f := servertest.MetaOf(back.Meta, protocol.MetadataIndexFrameRotation); f == nil || f.VarintVal != 1 {
		t.Errorf("the restored frame's rotation = %v, want 1 (the turned one)", f)
	}
	if n := againSrv.FrameCount(); n != 1 {
		t.Errorf("the restarted server holds %d frames, want the saved one", n)
	}
}

// TestAFixedFrameKeepsHangingWithoutSupport reloads a frame its save marks
// Fixed and Invisible: the invisible bit rides its metadata flags, the
// wall's loss cannot take it down — Fixed defeats the survives check the
// wall answers — and the use that would turn its item is refused.
func TestAFixedFrameKeepsHangingWithoutSupport(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	actor, watcher, actorEntityID := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)
	actor.SetCreativeSlot(player.HotbarSlot, diamondStack(t, 2))
	actor.UseOn(hung.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); got.ItemID != diamondStack(t, 1).ItemID {
		t.Fatal("the frame's slot stayed empty after the use")
	}

	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(actorEntityID, "Actor")
	srv.FlushMobs()
	srv.Shutdown()

	// The save edit a fixed, invisible frame would have come with: the two
	// flags set over the frame the test hung.
	editFrameEntity(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:item_frame", func(compound nbt.Compound) {
		compound["Fixed"] = nbt.Byte(1)
		compound["Invisible"] = nbt.Byte(1)
	})

	restarted := *cfg
	againSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	shown := again.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypeItemFrame: "frame",
	})
	back := servertest.CapturedOr(t, again, shown, "frame", protocol.EntityTypeItemFrame)
	servertest.ExpectUUID(t, back.Uuid, hung.Identity, "frame")
	if flags := servertest.MetaOf(back.Meta, protocol.MetadataIndexEntityFlags); flags == nil || flags.ByteVal&protocol.EntityFlagInvisible == 0 {
		t.Errorf("the fixed frame's flags = %v, want the invisible bit set", flags)
	}
	if f := servertest.MetaOf(back.Meta, protocol.MetadataIndexFrameItem); f == nil || f.SlotVal.ItemID == 0 {
		t.Fatalf("the restored frame holds nothing, want the diamond it was saved with")
	}

	// The wall goes; the fixed frame stays the way it was, whatever the
	// recheck the loss runs and the survives poll think of it.
	again.MoveToGround(3.5, 64, 4.5, true)
	again.Settle()
	again.BreakBlock(4, 64, 5)
	if n := againSrv.FrameCount(); n != 1 {
		t.Fatalf("the fixed frame came down with its wall: %d frames left", n)
	}
	servertest.HoldTicks(t, againSrv)
	servertest.AwaitTicks(t, againSrv, 150)
	if n := againSrv.FrameCount(); n != 1 {
		t.Errorf("the survives poll took the fixed frame down: %d frames left", n)
	}

	// The use is refused: no turn, no sound, the item kept as saved.
	again.SetCreativeSlot(player.HotbarSlot, diamondStack(t, 2))
	again.UseOn(back.EntityID)
	again.ExpectNoneWithinTicks(againSrv, 10,
		servertest.PacketIs(protocol.IDPlayEntityMetadata, protocol.IDPlaySoundEffect, protocol.IDPlayRemoveEntities))
	if n := againSrv.FrameCount(); n != 1 {
		t.Fatalf("the uses changed the fixed frame's world: %d frames left", n)
	}

	// A creative blow still takes it — the one hurt the Fixed flag cannot
	// refuse — and lays nothing behind it.
	again.Attack(back.EntityID)
	removed, sound, dropped := false, false, false
	again.ReadTicks(againSrv, 60, func(packetID int32, data []byte) bool {
		switch packetID {
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			if r.ReadVarInt() == 1 && r.ReadVarInt() == back.EntityID {
				removed = true
			}
		case protocol.IDPlaySoundEffect:
			r := protocol.NewReader(data)
			if id, ok := registry.SoundEventID("minecraft:entity.item_frame.break"); ok && r.ReadVarInt() == id+1 {
				sound = true
			}
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeItem {
				dropped = true
			}
		}
		return false
	})
	if !removed || !sound {
		t.Errorf("the creative blow took the fixed frame = %v with its sound = %v, want both", removed, sound)
	}
	if dropped {
		t.Error("a creative blow laid something behind the fixed frame")
	}
	if n := againSrv.FrameCount(); n != 0 {
		t.Errorf("the server holds %d frames after the blow, want none", n)
	}
}

// TestACommandBlockWriteLeavesFrameSupportToTheScheduledPoll removes a frame's
// wall with /setblock while the loop is held: nothing comes down with the write, only the
// poll on the frame's 101st tick.
// vanilla: BlockAttachedEntity.tick
func TestACommandBlockWriteLeavesFrameSupportToTheScheduledPoll(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Actor"))
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.HoldTicks(t, srv)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	actor.ChatCommand("setblock 4 64 5 minecraft:air")
	actor.Settle()
	if registry.BlockOf(srv.BlockAt(4, 64, 5)) != registry.BlockAir {
		t.Fatal("the wall was not removed")
	}
	if n := srv.FrameCount(); n != 1 {
		t.Fatalf("the block write took the frame down at once: %d frames left", n)
	}
	servertest.RunTicks(t, srv, 100)
	if n := srv.FrameCount(); n != 1 {
		t.Fatalf("the frame came down before tick 101: %d frames", n)
	}
	watcher.ReadTicks(srv, 0, func(id int32, _ []byte) bool {
		if id == protocol.IDPlayRemoveEntities {
			t.Error("early frame removal packet")
		}
		return false
	})
	servertest.RunTicks(t, srv, 1)
	if n := srv.FrameCount(); n != 0 {
		t.Fatalf("the poll left the unsupported frame: %d frames", n)
	}
	watcher.ExpectWithinTicks(srv, 0, protocol.IDPlayRemoveEntities)
}

// TestAnExplosionBreaksAFrameAndDropsBothItems primes a TNT beside a
// filled frame: the blast's hurt breaks the frame whatever the falloff,
// and the drops lay both the frame item and the held one.
func TestAnExplosionBreaksAFrameAndDropsBothItems(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, _, _ := servertest.PaintingWatchers(t, connect)
	obsidian := registry.MustState("minecraft:obsidian", nil)
	if !srv.SetBlock(4, 64, 5, obsidian) || !srv.SetBlock(4, 65, 5, obsidian) {
		t.Fatal("the wall's chunk is not loaded")
	}

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:glow_item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	got := actor.ExpectFrameSpawn(t, protocol.EntityTypeGlowItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	actor.ExpectPaintingSound(t, "minecraft:entity.glow_item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)
	actor.SetCreativeSlot(player.HotbarSlot, diamondStack(t, 2))
	actor.UseOn(got.EntityID)
	if got2 := servertest.FrameItemSlot(t, actor, got.EntityID); got2.ItemID != diamondStack(t, 1).ItemID {
		t.Fatal("the frame's slot stayed empty after the use")
	}

	if id := srv.SpawnTNT(4.5, 64, 1.5, 1); id == 0 {
		t.Fatal("the TNT never stood up")
	}
	// The frame leaves the count the moment it is removed, before the
	// drops are laid, so wait on the drops themselves, not the count.
	var drops map[string]bool
	servertest.WaitForTicks(t, srv, 20, func() bool {
		drops = map[string]bool{}
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if it.Item == "minecraft:glow_item_frame" || it.Item == "minecraft:diamond" {
				drops[it.Item] = true
			}
		}
		return drops["minecraft:glow_item_frame"] && drops["minecraft:diamond"]
	})
	if srv.BlockAt(4, 64, 5) != obsidian {
		t.Fatal("the blast destroyed the wall, so the frame's fall proves nothing")
	}
	if n := srv.FrameCount(); n != 0 {
		t.Fatalf("the server holds %d frames after the blast, want none", n)
	}

	// The blast laid the frame's own item and the diamond it held.
	if !drops["minecraft:glow_item_frame"] || !drops["minecraft:diamond"] {
		t.Errorf("the blast laid %v, want both the glow frame and the diamond", drops)
	}
}

// TestAnArrowPopsAFrameItemThenBreaksTheFrame shoots a filled frame: the
// shaft's hurt is the same a blow's is, dropping the held item and leaving
// the frame for the second shot. The arrows die on the hits.
// vanilla: ItemFrame.hurtServer, BlockAttachedEntity.hurtServer
func TestAnArrowPopsAFrameItemThenBreaksTheFrame(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)
	actor.SetCreativeSlot(player.HotbarSlot, diamondStack(t, 2))
	actor.UseOn(hung.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); got.ItemID != diamondStack(t, 1).ItemID {
		t.Fatal("the frame's slot stayed empty after the use")
	}

	// The shaft's line must miss the shooters: the actor steps out of it
	// before the bow works.
	actor.MoveToGround(2.5, 64, 3.5, true)
	actor.Settle()

	// The first arrow meets the frame's box ahead of the wall behind it,
	// and its hurt pops the item the way a blow's does.
	if id := srv.ShootArrow(4.5, 64.5, 1.5, 0, 0, 10); id == 0 {
		t.Fatal("the arrow never flew")
	}
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); !got.Empty() {
		t.Fatalf("the shot left the frame holding %+v, want nothing", got)
	}
	if dropped := servertest.ExpectDroppedStack(t, watcher); dropped.ItemID != diamondStack(t, 1).ItemID {
		t.Fatalf("the shot dropped %+v, want the frame's diamond", dropped)
	}
	if n := srv.FrameCount(); n != 1 {
		t.Fatalf("the server holds %d frames after the first shot, want the emptied one", n)
	}

	// The second takes the frame down: the removal out of every view, the
	// break sound, and the frame item laid — the spent shaft's own
	// removal rides the same window.
	servertest.HoldTicks(t, srv)
	if id := srv.ShootArrow(4.5, 64.5, 1.5, 0, 0, 10); id == 0 {
		t.Fatal("the arrow never flew")
	}
	removed, sound := false, false
	var laid protocol.ItemStack
	dropID, haveDrop := int32(0), false
	watcher.ReadTicks(srv, 60, func(packetID int32, data []byte) bool {
		switch packetID {
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			for i, n := int32(0), r.ReadVarInt(); i < n; i++ {
				if r.ReadVarInt() == hung.EntityID {
					removed = true
				}
			}
		case protocol.IDPlaySoundEffect:
			r := protocol.NewReader(data)
			if id, ok := registry.SoundEventID("minecraft:entity.item_frame.break"); ok && r.ReadVarInt() == id+1 {
				sound = true
			}
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			eid := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeItem {
				dropID, haveDrop = eid, true
			}
		case protocol.IDPlayEntityMetadata:
			if !haveDrop {
				return false
			}
			r := protocol.NewReader(data)
			if r.ReadVarInt() != dropID {
				return false
			}
			if fields, ok := servertest.ReadMetadata(r); ok {
				if f := servertest.MetaOf(fields, protocol.MetadataIndexItemSlot); f != nil {
					laid = f.SlotVal
				}
			}
		}
		return false
	})
	if !removed || !sound {
		t.Errorf("the second shot took the frame = %v with its sound = %v, want both", removed, sound)
	}
	if laid.ItemID != servertest.FrameItemID(t, "minecraft:item_frame") || laid.Count != 1 {
		t.Fatalf("the broken frame laid %+v, want the frame item", laid)
	}
	if n := srv.FrameCount(); n != 0 {
		t.Errorf("the server holds %d frames after the second shot, want none", n)
	}
}

// editFrameEntity rewrites a frame's compound of a chunk's entity file, the
// region-file edit the persistence tests use.
func editFrameEntity(t *testing.T, level string, pos world.ChunkPos, id string, edit func(nbt.Compound)) {
	t.Helper()
	servertest.EditChunkRecord(t, servertest.EntityRegionPath(level, pos), pos, func(record []byte) []byte {
		zr, err := zlib.NewReader(bytes.NewReader(record[5:]))
		if err != nil {
			t.Fatalf("decompress the entities: %v", err)
		}
		data, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("read the entities: %v", err)
		}
		_, tag, err := nbt.Decode(data)
		if err != nil {
			t.Fatalf("parse the entities: %v", err)
		}
		root, ok := tag.(nbt.Compound)
		if !ok {
			t.Fatalf("entities root is a %s, want a compound", tag.Type())
		}
		entities, _ := nbt.Get[nbt.List](root, "Entities")
		for i, item := range entities.Items {
			compound, ok := item.(nbt.Compound)
			if !ok {
				continue
			}
			if got, _ := nbt.Get[nbt.String](compound, "id"); string(got) != id {
				continue
			}
			edit(compound)
			entities.Items[i] = compound
		}
		encoded, err := nbt.Encode("", root)
		if err != nil {
			t.Fatalf("re-encode the entities: %v", err)
		}
		var compressed bytes.Buffer
		w := zlib.NewWriter(&compressed)
		if _, err := w.Write(encoded); err != nil {
			t.Fatalf("compress the entities: %v", err)
		}
		w.Close()
		out := append([]byte{0, 0, 0, 0, 2}, compressed.Bytes()...)
		binary.BigEndian.PutUint32(out[:4], uint32(len(out)-4))
		return out
	})
}

// TestAFixedFrameSurvivesABlast reloads a frame its save marks Fixed beside
// an unowned TNT: the blast cannot take it, an explosion being neither a
// source that bypasses invulnerability nor a creative player.
// vanilla: ItemFrame.hurtServer, ItemFrame.canHurtWhenFixed
func TestAFixedFrameSurvivesABlast(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	actor, watcher, actorEntityID := servertest.PaintingWatchers(t, connect)
	obsidian := registry.MustState("minecraft:obsidian", nil)
	if !srv.SetBlock(4, 64, 5, obsidian) || !srv.SetBlock(4, 65, 5, obsidian) {
		t.Fatal("the wall's chunk is not loaded")
	}

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

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

	// The unowned TNT beside the wall: the blast reaches the frame's box
	// and would take it were it not fixed.
	servertest.HoldTicks(t, againSrv)
	if id := againSrv.SpawnTNT(4.5, 64, 1.5, 1); id == 0 {
		t.Fatal("the TNT never stood up")
	}
	servertest.AwaitTicks(t, againSrv, 60)
	if n := againSrv.FrameCount(); n != 1 {
		t.Errorf("the blast took the fixed frame: %d frames left, want the one", n)
	}
}

// TestAFrameCannotHangOutsideTheBuildHeight places a frame item against the
// top face of the topmost block and the bottom face of the bottommost: the
// item's mayPlace refuses a cell outside the build height, so the click is
// spent and the frame stays in hand.
// vanilla: ItemFrameItem.mayPlace, Level.isInsideBuildHeight
func TestAFrameCannotHangOutsideTheBuildHeight(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	servertest.HoldTicks(t, srv)

	// The topmost block's top face: the frame would hang in y=320.
	if !srv.SetBlock(4, 319, 5, servertest.PaintingWallState) {
		t.Fatal("the top block's chunk is not loaded")
	}
	c.MoveToGround(4.5, 317, 3.5, true)
	c.PlaceBlock(4, 319, 5, protocol.FaceTop)
	if servertest.ASpawnOfWithinTicks(c, srv, 40, protocol.EntityTypeItemFrame) {
		t.Error("a frame hung above the build height")
	}
	if n := srv.FrameCount(); n != 0 {
		t.Errorf("the server holds %d frames, want none above the build height", n)
	}

	// The bottommost block's bottom face: the frame would hang in y=-65.
	// The stone band reaches y=-64, so the block is likely already there.
	srv.SetBlock(4, -64, 5, servertest.PaintingWallState)
	c.MoveToGround(4.5, -66, 3.5, true)
	c.PlaceBlock(4, -64, 5, protocol.FaceBottom)
	if servertest.ASpawnOfWithinTicks(c, srv, 40, protocol.EntityTypeItemFrame) {
		t.Error("a frame hung below the build height")
	}
	if n := srv.FrameCount(); n != 0 {
		t.Errorf("the server holds %d frames, want none below the build height", n)
	}
}

// TestAFramesDropsIgnoreTheFacingInY breaks a floor frame and a ceiling one:
// the drop is nudged along the facing in the ground plane only, its Y the
// box's centre plus the item's half-size — the feet an ItemEntity's position
// is measured from — not the facing's own step.
// vanilla: HangingEntity.spawnAtLocation, ItemEntity.<init>
func TestAFramesDropsIgnoreTheFacingInY(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	if !srv.SetBlock(6, 66, 5, servertest.PaintingWallState) {
		t.Fatal("the overhang's chunk is not loaded")
	}
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()

	// The floor frame's centre sits near the bottom of its cell, so a Y
	// nudge would show. Only survival pops a drop.
	actor.PlaceBlock(4, 65, 5, protocol.FaceTop)
	floor := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, 4.5, 66.03125, 5.5, 0, 270, protocol.FaceTop)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", 4.5, 66.03125, 5.5)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 65, 5, protocol.FaceTop)
	srv.GiveSlot("Actor", player.HotbarSlot, diamondStack(t, 2))
	actor.UseOn(floor.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, floor.EntityID); got.ItemID != diamondStack(t, 1).ItemID {
		t.Fatal("the frame's slot stayed empty after the use")
	}
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.add_item", 4.5, 66.03125, 5.5)
	actor.Attack(floor.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, floor.EntityID); !got.Empty() {
		t.Fatalf("the blow left the frame holding %+v, want nothing", got)
	}
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.remove_item", 4.5, 66.03125, 5.5)
	if drop := watcher.AwaitSpawn(protocol.EntityTypeItem); drop.Y != 66.03125 {
		t.Errorf("the floor frame's drop feet at y %v, want the box centre 66.03125", drop.Y)
	}

	// The ceiling frame hangs under the overhang, its centre near the top
	// of its cell.
	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(6.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(6, 66, 5, protocol.FaceBottom)
	ceiling := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, 6.5, 65.96875, 5.5, 0, 90, protocol.FaceBottom)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", 6.5, 65.96875, 5.5)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(6, 66, 5, protocol.FaceBottom)
	srv.GiveSlot("Actor", player.HotbarSlot, diamondStack(t, 2))
	actor.UseOn(ceiling.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, ceiling.EntityID); got.ItemID != diamondStack(t, 1).ItemID {
		t.Fatal("the frame's slot stayed empty after the use")
	}
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.add_item", 6.5, 65.96875, 5.5)
	actor.Attack(ceiling.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, ceiling.EntityID); !got.Empty() {
		t.Fatalf("the blow left the frame holding %+v, want nothing", got)
	}
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.remove_item", 6.5, 65.96875, 5.5)
	if drop := watcher.AwaitSpawn(protocol.EntityTypeItem); drop.Y != 65.96875 {
		t.Errorf("the ceiling frame's drop feet at y %v, want the box centre 65.96875", drop.Y)
	}
}

// TestAFramesDropChanceOfZeroWithholdsItsItem reloads a frame whose save
// carries ItemDropChance 0: the pop lays nothing, and the next save still
// carries the zero. vanilla: ItemFrame.readAdditionalSaveData,
// ItemFrame.addAdditionalSaveData, ItemFrame.dropItem
func TestAFramesDropChanceOfZeroWithholdsItsItem(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	actor, watcher, actorEntityID := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	srv.GiveSlot("Actor", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	srv.GiveSlot("Actor", player.HotbarSlot, diamondStack(t, 1))
	actor.UseOn(hung.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); got.ItemID != diamondStack(t, 1).ItemID {
		t.Fatal("the frame's slot stayed empty after the use")
	}
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.add_item", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)

	// The use's equipment broadcast trails its sound; the actor's settle
	// puts it on the watcher's wire before the watcher's own.
	actor.Settle()
	watcher.Settle()
	actor.Conn.Close()
	watcher.ExpectPlayerRemoved(actorEntityID, "Actor")
	srv.FlushMobs()
	srv.Shutdown()

	// The save edit a map-art frame's zero drop chance would have come with.
	editFrameEntity(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:item_frame", func(compound nbt.Compound) {
		compound["ItemDropChance"] = nbt.Float(0)
	})

	restarted := *cfg
	againSrv, reconnect := servertest.Start(t, &restarted)
	again := reconnect()
	again.Login("Keeper")
	again.Configure()
	shown := again.JoinCapturing("Keeper", map[int32]string{
		protocol.EntityTypeItemFrame: "frame",
	})
	back := servertest.CapturedOr(t, again, shown, "frame", protocol.EntityTypeItemFrame)
	if f := servertest.MetaOf(back.Meta, protocol.MetadataIndexFrameItem); f == nil || f.SlotVal.ItemID == 0 {
		t.Fatalf("the restored frame holds nothing, want the diamond it was saved with")
	}

	// The pop empties the frame but lays nothing with the chance at zero.
	again.MoveToGround(4.5, 64, 3.5, true)
	again.Settle()
	again.Attack(back.EntityID)
	if servertest.ASpawnOfWithinTicks(again, againSrv, 40, protocol.EntityTypeItem) {
		t.Error("a drop chance of zero still laid the held item")
	}
	if n := againSrv.FrameCount(); n != 1 {
		t.Errorf("the server holds %d frames after the pop, want the emptied one", n)
	}

	againSrv.FlushMobs()
	againSrv.Shutdown()

	// The zero survives the round trip, rather than being written back as
	// the default.
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:item_frame", func(compound nbt.Compound) {
		if v, ok := nbt.Get[nbt.Float](compound, "ItemDropChance"); !ok || v != 0 {
			t.Errorf("ItemDropChance = %v, %v; want the saved 0", v, ok)
		}
	})
}

// TestAComparatorSeesAnEmptyFrameBehindAConductor hangs an empty frame on
// the far side of a conductor the comparator reads through, with the
// conductor strongly powered short of fifteen: the frame is found and worth
// nothing, overriding the conductor's own power, where a cell with no
// frame would pass that power on.
// vanilla: ComparatorBlock.getInputSignal, ItemFrame.getAnalogOutput
func TestAComparatorSeesAnEmptyFrameBehindAConductor(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 0)
	servertest.Setblock(t, c, 4, 64, 0, "minecraft:stone")
	servertest.Setblock(t, c, 4, 64, 1, "minecraft:stone")
	// Two dust fed by a redstone block: the near one carries fourteen, so
	// the conductor is strongly powered short of the fifteen that would
	// pass through regardless.
	servertest.Setblock(t, c, 4, 65, 2, "minecraft:redstone_block")
	servertest.Setblock(t, c, 4, 65, 1, "minecraft:redstone_wire")
	servertest.Setblock(t, c, 4, 65, 0, "minecraft:redstone_wire")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 4, 65, 0, "minecraft:redstone_wire", map[string]string{"power": "14"})

	// With no frame hung the conductor's fourteen passes on; then the empty
	// frame hangs on its far side, where the comparator's read must find it
	// and take nothing rather than the conductor's fourteen.
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "14")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	c.MoveToGround(2.5, 64, 0.5, true)
	c.FacePlayer(4.5, 64, 0.5, -90, 0)
	c.Settle()
	c.PlaceBlock(4, 64, 0, protocol.FaceWest)
	hung := c.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, 3.96875, 64.5, 0.5, 90, 0, protocol.FaceWest)
	c.ExpectBlockAck()
	blinkFrameTestSource(t, c)
	servertest.AwaitTicks(t, srv, 10)
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "0")

	// A diamond reads its rotation plus one, so the frame is the thing
	// read, not the conductor; emptying it again reads nothing.
	c.SetCreativeSlot(player.HotbarSlot, diamondStack(t, 2))
	c.UseOn(hung.EntityID)
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "1")
	c.Attack(hung.EntityID)
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "0")
}

// TestAComparatorDoesNotReadAFrameInTheCellBeyond hangs a frame on the far
// side of the stone behind the conductor, one cell past the one the
// comparator reads: the frame is indexed under its support too, so only the
// cell itself may match. The conductor's fourteen must pass on.
// vanilla: ComparatorBlock.getItemFrame
func TestAComparatorDoesNotReadAFrameInTheCellBeyond(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 0)
	servertest.Setblock(t, c, 4, 64, 0, "minecraft:stone")
	servertest.Setblock(t, c, 3, 64, 0, "minecraft:stone")
	servertest.Setblock(t, c, 4, 64, 1, "minecraft:stone")
	servertest.Setblock(t, c, 4, 65, 2, "minecraft:redstone_block")
	servertest.Setblock(t, c, 4, 65, 1, "minecraft:redstone_wire")
	servertest.Setblock(t, c, 4, 65, 0, "minecraft:redstone_wire")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 4, 65, 0, "minecraft:redstone_wire", map[string]string{"power": "14"})
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "14")

	// The frame hangs on the west face of the stone at (3, 64, 0), in the
	// cell (2, 64, 0): its support cell is the one the comparator reads.
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	c.MoveToGround(1.5, 64, 0.5, true)
	c.FacePlayer(3.5, 64, 0.5, -90, 0)
	c.Settle()
	c.PlaceBlock(3, 64, 0, protocol.FaceWest)
	c.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, 2.96875, 64.5, 0.5, 90, 0, protocol.FaceWest)
	c.ExpectBlockAck()

	blinkFrameTestSource(t, c)
	servertest.AwaitTicks(t, srv, 10)
	servertest.ExpectWirePower(t, srv, 6, 64, 0, "14")
}

// TestASkeletonsArrowPopsAFrameItemButCannotBreakTheFrameWithMobGriefingOff
// flies a mob's own shaft into a filled frame with mob_griefing off: the
// item still pops, the pop running ahead of vanilla's griefing check, while
// the break of the emptied frame is refused the way a mob's block griefing
// is and the shaft deflects without taking it. With the rule back on the
// same shaft breaks the frame.
// vanilla: ItemFrame.hurtServer, BlockAttachedEntity.hurtServer
func TestASkeletonsArrowPopsAFrameItemButCannotBreakTheFrameWithMobGriefingOff(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	srv.SetGameRule("mob_griefing", false)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	// The frame is filled: the pop is not the griefing check's business.
	actor.SetCreativeSlot(player.HotbarSlot, diamondStack(t, 2))
	actor.UseOn(hung.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); got.ItemID != diamondStack(t, 1).ItemID {
		t.Fatal("the frame's slot stayed empty after the use")
	}

	// The shaft's line must miss the shooters: the actor steps out of it
	// before the skeleton works.
	actor.MoveToGround(2.5, 64, 3.5, true)
	actor.Settle()

	skeleton := srv.SpawnMob("minecraft:skeleton", 0.5, 64, 0.5)
	if skeleton == 0 {
		t.Fatal("the skeleton never stood up")
	}

	// The first shaft pops the item, frame standing, with the rule off.
	if id := srv.ShootMobArrowFrom(skeleton, 4.5, 64.5, 1.5, 0, 0, 10); id == 0 {
		t.Fatal("the skeleton's arrow never flew")
	}
	if dropped := servertest.ExpectDroppedStack(t, watcher); dropped.ItemID != diamondStack(t, 1).ItemID {
		t.Errorf("the skeleton's arrow with mob_griefing off dropped %+v, want the frame's item popped", dropped)
	}
	if n := srv.FrameCount(); n != 1 {
		t.Errorf("the skeleton's arrow broke the filled frame with mob_griefing off: %d frames left", n)
	}

	// The emptied frame refuses the break with the rule still off.
	if id := srv.ShootMobArrowFrom(skeleton, 4.5, 64.5, 1.5, 0, 0, 10); id == 0 {
		t.Fatal("the skeleton's arrow never flew")
	}
	servertest.AwaitTicks(t, srv, 20)
	if n := srv.FrameCount(); n != 1 {
		t.Errorf("the skeleton's arrow broke the empty frame with mob_griefing off: %d frames left", n)
	}

	// The rule back on: the same shaft breaks the frame.
	srv.SetGameRule("mob_griefing", true)
	if id := srv.ShootMobArrowFrom(skeleton, 4.5, 64.5, 1.5, 0, 0, 10); id == 0 {
		t.Fatal("the skeleton's arrow never flew")
	}
	servertest.AwaitTicks(t, srv, 20)
	if n := srv.FrameCount(); n != 0 {
		t.Errorf("the skeleton's arrow left the frame with mob_griefing on: %d frames left", n)
	}
}

// TestTwoSkeletonArrowsInOneTickCannotBreakAFrameWithMobGriefingOff flies
// two shafts of one mob into the same filled frame on the same tick with
// mob_griefing off. The first pops the item; the second finds the frame
// empty and has its break refused, so the frame stands with only the one
// item laid. The frame's state has to be read at the hurt, not from a
// tick-start snapshot of the filled frame, or the second would break the
// emptied one.
// vanilla: ItemFrame.hurtServer, BlockAttachedEntity.hurtServer
func TestTwoSkeletonArrowsInOneTickCannotBreakAFrameWithMobGriefingOff(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	srv.SetGameRule("mob_griefing", false)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	watcher.ExpectPaintingSound(t, "minecraft:entity.item_frame.place", servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	actor.SetCreativeSlot(player.HotbarSlot, diamondStack(t, 2))
	actor.UseOn(hung.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); got.ItemID != diamondStack(t, 1).ItemID {
		t.Fatal("the frame's slot stayed empty after the use")
	}

	// The shaft's line must miss the shooters: the actor steps out of it
	// before the skeleton works.
	actor.MoveToGround(2.5, 64, 3.5, true)
	actor.Settle()

	skeleton := srv.SpawnMob("minecraft:skeleton", 0.5, 64, 0.5)
	if skeleton == 0 {
		t.Fatal("the skeleton never stood up")
	}

	// Two identical shafts laid in the list before the next tick, so the
	// same tick sweeps both into the frame's box.
	if id := srv.ShootMobArrowFrom(skeleton, 4.5, 64.5, 1.5, 0, 0, 10); id == 0 {
		t.Fatal("the first arrow never flew")
	}
	if id := srv.ShootMobArrowFrom(skeleton, 4.5, 64.5, 1.5, 0, 0, 10); id == 0 {
		t.Fatal("the second arrow never flew")
	}

	// Only the popped diamond falls; the frame stands. A break would lay
	// the frame item too and leave no frame.
	if dropped := servertest.ExpectDroppedStack(t, watcher); dropped.ItemID != diamondStack(t, 1).ItemID {
		t.Errorf("the shots dropped %+v, want the frame's diamond popped", dropped)
	}
	servertest.AwaitTicks(t, srv, 20)
	if n := srv.FrameCount(); n != 1 {
		t.Errorf("two mob arrows in one tick with mob_griefing off left %d frames, want the emptied one", n)
	}
}

// placeFrameWithData places an item frame item carrying entity_data of the
// type given and returns the placing wall's server and the watcher's flags
// byte of the hung frame (0 when the frame's metadata carries none).
func placeFrameWithData(t *testing.T, typ int32, data nbt.Compound) (*server.Server, uint8) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"),
		Count: 1, Raw: []protocol.RawComponent{entityDataComponent(t, typ, data)}})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)

	var frameID int32 = -1
	for packetID, payload := range watcher.Packets(servertest.ExpectTimeout) {
		r := protocol.NewReader(payload)
		switch packetID {
		case protocol.IDPlaySpawnEntity:
			id := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeItemFrame {
				frameID = id
			}
		case protocol.IDPlayEntityMetadata:
			if r.ReadVarInt() != frameID {
				continue
			}
			fields, ok := servertest.ReadMetadata(r)
			if !ok {
				t.Fatal("the frame's metadata carries a field the reader does not know")
			}
			for _, f := range fields {
				if f.Index == 0 {
					return srv, f.ByteVal
				}
			}
			return srv, 0
		}
	}
	t.Fatal("no metadata for the hung frame arrived")
	return srv, 0
}

// TestAPlacedFrameCarriesItsItemsEntityData places a frame item whose
// entity_data names Invisible: the frame is shown with the invisible flag.
func TestAPlacedFrameCarriesItsItemsEntityData(t *testing.T) {
	t.Parallel()
	_, flags := placeFrameWithData(t, protocol.EntityTypeItemFrame, nbt.Compound{"Invisible": nbt.Byte(1)})
	if flags&protocol.EntityFlagInvisible == 0 {
		t.Errorf("entity flags = %#x, want the invisible bit the item's entity_data names", flags)
	}
}

// TestAPlacedFrameIgnoresAnotherTypesEntityData names another type in the
// component: the frame hangs plain.
func TestAPlacedFrameIgnoresAnotherTypesEntityData(t *testing.T) {
	t.Parallel()
	_, flags := placeFrameWithData(t, protocol.EntityTypeArmorStand, nbt.Compound{"Invisible": nbt.Byte(1)})
	if flags&protocol.EntityFlagInvisible != 0 {
		t.Errorf("entity flags = %#x, want another type's entity_data ignored", flags)
	}
}

// TestAFixedFrameFromEntityDataHangsWithoutSupport places a Fixed frame and
// removes its support: it stays past the survives poll.
func TestAFixedFrameFromEntityDataHangsWithoutSupport(t *testing.T) {
	t.Parallel()
	srv, _ := placeFrameWithData(t, protocol.EntityTypeItemFrame, nbt.Compound{"Fixed": nbt.Byte(1)})
	if !srv.SetBlock(4, 64, 5, 0) {
		t.Fatal("the wall's chunk is not loaded")
	}
	servertest.RunTicks(t, srv, 110)
	if n := srv.FrameCount(); n != 1 {
		t.Errorf("the server holds %d frames, want the fixed one still hanging", n)
	}
}

// TestHangingEntitiesAndArmorStandsObeySpawnProtection punches a painting, a
// frame, a lead knot and an armour stand inside the spawn radius: a non-op's
// blows are ignored, an op's remove all four.
// vanilla: BlockAttachedEntity.skipAttackInteraction, ArmorStand.skipAttackInteraction
func TestHangingEntitiesAndArmorStandsObeySpawnProtection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		ops  string
		left int
	}{
		{"non-op", "Boss", 1},
		{"op", "Actor", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, tc.ops), servertest.SpawnProtectionRadius(config.Default().SpawnProtection))
			c.MoveToGround(5.5, 64, 2.5, true)
			c.Settle()
			srv.StopTick()
			ids := []int32{
				srv.SpawnPaintingEntity(4, 64, 4, 0).ID,
				srv.SpawnFrameEntity(5, 64, 4).ID,
				srv.SpawnKnotEntity(6, 64, 4).ID,
				srv.SpawnArmorStand(3.5, 64, 3.5),
			}
			for _, id := range ids {
				c.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) {
					b.PutVarInt(id)
				})
			}
			c.Settle()
			for name, got := range map[string]int{
				"paintings": srv.PaintingCount(), "frames": srv.FrameCount(),
				"knots": srv.KnotCount(), "armour stands": srv.ArmorStandCount(),
			} {
				if got != tc.left {
					t.Errorf("%s left = %d, want %d", name, got, tc.left)
				}
			}
		})
	}
}
