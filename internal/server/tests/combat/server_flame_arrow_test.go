package combat

// The blocks a burning arrow's landing answers: the TNT it primes and
// takes away, and the campfire or candle it lights. The shaft is flown on a
// stopped loop so the burn is set before it lands and no tick races it.
// vanilla: TntBlock.onProjectileHit, CampfireBlock.onProjectileHit,
// AbstractCandleBlock.onProjectileHit

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// flameScene stops the loop and returns the server a burning arrow is flown
// against, the placement of the block under test left to the caller.
func flameScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.AttachActor(t)
	srv.StopTick()
	return srv, c
}

// TestABurningArrowPrimesTheTNTItSticksIn shoots a burning shaft into a TNT
// block: the block goes to air and a primed body stands in its place.
func TestABurningArrowPrimesTheTNTItSticksIn(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	if !srv.SetBlock(6, 65, 0, servertest.BlockState(t, "minecraft:tnt")) {
		t.Fatal("could not place the TNT")
	}
	id := srv.ShootArrow(8.0, 65.5, 0.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	servertest.DriveTargetTicks(srv, 8)

	if got := srv.BlockAt(6, 65, 0); got != 0 {
		t.Errorf("the TNT = %d, want air after the burning shaft primed it", got)
	}
	if n := len(srv.TNTsInDimension(world.DimensionName)); n != 1 {
		t.Errorf("primed TNT in the overworld = %d, want the one the shaft lit", n)
	}
}

// TestABurningArrowLightsTheCampfireItSticksIn shoots a burning shaft into
// an unlit campfire: the block it stuck in lights.
func TestABurningArrowLightsTheCampfireItSticksIn(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	if !srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:campfire", map[string]string{"lit": "false"})) {
		t.Fatal("could not place the campfire")
	}
	id := srv.ShootArrow(8.0, 65.2, 0.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	servertest.DriveTargetTicks(srv, 8)

	servertest.ExpectBlockNamed(t, srv, 6, 65, 0, "minecraft:campfire", map[string]string{"lit": "true"})
}

// TestAPlainArrowLeavesTheBlocksItSticksInAlone pins the burn as the whole
// of what the hooks ask: a shaft that is not on fire primes no TNT and
// lights no campfire.
func TestAPlainArrowLeavesTheBlocksItSticksInAlone(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	tnt := servertest.BlockState(t, "minecraft:tnt")
	if !srv.SetBlock(6, 65, 0, tnt) {
		t.Fatal("could not place the TNT")
	}
	if !srv.SetBlock(6, 65, 4, servertest.MustState(t, "minecraft:campfire", map[string]string{"lit": "false"})) {
		t.Fatal("could not place the campfire")
	}
	srv.ShootArrow(8.0, 65.5, 0.5, -3, 0, 0)
	srv.ShootArrow(8.0, 65.2, 4.5, -3, 0, 0)
	servertest.DriveTargetTicks(srv, 8)

	if got := srv.BlockAt(6, 65, 0); got != tnt {
		t.Errorf("the TNT = %d, want the %d a plain shaft leaves", got, tnt)
	}
	if n := len(srv.TNTsInDimension(world.DimensionName)); n != 0 {
		t.Errorf("primed TNT in the overworld = %d, want none", n)
	}
	servertest.ExpectBlockNamed(t, srv, 6, 65, 4, "minecraft:campfire", map[string]string{"lit": "false"})
}

// TestAMobOwnedBurningArrowObeysMobGriefing flies a mob's burning shaft at
// a TNT, a campfire and a candle under mob_griefing off: mayInteract refuses
// the first two, while the candle skips it, as AbstractCandleBlock does.
func TestAMobOwnedBurningArrowObeysMobGriefing(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)
	srv.SetGameRule(world.RuleMobGriefing.String(), false)

	cow := srv.SpawnMob("minecraft:cow", 9.5, 65, 0.5)
	tnt := servertest.BlockState(t, "minecraft:tnt")
	if !srv.SetBlock(6, 65, 0, tnt) {
		t.Fatal("could not place the TNT")
	}
	if !srv.SetBlock(6, 65, 4, servertest.MustState(t, "minecraft:campfire", map[string]string{"lit": "false"})) {
		t.Fatal("could not place the campfire")
	}
	if !srv.SetBlock(6, 65, 8, servertest.MustState(t, "minecraft:candle", map[string]string{"lit": "false"})) {
		t.Fatal("could not place the candle")
	}
	id := srv.ShootMobArrow(cow, 8.0, 65.5, 0.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	id = srv.ShootMobArrow(cow, 8.0, 65.2, 4.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	id = srv.ShootMobArrow(cow, 8.0, 65.2, 8.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	servertest.DriveTargetTicks(srv, 8)

	if got := srv.BlockAt(6, 65, 0); got != tnt {
		t.Errorf("the TNT = %d, want the %d a mob's shaft may not prime", got, tnt)
	}
	if n := len(srv.TNTsInDimension(world.DimensionName)); n != 0 {
		t.Errorf("primed TNT in the overworld = %d, want none", n)
	}
	servertest.ExpectBlockNamed(t, srv, 6, 65, 4, "minecraft:campfire", map[string]string{"lit": "false"})
	servertest.ExpectBlockNamed(t, srv, 6, 65, 8, "minecraft:candle", map[string]string{"lit": "true"})
}

// TestAnUnlitArrowFlownThroughLavaOrFireSetsTheMobItHitsAlight shoots a plain
// shaft through a lava cell or a fire block at a cow: the shaft picks up the
// burn on the way and hands it on.
// vanilla: LavaFluid.entityInside, BaseFireBlock.entityInside
func TestAnUnlitArrowFlownThroughLavaOrFireSetsTheMobItHitsAlight(t *testing.T) {
	t.Parallel()
	for _, block := range []string{"minecraft:lava", "minecraft:fire"} {
		t.Run(block, func(t *testing.T) {
			t.Parallel()
			srv, _ := flameScene(t)
			if !srv.SetBlock(3, 65, 0, servertest.BlockState(t, block)) {
				t.Fatal("could not place the block")
			}
			cow := srv.SpawnMob("minecraft:cow", 8.5, 64, 0.5)
			if srv.ShootArrow(0.5, 65.5, 0.5, 3, 0, 0) == 0 {
				t.Fatal("could not shoot")
			}
			servertest.DriveTargetTicks(srv, 10)

			if srv.MobFireTicks(cow) <= 0 {
				t.Errorf("the cow is not burning after the shaft flew through %s", block)
			}
		})
	}
}
