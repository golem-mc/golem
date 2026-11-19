package gamemode

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// TestCreativeSlotIgnoredOutsideCreative checks that a survival client cannot
// conjure items with the creative inventory packet, which the vanilla server
// only honours from a creative player.
func TestCreativeSlotIgnoredOutsideCreative(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Cheater")

	diamond, _ := registry.ItemID("minecraft:diamond")
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: diamond, Count: 64})
	c.Settle()

	if got := srv.InventorySlot("Cheater", player.HotbarSlot); !got.Empty() {
		t.Errorf("survival player's hotbar after a creative slot = %+v, want empty", got)
	}
}
