package servertest

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func TestFrameReadersAcceptUseSwings(t *testing.T) {
	t.Parallel()
	const entityID = 42
	item := protocol.ItemStack{ItemID: int32(registry.ItemDiamond), Count: 1}
	var spawn, metadata, swing protocol.Buffer
	spawn.PutVarInt(entityID)
	spawn.PutUUID(protocol.OfflineUUID("Frame"))
	spawn.PutVarInt(protocol.EntityTypeItemFrame)
	spawn.PutF64(WallFrameX)
	spawn.PutF64(WallFrameY)
	spawn.PutF64(WallFrameZ)
	spawn.PutLpVec3(0, 0, 0)
	spawn.PutAngle(0)
	spawn.PutAngle(180)
	spawn.PutAngle(0)
	spawn.PutVarInt(protocol.FaceNorth)
	metadata.PutVarInt(entityID)
	metadata.PutMetadata(protocol.MetadataIndexHangingDirection, protocol.MetadataDirection)
	metadata.PutVarInt(protocol.FaceNorth)
	metadata.PutMetadata(protocol.MetadataIndexFrameItem, protocol.MetadataSlot)
	metadata.PutSlot(item)
	metadata.PutMetadata(protocol.MetadataIndexFrameRotation, protocol.MetadataVarInt)
	metadata.PutVarInt(3)
	metadata.EndMetadata()
	swing.PutVarInt(7)
	swing.PutVarInt(protocol.HandMain)
	swing.PutVarInt(registry.DefaultSwingAnimation.Type)
	swing.PutVarInt(registry.DefaultSwingAnimation.Duration)
	spawnPacket := PushedPacket{Id: protocol.IDPlaySpawnEntity, Data: spawn.Bytes()}
	metadataPacket := PushedPacket{Id: protocol.IDPlayEntityMetadata, Data: metadata.Bytes()}
	swingPacket := PushedPacket{Id: protocol.IDPlaySwingAnimation, Data: swing.Bytes()}

	for _, tc := range []struct {
		name    string
		packets []PushedPacket
		read    func(*testing.T, *Client)
	}{
		{"spawn", []PushedPacket{swingPacket, spawnPacket, swingPacket, metadataPacket}, func(t *testing.T, c *Client) {
			got := c.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, WallFrameX, WallFrameY, WallFrameZ, 180, 0, protocol.FaceNorth)
			if got.EntityID != entityID || got.Item.ItemID != item.ItemID || got.Item.Count != 1 || got.rotation != 3 {
				t.Fatalf("frame = %+v, want entity %d holding one diamond at rotation 3", got, entityID)
			}
		}},
		{"metadata", []PushedPacket{swingPacket, metadataPacket}, func(t *testing.T, c *Client) {
			got, rotation := ExpectFrameMetadata(t, c, entityID, protocol.FaceNorth)
			if got.ItemID != item.ItemID || got.Count != 1 || rotation != 3 {
				t.Fatalf("item = %+v, rotation = %d, want one diamond at rotation 3", got, rotation)
			}
		}},
		{"item", []PushedPacket{swingPacket, metadataPacket}, func(t *testing.T, c *Client) {
			if got := FrameItemSlot(t, c, entityID); got.ItemID != item.ItemID || got.Count != 1 {
				t.Fatalf("item = %+v, want one diamond", got)
			}
		}},
		{"rotation", []PushedPacket{swingPacket, metadataPacket}, func(t *testing.T, c *Client) {
			if got := FrameMetadataField(t, c, entityID, protocol.MetadataIndexFrameRotation).VarintVal; got != 3 {
				t.Fatalf("rotation = %d, want 3", got)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &Client{T: t, Pushback: tc.packets}
			tc.read(t, c)
			if len(c.Pushback) != 0 {
				t.Fatalf("%d packets left after the frame read", len(c.Pushback))
			}
		})
	}
}
