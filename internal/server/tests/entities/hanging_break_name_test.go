package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ItemFrame.getPickResult, Painting.getPickResult
func TestPickingNamedHangingEntity(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"painting", "item_frame", "glow_item_frame"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			srv, c, id := placeNamedHangingEntity(t, kind, "creative")
			// Clear the original stack so a pick must supply a new one.
			srv.GiveSlot("Hanger", player.HotbarSlot, protocol.ItemStack{})
			c.Settle()
			got := servertest.PickedEntityStack(t, c, id)
			if got.ItemID != servertest.MustItem("minecraft:"+kind) || got.Count != 1 {
				t.Fatalf("picked %+v, want one %s", got, kind)
			}
			if kind == "painting" {
				if got.CustomName != nil {
					t.Errorf("painting pick name = %+v, want none", got.CustomName)
				}
				return
			}
			if got.CustomName == nil || got.CustomName.Text != "Bob" {
				t.Errorf("empty frame pick name = %+v, want Bob", got.CustomName)
			}
			srv.GiveSlot("Hanger", player.HotbarSlot, protocol.ItemStack{
				ItemID: int32(registry.ItemDiamond), Count: 1, CustomName: &protocol.Text{Text: "Gem"},
			})
			c.Settle()
			c.InteractAt(id, protocol.HandMain, 0.5, 0.5, 0.5)
			c.Settle()
			srv.GiveSlot("Hanger", player.HotbarSlot, protocol.ItemStack{})
			c.Settle()
			got = servertest.PickedEntityStack(t, c, id)
			if got.ItemID != int32(registry.ItemDiamond) || got.Count != 1 || got.CustomName == nil || got.CustomName.Text != "Gem" {
				t.Errorf("filled frame picked %+v, want one diamond named Gem", got)
			}
		})
	}
}

func placeNamedHangingEntity(t *testing.T, kind, mode string) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.GameMode = mode
	srv, c := servertest.Join(t, cfg, "Hanger")
	servertest.HoldTicks(t, srv)
	if !srv.SetBlock(4, 65, 5, paintingWallState) {
		t.Fatal("wall chunk is not loaded")
	}
	srv.GiveSlot("Hanger", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItem("minecraft:" + kind), Count: 1, CustomName: &protocol.Text{Text: "Bob"},
	})
	c.MoveToGround(4.5, 64, 3.5, true)
	c.Settle()
	c.PlaceBlock(4, 65, 5, protocol.FaceNorth)
	typ := int32(protocol.EntityTypeItemFrame)
	if kind == "painting" {
		typ = protocol.EntityTypePainting
	} else if kind == "glow_item_frame" {
		typ = protocol.EntityTypeGlowItemFrame
	}
	var id int32
	if !c.ReadTicks(srv, 5, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		spawnID := r.ReadVarInt()
		r.ReadUUID()
		if r.ReadVarInt() != typ {
			return false
		}
		id = spawnID
		return true
	}) {
		t.Fatalf("no %s spawned within 5 ticks", kind)
	}
	c.Settle()
	return srv, c, id
}
