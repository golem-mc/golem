package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

// The weathering copper's oxidation, black-box: the bare copper block
// climbing a stage under the pass's dice until the first change the test
// catches, the waxed twin holding through every pass, and the state
// properties — a slab's half, a door's halves and hinge, a chest's facing
// — arriving at the next stage whole. The dice are pinned and the tick
// rule raised the way the farm tests pin and raise theirs; one stage is
// the only change a landed die can write, so the caught change names the
// stage the block stopped on.

// TestCopperOxidizesOnRandomTicks checks the family's advance: a copper
// block's first stage under the random ticks is exposed_copper, and the
// waxed twin two squares off stays waxed through the same passes — wax
// is not one of the stages a neighbour's weathering sees, so the bare
// block beside it advances as if alone.
func TestCopperOxidizesOnRandomTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:copper_block", nil)
	servertest.SetFarmBlock(t, c, srv, 7, 64, 0, "minecraft:waxed_copper_block", nil)

	var weathered string
	for range 200 {
		srv.TickRandomTicks(100)
		if w := servertest.CropsNameOf(t, srv.BlockAt(7, 64, 0)); w != "minecraft:waxed_copper_block" {
			t.Fatalf("the waxed twin weathered to %s, want it staying waxed", w)
		}
		if name := servertest.CropsNameOf(t, srv.BlockAt(5, 64, 0)); name != "minecraft:copper_block" {
			weathered = name
			break
		}
	}
	if weathered == "" {
		t.Fatal("20000 random tick passes never weathered the copper block")
	}
	if weathered != "minecraft:exposed_copper" {
		t.Errorf("the copper block's first stage was %s, want minecraft:exposed_copper", weathered)
	}
}
