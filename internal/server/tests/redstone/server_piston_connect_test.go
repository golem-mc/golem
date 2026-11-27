package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// A connecting block beside a cell a sticky piston pulls the block out of
// drops that side in the tick of the pull.
func TestAConnectingBlockLosesTheSideAPistonPullsAway(t *testing.T) {
	t.Parallel()
	type pos = [3]int32
	// facing east: the pulled block at (4,64,0) rides into the head cell (3,64,0).
	// facing down: the pulled block at (2,62,0) rides up into the head cell
	// (2,63,0); the world below y=64 is stone, so the pane's other sides stay
	// connected and only the named side is compared.
	for _, tc := range []struct {
		name, block, side, gone, pulled, facing string
		pane, at, ride                          pos
	}{
		{"paneBehind", "glass_pane", "west", "false", "stone", "east", pos{5, 64, 0}, pos{4, 64, 0}, pos{3, 64, 0}},
		{"pane", "glass_pane", "north", "false", "stone", "east", pos{4, 64, 1}, pos{4, 64, 0}, pos{3, 64, 0}},
		{"bars", "iron_bars", "north", "false", "stone", "east", pos{4, 64, 1}, pos{4, 64, 0}, pos{3, 64, 0}},
		{"fence", "oak_fence", "north", "false", "stone", "east", pos{4, 64, 1}, pos{4, 64, 0}, pos{3, 64, 0}},
		{"wall", "cobblestone_wall", "north", "none", "stone", "east", pos{4, 64, 1}, pos{4, 64, 0}, pos{3, 64, 0}},
		{"paneBesideVerticalPull", "glass_pane", "south", "false", "target", "down", pos{2, 62, -1}, pos{2, 62, 0}, pos{2, 63, 0}},
		{"barsBesideVerticalPull", "iron_bars", "south", "false", "target", "down", pos{2, 62, -1}, pos{2, 62, 0}, pos{2, 63, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Actor"))
			actor := connect().Join("Actor")
			servertest.HoldTicks(t, srv)
			standExtendedPistonFacing(t, srv, true, tc.facing)
			servertest.Setblock(t, actor, tc.at[0], tc.at[1], tc.at[2], tc.pulled)
			servertest.Setblock(t, actor, tc.pane[0], tc.pane[1], tc.pane[2], "minecraft:"+tc.block)
			sideAt := func(p pos) string {
				_, props := servertest.StateProps(srv.BlockAt(p[0], p[1], p[2]))
				return props[tc.side]
			}
			if got := sideAt(tc.pane); got == tc.gone {
				t.Fatalf("setup: %s %s = %q, want connected", tc.block, tc.side, got)
			}
			servertest.Setblock(t, actor, 2, 64, -1, "redstone_block")
			servertest.RunTicks(t, srv, 1)
			servertest.Setblock(t, actor, 2, 64, -1, "air")
			// The side drops in the tick the pull turns the pulled block's
			// cell into its ride, not a tick later.
			servertest.UntilTicks(t, srv, 6, "the pull's ride appears", func() bool {
				name, _ := servertest.StateProps(srv.BlockAt(tc.ride[0], tc.ride[1], tc.ride[2]))
				return name == "minecraft:moving_piston"
			})
			if got := sideAt(tc.pane); got != tc.gone {
				t.Errorf("%s %s = %q when the pull began, want %q", tc.block, tc.side, got, tc.gone)
			}
		})
	}
}
