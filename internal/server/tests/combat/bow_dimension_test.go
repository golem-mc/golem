package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestChangingDimensionWithADrawnBowFiresNothing crosses into the Nether
// with a bow fully drawn: the draw is dropped, no arrow is spent.
// vanilla: ServerPlayer.teleport, LivingEntity.stopUsingItem
func TestChangingDimensionWithADrawnBowFiresNothing(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Archer")
	srv.GiveSlot("Archer", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:bow"), Count: 1})
	srv.GiveSlot("Archer", 37, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:arrow"), Count: 3})
	servertest.DrawBow(c, 0, 0)
	servertest.HoldDraw(t, srv, "Archer", 24)

	c.ChatCommand("execute in minecraft:the_nether run tp @s 0 70 0")
	servertest.WaitFor(t, srv, 1200, func() bool {
		return srv.PlayerDimension("Archer") == "minecraft:the_nether"
	})
	since := srv.TicksRun()
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.TicksRun() >= since+5 })

	if got := srv.BowTicks("Archer"); got != 0 {
		t.Errorf("bow draw after the crossing = %d ticks, want 0", got)
	}
	if got := srv.InventorySlot("Archer", 37); got.Count != 3 {
		t.Errorf("arrows after the crossing = %d, want 3", got.Count)
	}
}
