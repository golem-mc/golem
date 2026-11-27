package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestPlacementIsRefusedIntoAMobAcrossAChunkBorder places stone into the
// cell east of x=15 while a cow whose position is in the next chunk
// (x=16.2) has its box reaching back into that cell, and holds that the
// placement is refused until the cow moves clear.
func TestPlacementIsRefusedIntoAMobAcrossAChunkBorder(t *testing.T) {
	t.Parallel()
	const name = "Placer"
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	srv.SetBlock(15, 63, 5, registry.MustState("minecraft:grass_block", map[string]string{"snowy": "false"}))
	srv.GiveSlot(name, player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemStone), Count: 2})
	c.MoveToGrounded(13.5, 64, 5.5, 0, 30, true)
	srv.SpawnMob("minecraft:cow", 16.2, 64, 5.5)

	c.PlaceBlock(15, 63, 5, protocol.FaceTop)
	c.Settle()
	srv.FlushBlockAcks()
	if srv.BlockAt(15, 64, 5) != 0 {
		t.Error("stone was placed into a cow standing across the chunk border")
	}
	if got := srv.InventorySlot(name, player.HotbarSlot).Count; got != 2 {
		t.Errorf("held count after the refusal = %d, want 2", got)
	}
}
