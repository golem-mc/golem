package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
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

// TestNetherWartRipensBeyondTheOverworld checks the ripening off the
// overworld: the pass the gamerule drives runs the same dice in every
// dimension, and a wart set on soul sand on the end's island ripens there
// as it does at home. No light is asked anywhere, so the void's skyless
// dark is another thing the wart is judged not by.
func TestNetherWartRipensBeyondTheOverworld(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	arrival := world.EndArrival()
	srv.MovePlayerToDimension("Camper", world.EndName,
		float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+0.5)
	// The arrival's chunk stream has to be read: a connection that stops
	// reading holds the whole server's tick loop up on its socket, timing
	// out everyone else's game.
	c.Settle()
	soul, ok := registry.FindState("minecraft:soul_sand", nil)
	if !ok {
		t.Fatal("the block registry has no soul sand")
	}
	servertest.PollUntil(t, 10*time.Second, func() bool {
		return srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z, soul)
	})

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)

	wart, ok := registry.FindState("minecraft:nether_wart", map[string]string{"age": "0"})
	if !ok {
		t.Fatal("the block registry has no nether wart at age 0")
	}
	if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y+1, arrival.Z, wart) {
		t.Fatal("the wart was not placed in the end")
	}

	srv.TickRandomTicks(20000)

	got := srv.BlockInDimension(world.EndName, arrival.X, arrival.Y+1, arrival.Z)
	if age := servertest.CropsProps(t, got, "minecraft:nether_wart")["age"]; age != "3" {
		t.Errorf("the end's wart ripened to age=%s, want 3", age)
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

// TestBerryBushesRipenOnlyUnderTheSky checks the berry bush's growth and
// its light gate: a bush a full block shuts the sky off over stands at its
// first age however many passes run, and the same square with the sky open
// again ripens to the third.
func TestBerryBushesRipenOnlyUnderTheSky(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.CropsTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 65, 0, "minecraft:stone", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:sweet_berry_bush", map[string]string{"age": "0"})

	srv.TickRandomTicks(10000)
	if age := servertest.CropsProps(t, srv.BlockAt(5, 64, 0), "minecraft:sweet_berry_bush")["age"]; age != "0" {
		t.Fatalf("the covered bush ripened to age=%s, want it standing at 0", age)
	}

	servertest.SetFarmBlock(t, c, srv, 5, 65, 0, "minecraft:air", nil)
	srv.TickRandomTicks(20000)
	if age := servertest.CropsProps(t, srv.BlockAt(5, 64, 0), "minecraft:sweet_berry_bush")["age"]; age != "3" {
		t.Errorf("the bush ripened to age=%s under the open sky, want 3", age)
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
