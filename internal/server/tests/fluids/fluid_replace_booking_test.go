package fluids

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

const replaceY = 64

func fluidAt(t *testing.T, x, z int32, srv interface{ BlockAt(x, y, z int32) uint16 }) string {
	t.Helper()
	name, _ := servertest.StateProps(srv.BlockAt(x, replaceY, z))
	return name
}

// TestWaterBesideDiagonalLavaFlowsAtItsOwnDelay places lava and water on a
// diagonal: the water's own visit, five ticks out, floods both corner cells
// and turns the lava to obsidian.
// vanilla: WaterFluid.getTickDelay, LiquidBlock.shouldSpreadLiquid
func TestWaterBesideDiagonalLavaFlowsAtItsOwnDelay(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	servertest.Setblock(t, c, 5, replaceY, 5, "lava")
	// A lava visit pending at the water's position, replaced at once.
	servertest.Setblock(t, c, 4, replaceY, 4, "lava")
	servertest.Setblock(t, c, 4, replaceY, 4, "water")
	servertest.RunTicks(t, srv, 4)
	for _, p := range [][2]int32{{5, 4}, {4, 5}} {
		if got := fluidAt(t, p[0], p[1], srv); got != "minecraft:air" {
			t.Fatalf("corner %v after 4 ticks = %s, want air", p, got)
		}
	}
	servertest.RunTicks(t, srv, 1)
	for _, p := range [][2]int32{{5, 4}, {4, 5}} {
		if got := fluidAt(t, p[0], p[1], srv); got != "minecraft:water" {
			t.Errorf("corner %v after 5 ticks = %s, want water", p, got)
		}
	}
	if got := fluidAt(t, 5, 5, srv); got != "minecraft:obsidian" {
		t.Errorf("lava after 5 ticks = %s, want obsidian", got)
	}
}

// TestFreshWaterIgnoresTheVisitOfTheLavaItReplaced puts lava in a walled cell,
// replaces it with water, then clears the cell, opens a side and places fresh
// water at tick 27: the lava's visit due at tick 30 must neither spread the
// water early nor spend its booking, so the side fills at tick 32.
// vanilla: LiquidBlock.onPlace, ScheduledTick.UNIQUE_TICK_HASH, ServerLevel.tickFluid
func TestFreshWaterIgnoresTheVisitOfTheLavaItReplaced(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Builder", servertest.OpsListing(t, "Builder"))
	servertest.HoldTicks(t, srv)
	for _, p := range [][2]int32{{3, 4}, {4, 3}, {4, 5}, {5, 4}} {
		servertest.Setblock(t, c, p[0], replaceY, p[1], "stone")
	}
	servertest.Setblock(t, c, 4, replaceY, 4, "lava") // tick 0
	servertest.RunTicks(t, srv, 1)
	servertest.Setblock(t, c, 4, replaceY, 4, "water") // tick 1
	servertest.RunTicks(t, srv, 18)
	servertest.Setblock(t, c, 4, replaceY, 4, "air")
	servertest.Setblock(t, c, 5, replaceY, 4, "air")
	servertest.RunTicks(t, srv, 8)
	servertest.Setblock(t, c, 4, replaceY, 4, "water") // tick 27
	servertest.RunTicks(t, srv, 3)                     // tick 30: the lava's visit runs
	if got := fluidAt(t, 5, 4, srv); got != "minecraft:air" {
		t.Fatalf("open side at tick 30 = %s, want air", got)
	}
	servertest.RunTicks(t, srv, 2)
	if got := fluidAt(t, 5, 4, srv); got != "minecraft:water" {
		t.Errorf("open side at tick 32 = %s, want water", got)
	}
}
