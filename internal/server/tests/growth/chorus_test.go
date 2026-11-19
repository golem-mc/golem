package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The chorus flower's random tick, black-box: a young flower with room above
// grows a stem upward, a flower it cannot grow at all dies at age five, and a
// dead flower is no state the dice land on. The growth and death carry the
// level events 1033 and 1034.

// chorusTestConfig cuts the simulation distance to the client's render
// distance (four in configure), so the join loads every held chunk up front
// and the random passes below see a settled world: the paced chunk takes
// would otherwise grow the chunk set mid-pass and shift the pinned dice.
func chorusTestConfig() *config.Config {
	cfg := servertest.BlockTestConfig()
	cfg.SimulationDistance = 4
	return cfg
}

// TestChorusFlowerGrowsUpOnEndStone checks the upward growth: a flower on end
// stone whose space above is free becomes a chorus plant joined to the flower
// above (up=true), a young flower stands one block higher, and the growth
// plays level event 1033 at the new flower.
func TestChorusFlowerGrowsUpOnEndStone(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, chorusTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), 1)

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:end_stone", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:chorus_flower", map[string]string{"age": "0"})

	srv.TickRandomTicks(20000)

	stem := servertest.CropsStateOf(t, "minecraft:chorus_plant", map[string]string{"down": "true", "up": "true"})
	flower := servertest.CropsStateOf(t, "minecraft:chorus_flower", map[string]string{"age": "0"})
	sawStem, sawFlower, sawGrow := false, false, false
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
			for _, ch := range servertest.BlockChangesIn(t, id, data) {
				if ch.X == 5 && ch.Y == 64 && ch.Z == 0 && ch.State == stem {
					sawStem = true
				}
				if ch.X == 5 && ch.Y == 65 && ch.Z == 0 && ch.State == flower {
					sawFlower = true
				}
			}
		case protocol.IDPlayWorldEvent:
			r := protocol.NewReader(data)
			if r.ReadI32() != 1033 {
				return
			}
			ex, ey, ez := r.ReadPosition()
			if ex == 5 && ey == 65 && ez == 0 {
				sawGrow = true
			}
		}
	})
	if !sawStem {
		t.Error("the end-stone flower never left a chorus plant below it")
	}
	if !sawFlower {
		t.Error("no young chorus flower appeared one block above the flower")
	}
	if !sawGrow {
		t.Error("the growth never played level event 1033 at the new flower")
	}
}

// TestAChorusFlowerBranches checks the sideways growth: a flower whose cell
// above is hemmed in branches into a neighbouring flower one age older, and
// the plant it leaves behind joins that branch on the branch's side.
func TestAChorusFlowerBranches(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, chorusTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), 1)

	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:end_stone", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 65, 0, "minecraft:chorus_plant", map[string]string{"down": "true"})
	servertest.SetFarmBlock(t, c, srv, 5, 66, 0, "minecraft:chorus_flower", map[string]string{"age": "0"})
	for _, at := range [][3]int32{{4, 67, 0}, {6, 67, 0}, {5, 67, -1}, {5, 67, 1}} {
		servertest.SetFarmBlock(t, c, srv, at[0], at[1], at[2], "minecraft:stone", nil)
	}

	srv.TickRandomTicks(20000)

	base := srv.BlockAt(5, 66, 0)
	props := servertest.CropsProps(t, base, "minecraft:chorus_plant")
	joined := false
	for _, br := range []struct {
		dx, dz int32
		face   string
	}{
		{0, -1, "north"}, {0, 1, "south"}, {-1, 0, "west"}, {1, 0, "east"},
	} {
		name := srv.BlockNameAt(5+br.dx, 66, 0+br.dz)
		if name != "minecraft:chorus_flower" && name != "minecraft:chorus_plant" {
			continue
		}
		if props[br.face] != "true" {
			t.Errorf("the base's %s connection = %q, want true for the branch", br.face, props[br.face])
		}
		joined = true
	}
	if !joined {
		t.Error("the flower hemmed in above made no sideways branch")
	}
}

// TestPlacingAChorusPlantJoinsItsNeighbours checks ChorusPlantBlock's
// placement: a plant placed on end stone gets down=true, one placed under a
// chorus flower gets up=true, and a plant beside a plant joins it, whose own
// side then joins back through the shape update.
func TestPlacingAChorusPlantJoinsItsNeighbours(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	plant := func(props map[string]string) uint16 {
		return servertest.CropsStateOf(t, "minecraft:chorus_plant", props)
	}
	item, _ := registry.ItemID("minecraft:chorus_plant")
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: item, Count: 1})

	srv.SetBlock(3, 63, 0, servertest.BlockState(t, "minecraft:end_stone"))
	srv.SetBlock(5, 65, 0, servertest.CropsStateOf(t, "minecraft:chorus_flower", map[string]string{"age": "0"}))
	srv.SetBlock(5, 63, 0, servertest.BlockState(t, "minecraft:end_stone"))

	// On end stone: down only.
	actor.FacePlayer(4.5, 64, -2.5, 0, 0)
	actor.PlaceBlock(3, 63, 0, protocol.FaceTop)
	actor.AwaitBlockState(3, 64, 0, plant(map[string]string{"down": "true"}))

	// Under a flower, on end stone: down and up.
	actor.PlaceBlock(5, 63, 0, protocol.FaceTop)
	actor.AwaitBlockState(5, 64, 0, plant(map[string]string{"down": "true", "up": "true"}))

	// Beside the first plant, against its east face: west joins it.
	actor.PlaceBlock(3, 64, 0, protocol.FaceEast)
	actor.AwaitBlockState(4, 64, 0, plant(map[string]string{"west": "true", "east": "true"}))

	// North and south plants, a flower to the west, a stone floor below
	// (which does not support a plant): only the sides join. The plant at
	// (8,64,0) has a flower west of it and plants north and south.
	srv.SetBlock(8, 63, 0, servertest.BlockState(t, "minecraft:stone"))
	srv.SetBlock(7, 64, 0, servertest.CropsStateOf(t, "minecraft:chorus_flower", map[string]string{"age": "0"}))
	srv.SetBlock(8, 63, -1, servertest.BlockState(t, "minecraft:end_stone"))
	srv.SetBlock(8, 63, 1, servertest.BlockState(t, "minecraft:end_stone"))
	srv.SetBlock(8, 64, -1, plant(nil))
	srv.SetBlock(8, 64, 1, plant(nil))
	actor.FacePlayer(8.5, 66, -2.5, 0, 0)
	actor.PlaceBlock(8, 63, 0, protocol.FaceTop)
	actor.AwaitBlockState(8, 64, 0, plant(map[string]string{"north": "true", "south": "true", "west": "true"}))
}
