package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// framedMapFarX is where the actor stands east of the standard wall's frame
// (box x 4..5 holding a map, 4.125..4.875 otherwise), eye at 65.62 against
// the box top at 65 or 64.875: 7.92 blocks from the map's box, 8.06 from
// the narrow one, either side of a creative hand's reach of 5 plus the
// server's 3 of slack.
const framedMapFarX = 12.9

// frameMetaOf reads the next metadata for the frame and answers the item
// and the rotation it carries.
func frameMetaOf(t *testing.T, c *servertest.Client, entityID int32) (protocol.ItemStack, int32) {
	t.Helper()
	meta := c.AwaitMetadataFor(entityID)
	item, rotation := servertest.MetaOf(meta, uint8(protocol.MetadataIndexFrameItem)), servertest.MetaOf(meta, uint8(protocol.MetadataIndexFrameRotation))
	if item == nil || rotation == nil {
		t.Fatalf("the frame's metadata %+v lacks the item or the rotation", meta)
	}
	return item.SlotVal, rotation.VarintVal
}

// TestAFramedMapWidensTheFramesBox fills a frame with a filled map, then
// with stone, and uses and hits it from a spot inside reach of the
// full-block box a map gives the frame but out of reach of the 3/4 one:
// the map's frame turns and pops, the stone's refuses both, which the
// first change heard after a use or a hit from close by shows.
// vanilla: ItemFrame.createBoundingBox, ItemFrame.onItemChanged
func TestAFramedMapWidensTheFramesBox(t *testing.T) {
	t.Parallel()
	filledMap := func(t *testing.T) protocol.ItemStack {
		stack := servertest.ItemStack(t, "minecraft:filled_map", 1)
		stack.SetMapID(0)
		return stack
	}
	stone := func(t *testing.T) protocol.ItemStack { return servertest.ItemStack(t, "minecraft:stone", 1) }
	for _, tc := range []struct {
		name    string
		item    func(*testing.T) protocol.ItemStack
		reaches bool
	}{
		{"a filled map", filledMap, true},
		{"stone", stone, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.BlockTestConfig())
			actor, watcher, _ := servertest.PaintingWatchers(t, connect)
			servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
			actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
			actor.MoveToGround(4.5, 64, 3.5, true)
			actor.Settle()
			actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
			hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
			actor.ExpectBlockAck()

			held := tc.item(t)
			actor.SetCreativeSlot(player.HotbarSlot, held)
			actor.UseOn(hung.EntityID)
			if item, _ := frameMetaOf(t, watcher, hung.EntityID); item.ItemID != held.ItemID {
				t.Fatalf("the frame holds %+v after the use, want %+v", item, held)
			}
			near := func() {
				actor.MoveToGround(4.5, 64, 3.5, true)
				actor.Settle()
			}
			far := func() {
				actor.MoveToGround(framedMapFarX, 64, servertest.WallFrameZ, true)
				actor.Settle()
			}

			if tc.reaches {
				far()
				actor.UseOn(hung.EntityID)
				if item, rotation := frameMetaOf(t, watcher, hung.EntityID); item.ItemID != held.ItemID || rotation != 1 {
					t.Fatalf("the far use left %+v at rotation %d, want the map turned once", item, rotation)
				}
				actor.Attack(hung.EntityID)
				if item, _ := frameMetaOf(t, watcher, hung.EntityID); !item.Empty() {
					t.Fatalf("the far blow left the frame holding %+v, want the map popped", item)
				}
				return
			}

			// The far use refused, the near blow's pop is the first change.
			far()
			actor.UseOn(hung.EntityID)
			near()
			actor.Attack(hung.EntityID)
			if item, rotation := frameMetaOf(t, watcher, hung.EntityID); !item.Empty() || rotation != 0 {
				t.Fatalf("first change after the far use and the near blow: %+v at rotation %d, want the unturned stone popped", item, rotation)
			}
			actor.UseOn(hung.EntityID)
			if item, _ := frameMetaOf(t, watcher, hung.EntityID); item.ItemID != held.ItemID {
				t.Fatalf("the frame holds %+v after the refill, want %+v", item, held)
			}

			// The far blow refused, the near use's turn is the first change.
			far()
			actor.Attack(hung.EntityID)
			near()
			actor.UseOn(hung.EntityID)
			if item, rotation := frameMetaOf(t, watcher, hung.EntityID); item.ItemID != held.ItemID || rotation != 1 {
				t.Fatalf("first change after the far blow and the near use: %+v at rotation %d, want the stone turned once", item, rotation)
			}
		})
	}
}
