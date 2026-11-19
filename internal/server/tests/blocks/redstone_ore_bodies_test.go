package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestRedstoneOreLightsUnderOrbTNTAndFallingBlock: an experience orb, a
// primed TNT and a falling block each light the unlit redstone ore they rest on.
// vanilla: Entity.applyEffectsFromBlocks, RedStoneOreBlock.stepOn
func TestRedstoneOreLightsUnderOrbTNTAndFallingBlock(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Miner")
	c.MoveToGround(12.5, 64, 0.5, true)
	c.SettleKeeping()
	servertest.HoldTicks(t, srv)
	ore := servertest.MustState(t, "minecraft:redstone_ore", map[string]string{"lit": "false"})
	for _, x := range []int32{2, 4, 6} {
		if !srv.SetBlock(x, 63, 0, ore) {
			t.Fatal("the ore's chunk was not loaded")
		}
	}
	srv.SpawnOrb(2.5, 64, 0.5, 3)
	srv.SpawnTNT(4.5, 64, 0.5, 200)
	srv.SpawnFallingBlock(6, 66, 0, servertest.BlockState(t, "minecraft:sand"))
	servertest.RunTicks(t, srv, 40)
	for _, x := range []int32{2, 4, 6} {
		if _, props := servertest.StatePropsOf(t, srv, x, 63, 0); props["lit"] != "true" {
			t.Errorf("the ore at x=%d is not lit", x)
		}
	}
}
