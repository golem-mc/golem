package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestASwimmingPlayerCanPlaceABlockAboveTheirBody(t *testing.T) {
	t.Parallel()
	for _, swimming := range []bool{false, true} {
		name := "standing"
		if swimming {
			name = "swimming"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, c, id := servertest.JoinAs(t, servertest.BlockTestConfig(), "Builder")
			servertest.HoldTicks(t, srv)
			water := registry.MustState("minecraft:water", nil)
			stone := registry.MustState("minecraft:stone", nil)
			for y := int32(64); y <= 66; y++ {
				srv.SetBlock(0, y, 0, water)
			}
			srv.SetBlock(1, 65, 0, stone)
			c.MoveToGround(0.5, 64, 0.5, false)
			if swimming {
				c.Send(protocol.IDPlayPlayerCommand, func(b *protocol.Buffer) {
					b.PutVarInt(id)
					b.PutVarInt(protocol.PlayerCommandStartSprinting)
					b.PutVarInt(0)
				})
				if got := c.ExpectMetadata(); got.Pose != protocol.PoseSwimming {
					t.Fatalf("pose = %d, want swimming", got.Pose)
				}
			}
			c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{
				ItemID: servertest.MustItemID(t, "minecraft:stone"), Count: 1,
			})
			c.UseBlockSeq(1, 65, 0, protocol.FaceWest, 1)
			c.Settle()
			want := water
			if swimming {
				want = stone
			}
			if got := srv.BlockAt(0, 65, 0); got != want {
				t.Errorf("block above %s player = %d, want %d", name, got, want)
			}
		})
	}
}
