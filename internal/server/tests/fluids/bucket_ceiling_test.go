package fluids

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestBucketPourAtDimensionCeiling: a lava bucket poured onto a top face at
// the Nether's build limit stays full and uses nothing; one level lower it
// empties into a source.
// vanilla: BucketItem.emptyContents, Level.setBlock
func TestBucketPourAtDimensionCeiling(t *testing.T) {
	t.Parallel()
	cfg := servertest.CrossingTestConfig(t)
	cfg.GameMode = "survival"
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Pourer")
	servertest.HoldTicks(t, srv)
	// Only the target chunk is loaded in the nether: the sender is held and
	// the chunk is pinned instead of the arrival's whole view.
	release := srv.HoldChunkSenders()
	t.Cleanup(release)
	defer srv.HoldChunkInDimension(world.NetherName, 6, 0)()
	servertest.CarryIntoNether(t, srv, c, "Pourer", 6, 0)

	lava := registry.MustState("minecraft:lava", map[string]string{"level": "0"})
	lavaBucket := servertest.ItemIDOrDie(t, "minecraft:lava_bucket")
	stone := registry.MustState("minecraft:stone", nil)
	air := registry.MustState("minecraft:air", nil)

	for _, tc := range []struct {
		name       string
		stoneY     int32
		wantSource bool
	}{
		{"ceiling", 255, false},
		{"below", 254, true},
	} {
		srv.SetBlockInDimension(world.NetherName, 6, tc.stoneY, 0, stone)
		srv.SetBlockInDimension(world.NetherName, 6, tc.stoneY+1, 0, air)
		srv.SetBlockInDimension(world.NetherName, 6, tc.stoneY+2, 0, air)
		srv.GiveSlot("Pourer", player.HotbarSlot, protocol.ItemStack{ItemID: lavaBucket, Count: 1})
		before := srv.StatValue("Pourer", registry.StatUsed, "minecraft:lava_bucket")

		y := float64(tc.stoneY + 1)
		c.FacePlayer(6.5, y, 0.5, 0, 90)
		servertest.UseHeld(c, 0, 90)
		c.Settle()

		got := srv.BlockInDimension(world.NetherName, 6, tc.stoneY+1, 0) == lava
		if got != tc.wantSource {
			t.Errorf("%s: source above the stone = %v, want %v", tc.name, got, tc.wantSource)
		}
		held := srv.InventorySlot("Pourer", player.HotbarSlot).ItemID
		wantHeld, wantUse := lavaBucket, int32(0)
		if tc.wantSource {
			wantHeld, wantUse = int32(registry.ItemBucket), 1
		}
		if held != wantHeld {
			t.Errorf("%s: held item = %d, want %d", tc.name, held, wantHeld)
		}
		if use := srv.StatValue("Pourer", registry.StatUsed, "minecraft:lava_bucket") - before; use != wantUse {
			t.Errorf("%s: lava bucket uses = %d, want %d", tc.name, use, wantUse)
		}
	}
	c.Disconnect()
}
