package blocks

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// A burning mob, arrow or item in a layered cauldron is put out and the
// cauldron drops a level.
// vanilla: LayeredCauldronBlock.entityInside

func TestCauldronPutsOutABurningMobAndDropsALevel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, block, props string
		fire               int
		wantBlock          string
		wantLevel          string
	}{
		{"water", "minecraft:water_cauldron", "level=3", 200, "minecraft:water_cauldron", "2"},
		{"powder snow", "minecraft:powder_snow_cauldron", "level=2", 200, "minecraft:water_cauldron", "1"},
		{"not burning", "minecraft:water_cauldron", "level=3", 0, "minecraft:water_cauldron", "3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Warden", servertest.OpsListing(t, "Warden"))
			c.Settle()
			srv.SetDayTime(servertest.TestMidnight)

			cow := srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
			c.ExpectMobSpawn(protocol.EntityTypeCow)
			srv.StopTick()
			c.Settle()
			if !srv.SetMobNoAI(cow) {
				t.Fatal("the cow could not be held")
			}
			if !srv.SetBlock(4, 64, 0, servertest.BlockStateWith(t, tc.block, tc.props)) {
				t.Fatal("the chunk was not loaded")
			}
			srv.SetMobFire(cow, tc.fire)
			srv.TickMobs(1)
			c.Settle()

			if fire := srv.MobFire(cow); fire > 0 {
				t.Errorf("the cow kept %d fire ticks in the cauldron, want it put out", fire)
			}
			servertest.ExpectBlockNamed(t, srv, 4, 64, 0, tc.wantBlock, map[string]string{"level": tc.wantLevel})
		})
	}
}

func TestCauldronPutsOutABurningArrowAndDropsALevel(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.AttachActor(t)
	srv.StopTick()
	if !srv.SetBlock(6, 65, 0, servertest.BlockStateWith(t, "minecraft:water_cauldron", "level=3")) {
		t.Fatal("could not place the cauldron")
	}
	id := srv.ShootArrow(8.0, 65.2, 0.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	servertest.DriveTargetTicks(srv, 4)

	if fire := srv.ArrowFire(id); fire != 0 {
		t.Errorf("the arrow kept %d fire ticks in the cauldron, want 0", fire)
	}
	servertest.ExpectBlockNamed(t, srv, 6, 65, 0, "minecraft:water_cauldron", map[string]string{"level": "2"})
}

// A burning arrow whose box crosses a cauldron's corner between two
// tick-end positions clear of it is put out in that tick: at three blocks a
// tick from x 8 to x 5, its box (z 0.95 to 1.45, y 64.7 to 65.2) passes the
// cauldron at x 6 while its centre flies by outside the walls.
// vanilla: Entity.checkInsideBlocks, BlockGetter.forEachBlockIntersectedBetween
func TestCauldronPutsOutABurningArrowCrossingItWithinATick(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.AttachActor(t)
	srv.StopTick()
	if !srv.SetBlock(6, 65, 0, servertest.BlockStateWith(t, "minecraft:water_cauldron", "level=3")) {
		t.Fatal("could not place the cauldron")
	}
	id := srv.ShootArrow(8.0, 64.7, 1.2, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	srv.TickEntities(1)

	if fire := srv.ArrowFire(id); fire != 0 {
		t.Errorf("the arrow kept %d fire ticks after crossing the cauldron, want 0", fire)
	}
	servertest.ExpectBlockNamed(t, srv, 6, 65, 0, "minecraft:water_cauldron", map[string]string{"level": "2"})
}

// A mob's arrow only touches the cauldron while mob_griefing allows it.
// vanilla: Projectile.mayInteract
func TestCauldronHoldsAgainstAMobArrowWithoutGriefing(t *testing.T) {
	t.Parallel()
	for _, grief := range []bool{true, false} {
		name := "griefing off"
		level := "3"
		if grief {
			name, level = "griefing on", "2"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Warden", servertest.OpsListing(t, "Warden"))
			c.Settle()
			srv.SetGameRule(world.RuleMobGriefing.String(), grief)
			cow := srv.SpawnMob("minecraft:cow", 20.5, 64, 20.5)
			c.ExpectMobSpawn(protocol.EntityTypeCow)
			srv.StopTick()
			c.Settle()
			if !srv.SetBlock(6, 65, 0, servertest.BlockStateWith(t, "minecraft:water_cauldron", "level=3")) {
				t.Fatal("could not place the cauldron")
			}
			id := srv.ShootMobArrowFrom(cow, 8.0, 65.2, 0.5, -3, 0, 0)
			srv.IgniteArrow(id, 100)
			servertest.DriveTargetTicks(srv, 4)

			servertest.ExpectBlockNamed(t, srv, 6, 65, 0, "minecraft:water_cauldron", map[string]string{"level": level})
		})
	}
}

func TestCauldronPutsOutABurningItemAndDropsALevel(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.AttachActor(t)
	srv.StopTick()
	if !srv.SetBlock(6, 65, 0, servertest.BlockStateWith(t, "minecraft:water_cauldron", "level=3")) {
		t.Fatal("could not place the cauldron")
	}
	// The box (65.275 to 65.525) clears the floor at 4/16, where an item
	// stuck in the collision takes no block effects, and sits in the water.
	// vanilla: AbstractCauldronBlock.SHAPE_INSIDE, Entity.isAffectedByBlocks
	id := srv.SpawnItemEntity(6.5, 65.4, 0.5, "minecraft:stick", 1)
	srv.SetItemFire(id, 100)
	srv.TickEntities(1)

	if fire := srv.ItemFire(id); fire > 0 {
		t.Errorf("the item kept %d fire ticks in the cauldron, want it put out", fire)
	}
	servertest.ExpectBlockNamed(t, srv, 6, 65, 0, "minecraft:water_cauldron", map[string]string{"level": "2"})
}
