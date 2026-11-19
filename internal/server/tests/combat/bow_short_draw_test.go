package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestABowDrawnTwoElapsedTicksFiresNothing: two elapsed ticks give a power of
// 0.073, under the 0.1 a shot needs.
func TestABowDrawnTwoElapsedTicksFiresNothing(t *testing.T) {
	t.Parallel()
	cfg := servertest.BowTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Archer")
	srv.GiveSlot("Archer", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:bow"), Count: 1})
	srv.GiveSlot("Archer", player.MainSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:arrow"), Count: 8})

	c.MoveTo(0.5, 64, 0.5, -90, 0)
	c.Settle()
	srv.StopTick()
	servertest.DrawBow(c, -90, 0)
	c.Settle()
	for i := 0; srv.BowTicks("Archer") < 3; i++ {
		if i == 8 {
			t.Fatalf("eight ticks drew the bow %d ticks, want 3", srv.BowTicks("Archer"))
		}
		srv.TickSurvival()
	}
	servertest.ReleaseBow(c)
	c.SettleKeeping()
	servertest.HoldTicks(t, srv)

	c.ExpectNoneWithinTicks(srv, 0, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		r.ReadUUID()
		return r.ReadVarInt() == protocol.EntityTypeArrow
	})
	if got := srv.InventorySlot("Archer", player.MainSlot).Count; got != 8 {
		t.Errorf("the arrow stack holds %d, want 8 left by the refused shot", got)
	}
}
