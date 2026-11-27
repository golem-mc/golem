package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// A connecting block beside a cell a sticky piston pulls the block out of
// drops that side in the tick of the pull.
func TestAConnectingBlockLosesTheSideAPistonPullsAway(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, block, off, on string }{
		{"paneBehind", "glass_pane", "east=false,north=false,south=false,waterlogged=false,west=false", "east=false,north=false,south=false,waterlogged=false,west=true"},
		{"pane", "glass_pane", "east=false,north=false,south=false,waterlogged=false,west=false", "east=false,north=true,south=false,waterlogged=false,west=false"},
		{"bars", "iron_bars", "east=false,north=false,south=false,waterlogged=false,west=false", "east=false,north=true,south=false,waterlogged=false,west=false"},
		{"fence", "oak_fence", "east=false,north=false,south=false,waterlogged=false,west=false", "east=false,north=true,south=false,waterlogged=false,west=false"},
		{"wall", "cobblestone_wall", "east=none,north=none,south=none,up=true,waterlogged=false,west=none", "east=none,north=low,south=none,up=true,waterlogged=false,west=none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Actor"))
			actor := connect().Join("Actor")
			servertest.HoldTicks(t, srv)
			standExtendedPiston(t, srv, true)
			servertest.Setblock(t, actor, 4, 64, 0, "stone")
			pz, px := int32(1), int32(4)
			if tc.name == "paneBehind" {
				pz, px = 0, 5
			}
			servertest.Setblock(t, actor, px, 64, pz, "minecraft:"+tc.block)
			on := servertest.BlockStateWith(t, "minecraft:"+tc.block, tc.on)
			if got := srv.BlockAt(px, 64, pz); got != on {
				t.Fatalf("setup: %s = %d, want connected %d", tc.block, got, on)
			}
			servertest.Setblock(t, actor, 2, 64, -1, "redstone_block")
			servertest.RunTicks(t, srv, 1)
			servertest.Setblock(t, actor, 2, 64, -1, "air")
			off := servertest.BlockStateWith(t, "minecraft:"+tc.block, tc.off)
			// The side drops in the tick the pull turns the pulled block's
			// cell into its ride, not a tick later.
			servertest.UntilTicks(t, srv, 6, "the pull's ride appears", func() bool {
				name, _ := servertest.StateProps(srv.BlockAt(3, 64, 0))
				return name == "minecraft:moving_piston"
			})
			if got := srv.BlockAt(px, 64, pz); got != off {
				t.Errorf("%s = %d when the pull began, want the side dropped %d", tc.block, got, off)
			}
		})
	}
}
