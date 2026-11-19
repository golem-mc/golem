package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The bamboo, black-box: the random ticks the vanilla BambooSaplingBlock and
// BambooStalkBlock run on their own clocks — the sapling's one-in-three roll
// turning it and the cell over it into a stalk, and a stage-0 stalk gaining a
// cell above it — with the dice seeded and the tick rule turned up, so the
// growth writes reach the client inside a pass count the test can afford.

// bambooSaplingGrew drives random tick passes one at a time until the block at
// (x, y, z) is no longer a bamboo sapling, and reports whether it grew.
func bambooSaplingGrew(srv *server.Server, x, y, z int32) bool {
	for range 500 {
		if name, _, _ := registry.BlockStateName(srv.BlockAt(x, y, z)); name != "minecraft:bamboo_sapling" {
			return true
		}
		srv.TickRandomTicks(1)
	}
	return false
}

// TestBambooSaplingGrowsOnRandomTicks checks the sapling's own random tick: a
// sapling on grass under the open sky becomes a stalk, the cell above it grown
// with small leaves, and the client is sent the block update of the sapling it
// became.
func TestBambooSaplingGrowsOnRandomTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.GrowthTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:bamboo_sapling", nil)

	if !bambooSaplingGrew(srv, 5, 64, 0) {
		t.Fatal("the sapling never grew under the raised dice")
	}

	if name, props := servertest.StateProps(srv.BlockAt(5, 64, 0)); name != "minecraft:bamboo" || props["leaves"] != "none" {
		t.Errorf("the sapling stands as %s %v, want the bamboo it became", name, props)
	}
	if name, props := servertest.StateProps(srv.BlockAt(5, 65, 0)); name != "minecraft:bamboo" || props["leaves"] != "small" {
		t.Errorf("above the sapling stands %s %v, want a bamboo with small leaves", name, props)
	}

	// The growth is a flags-3 write, so the client hears of the cell as bamboo.
	servertest.AwaitBlockEventually(t, c, 5, 64, 0, servertest.CropsStateOf(t, "minecraft:bamboo", nil))
}

// TestBambooStalkGrowsOnRandomTicks checks a stalk's own random tick: a
// stage-0 stalk on grass under the open sky gains a cell above it, the client
// told of the new cell.
func TestBambooStalkGrowsOnRandomTicks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.GrowthTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:grass_block", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:bamboo",
		map[string]string{"age": "0", "leaves": "none", "stage": "0"})

	grown := false
	for range 500 {
		if name, _, _ := registry.BlockStateName(srv.BlockAt(5, 65, 0)); name == "minecraft:bamboo" {
			grown = true
			break
		}
		srv.TickRandomTicks(1)
	}
	if !grown {
		t.Fatal("the stalk never grew a cell under the raised dice")
	}

	if name, props := servertest.StateProps(srv.BlockAt(5, 65, 0)); name != "minecraft:bamboo" ||
		props["age"] != "0" || props["leaves"] != "small" || props["stage"] != "0" {
		t.Errorf("above the stalk stands %s %v, want a young bamboo with small leaves", name, props)
	}

	// The growth is a flags-3 write, so the client hears of the new cell.
	servertest.AwaitBlockEventually(t, c, 5, 65, 0,
		servertest.CropsStateOf(t, "minecraft:bamboo", map[string]string{"age": "0", "leaves": "small", "stage": "0"}))
}

// TestBambooStalkAtBuildCeilingReshufflesLeaves checks a stage-0 stalk topping
// out at the build ceiling: the cell above reads as air and full sky light, so
// the tick still runs growBambooCell, which rewrites the leaves of the two
// cells below even though the new cell at y=320 cannot be placed.
func TestBambooStalkAtBuildCeilingReshufflesLeaves(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Gardener", servertest.OpsListing(t, "Gardener"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), servertest.GrowthTickSpeed)

	servertest.SetFarmBlock(t, c, srv, 5, 316, 0, "minecraft:grass_block", nil)
	// The bottom stalk is stage 1, so it never ticks and cannot rewrite 318.
	servertest.SetFarmBlock(t, c, srv, 5, 317, 0, "minecraft:bamboo",
		map[string]string{"age": "0", "leaves": "none", "stage": "1"})
	servertest.SetFarmBlock(t, c, srv, 5, 318, 0, "minecraft:bamboo",
		map[string]string{"age": "0", "leaves": "large", "stage": "0"})
	servertest.SetFarmBlock(t, c, srv, 5, 319, 0, "minecraft:bamboo",
		map[string]string{"age": "0", "leaves": "none", "stage": "0"})

	reshuffled := false
	for range 500 {
		if _, props := servertest.StateProps(srv.BlockAt(5, 318, 0)); props["leaves"] == "small" {
			reshuffled = true
			break
		}
		srv.TickRandomTicks(1)
	}
	if !reshuffled {
		t.Fatal("the ceiling stalk never rewrote the leaves below it")
	}
}
