package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestArmorStandPlacementUsesThePlayersDimensions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, scale string
		x, y        int32
		want        int
	}{
		{"above a small player", "0.5", 0, 65, 1},
		{"inside a standing player", "1", 0, 65, 0},
		{"beside a wide player", "2", 1, 64, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("attribute @s minecraft:scale base set " + tc.scale)
			servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
			// The stand starts a quarter-block inside its cell.
			c.MoveToGround(0.8, 64, 0.5, true)
			srv.SetBlock(tc.x+1, tc.y, 0, registry.MustState("minecraft:stone", nil))
			c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: armorStandItemID, Count: 1})
			c.UseBlockSeq(tc.x+1, tc.y, 0, protocol.FaceWest, 1)
			c.Settle()
			if got := srv.ArmorStandCount(); got != tc.want {
				t.Errorf("stands placed %s = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}
