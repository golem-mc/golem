package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestATurtleEggHatchesIntoBabyTurtlesBeforeDawn: in the hatch window every
// random tick on a sand-backed egg raises its stage, and the tick at stage 2
// removes the block and stands one baby turtle per egg along the cell's edge.
// vanilla: TurtleEggBlock.randomTick
func TestATurtleEggHatchesIntoBabyTurtlesBeforeDawn(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, driedGhastSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), driedGhastTickets)
	srv.SetDayTime(21500)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:sand", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:turtle_egg", map[string]string{"eggs": "2", "hatch": "0"})

	// Single passes, so a stage is seen before the next die can land.
	next := func() uint16 {
		before := srv.BlockAt(5, 64, 0)
		for range 100 {
			srv.TickRandomTicks(1)
			if got := srv.BlockAt(5, 64, 0); got != before {
				return got
			}
		}
		t.Fatal("no die advanced the egg in 100 passes")
		return 0
	}
	for stage := 1; stage <= 2; stage++ {
		want := registry.MustState("minecraft:turtle_egg", map[string]string{"eggs": "2", "hatch": string(rune('0' + stage))})
		if got := next(); got != want {
			t.Fatalf("stage %d: the egg is state %d, want %d", stage, got, want)
		}
	}
	if got := next(); got != registry.MustState("minecraft:air", nil) {
		t.Fatalf("the hatched egg left state %d, want air", got)
	}
	var xs []float64
	for _, m := range srv.Mobs() {
		if m.Kind != "minecraft:turtle" || !m.Baby {
			t.Errorf("unexpected mob %s baby=%v", m.Kind, m.Baby)
			continue
		}
		xs = append(xs, m.X)
	}
	if len(xs) != 2 {
		t.Fatalf("%d hatchlings, want 2", len(xs))
	}
	for _, x := range xs {
		if math.Abs(x-5.3) > 0.5 {
			t.Errorf("a hatchling stands at x=%v, want near the egg cell", x)
		}
	}
}

// TestATurtleEggOffSandOrByDayDoesNotHatch: an egg on a non-sand block never
// advances, and by day it advances at the attribute's default chance only.
func TestATurtleEggOffSandOrByDayDoesNotHatch(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, driedGhastSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), driedGhastTickets)
	srv.SetDayTime(21500)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:dirt", nil)
	egg := servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:turtle_egg", map[string]string{"eggs": "1", "hatch": "0"})
	srv.TickRandomTicks(12)
	if got := srv.BlockAt(5, 64, 0); got != egg {
		t.Errorf("an egg on dirt changed to state %d", got)
	}
}
