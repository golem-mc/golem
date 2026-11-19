package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

// The plantings that age under the random ticks, black-box: the nether
// wart on its soul sand and the cocoa pod on its log ripening a step at a
// time until each reaches its last age, the berry bush doing the same only
// under an open-enough sky, and a picked bush — a pick that leaves it at
// age one — ripening back to its ripe one, in the overworld and in another
// dimension. The dice are seeded and the tick speed raised, so the pass
// counts are tens of hits on the one cell each test watches.

// TestNetherWartRipensUnderRandomTicks checks the wart's growth: a wart at
// its first age on soul sand climbs under the random ticks to the third,
// the age none of its dice can pass.
func TestNetherWartRipensUnderRandomTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:soul_sand", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:nether_wart", map[string]string{"age": "0"})

	srv.TickRandomTicks(20000)

	if age := servertest.CropsProps(t, srv.BlockAt(5, 64, 0), "minecraft:nether_wart")["age"]; age != "3" {
		t.Errorf("the wart ripened to age=%s, want 3", age)
	}
}

// TestCocoaPodsRipenUnderRandomTicks checks the pod's growth: a first-age
// pod on a jungle log climbs to the second, the age none of its dice can
// pass, its facing kept as the steps write it.
func TestCocoaPodsRipenUnderRandomTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 6, 64, 0, "minecraft:jungle_log", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:cocoa",
		map[string]string{"age": "0", "facing": "east"})

	srv.TickRandomTicks(20000)

	props := servertest.CropsProps(t, srv.BlockAt(5, 64, 0), "minecraft:cocoa")
	if age := props["age"]; age != "2" {
		t.Errorf("the pod ripened to age=%s, want 2", age)
	}
	if facing := props["facing"]; facing != "east" {
		t.Errorf("the ripened pod faces %s, want it kept east", facing)
	}
}

// TestAPickedBerryBushRegrows checks the regrowth: a bush dropped back to
// its first berried age — what every pick leaves — climbs again to its
// ripe one under the same passes.
func TestAPickedBerryBushRegrows(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:sweet_berry_bush", map[string]string{"age": "1"})

	srv.TickRandomTicks(20000)

	if age := servertest.CropsProps(t, srv.BlockAt(5, 64, 0), "minecraft:sweet_berry_bush")["age"]; age != "3" {
		t.Errorf("the picked bush ripened back to age=%s, want 3", age)
	}
}
