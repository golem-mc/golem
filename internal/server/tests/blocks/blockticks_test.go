package blocks

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The blocks vanilla changes on its own: a dirt path under a solid block,
// frosted ice in the light, and a chorus flower or plant that has lost its
// hold. Each is a scheduled or random tick the physics engine runs, driven by
// hand with the main loop stopped the way the farm tests drive theirs.

// TestADirtPathUnderASolidBlockTurnsToDirt checks the path's one-tick death:
// a solid block set over a dirt path books the visit the vanilla
// DirtPathBlock.updateShape writes, and the visit turns the path to dirt.
func TestADirtPathUnderASolidBlockTurnsToDirt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:dirt_path", nil)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:stone", nil)

	srv.TickPhysics(1)

	dirt := servertest.CropsStateOf(t, "minecraft:dirt", nil)
	servertest.AwaitBlockUpdate(t, c, 5, 63, 0, dirt)
	if got := srv.BlockAt(5, 63, 0); got != dirt {
		t.Errorf("the path under the stone = %d, want %d (dirt)", got, dirt)
	}
}

// TestFrostedIceMeltsUnderTheSky checks the ice's aging tick: an aged block
// in the open sky, where the raw light is past 11 minus its age, melts to
// water once the visit the vanilla FrostedIceBlock.onPlace books comes due.
func TestFrostedIceMeltsUnderTheSky(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	servertest.SeedPhysics(t, srv, servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:frosted_ice", map[string]string{"age": "3"})

	srv.TickPhysics(150)

	water := servertest.CropsStateOf(t, "minecraft:water", map[string]string{"level": "0"})
	servertest.AwaitBlockUpdate(t, c, 5, 64, 0, water)
	if got := srv.BlockAt(5, 64, 0); got != water {
		t.Errorf("the frosted ice in the sky = %d, want %d (water)", got, water)
	}
}

// TestAnUnsupportedChorusFlowerBreaks checks the flower's end: a chorus
// flower set where nothing holds it up is not where the vanilla
// ChorusFlowerBlock.canSurvive lets it stand, and the updateShape's one-tick
// visit destroys it. The block's loot table requires a breaking entity
// (entity_properties: this), so an unsupported flower breaking on its own
// leaves no drop, as the vanilla tick's destroyBlock(pos, true) does.
func TestAnUnsupportedChorusFlowerBreaks(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()
	srv.LootSeed(servertest.CropsSeed)

	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:chorus_flower", map[string]string{"age": "0"})
	srv.TickPhysics(1)

	drops := servertest.AwaitCropBreak(t, c, 5, 64, 0, 0)
	if got := srv.BlockAt(5, 64, 0); got != 0 {
		t.Errorf("the unsupported chorus flower = %d, want 0 (air)", got)
	}
	for _, drop := range drops {
		if name, _ := registry.ItemName(drop.ItemID); name != "minecraft:chorus_flower" {
			t.Errorf("the unsupported chorus flower dropped %s, want only chorus_flower or nothing", name)
		}
	}
}

// TestAnItemOnAPathIsPushedUpAsItTurnsToDirt checks that the vanilla
// Block.pushEntitiesUp reaches a dropped item: one resting on the 15/16-high
// path that becomes full dirt is raised with it, not left inside the block.
func TestAnItemOnAPathIsPushedUpAsItTurnsToDirt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Camper", servertest.OpsListing(t, "Camper"))

	srv.StopTick()

	servertest.SetFarmBlock(t, c, srv, 5, 63, 0, "minecraft:dirt_path", nil)
	srv.SpawnItemEntity(5.5, 63+15.0/16.0+entity.ItemHalfSize, 0.5, "minecraft:stick", 1)
	servertest.SetFarmBlock(t, c, srv, 5, 64, 0, "minecraft:stone", nil)
	srv.TickPhysics(1)

	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 {
		t.Fatalf("item entities in the world = %d, want 1", len(items))
	}
	// The item's Y is its box's centre; its feet rest on the dirt's top.
	if got := items[0].Y - entity.ItemHalfSize; servertest.Abs64(got-64.0) > 0.001 {
		t.Errorf("the item on the path that turned to dirt = feet y %v, want 64", got)
	}
}
