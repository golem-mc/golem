package combat

// The blocks a burning arrow's landing answers: the TNT it primes and
// takes away, and the campfire or candle it lights. The shaft is flown on a
// stopped loop so the burn is set before it lands and no tick races it.
// vanilla: TntBlock.onProjectileHit, CampfireBlock.onProjectileHit,
// AbstractCandleBlock.onProjectileHit

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
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

// TestABurningArrowLightsTheCandleItSticksIn shoots a burning shaft into an
// unlit candle: the block it stuck in lights.
func TestABurningArrowLightsTheCandleItSticksIn(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	if !srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:candle", map[string]string{"lit": "false"})) {
		t.Fatal("could not place the candle")
	}
	id := srv.ShootArrow(8.0, 65.2, 0.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	servertest.DriveTargetTicks(srv, 8)

	servertest.ExpectBlockNamed(t, srv, 6, 65, 0, "minecraft:candle", map[string]string{"lit": "true"})
}

// TestABurningArrowLightsTheCandleCakeItSticksIn covers the other block of
// the candle hook: a candle cake, which the same onProjectileHit lights.
func TestABurningArrowLightsTheCandleCakeItSticksIn(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	if !srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:candle_cake", map[string]string{"lit": "false"})) {
		t.Fatal("could not place the candle cake")
	}
	id := srv.ShootArrow(8.0, 65.2, 0.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	servertest.DriveTargetTicks(srv, 8)

	servertest.ExpectBlockNamed(t, srv, 6, 65, 0, "minecraft:candle_cake", map[string]string{"lit": "true"})
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

// TestABurningArrowObeysTntExplodes pins the TNT path's own rule: with
// tnt_explodes off TntBlock.prime declines, leaving the block where it
// stood and standing no body up.
func TestABurningArrowObeysTntExplodes(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)
	srv.SetGameRule(world.RuleTNTExplodes.String(), false)

	tnt := servertest.BlockState(t, "minecraft:tnt")
	if !srv.SetBlock(6, 65, 0, tnt) {
		t.Fatal("could not place the TNT")
	}
	id := srv.ShootArrow(8.0, 65.5, 0.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	servertest.DriveTargetTicks(srv, 8)

	if got := srv.BlockAt(6, 65, 0); got != tnt {
		t.Errorf("the TNT = %d, want the %d tnt_explodes keeps", got, tnt)
	}
	if n := len(srv.TNTsInDimension(world.DimensionName)); n != 0 {
		t.Errorf("primed TNT in the overworld = %d, want none", n)
	}
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

// TestAnOwnerlessBurningArrowIgnoresMobGriefing pins that only a mob owner
// is refused: Projectile.mayInteract allows a shaft with no owner, as a
// dispenser's is, whatever mob_griefing says.
func TestAnOwnerlessBurningArrowIgnoresMobGriefing(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)
	srv.SetGameRule(world.RuleMobGriefing.String(), false)

	if !srv.SetBlock(6, 65, 0, servertest.BlockState(t, "minecraft:tnt")) {
		t.Fatal("could not place the TNT")
	}
	if !srv.SetBlock(6, 65, 4, servertest.MustState(t, "minecraft:campfire", map[string]string{"lit": "false"})) {
		t.Fatal("could not place the campfire")
	}
	id := srv.ShootArrow(8.0, 65.5, 0.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	id = srv.ShootArrow(8.0, 65.2, 4.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	servertest.DriveTargetTicks(srv, 8)

	if got := srv.BlockAt(6, 65, 0); got != 0 {
		t.Errorf("the TNT = %d, want air after an ownerless shaft primed it", got)
	}
	if n := len(srv.TNTsInDimension(world.DimensionName)); n != 1 {
		t.Errorf("primed TNT in the overworld = %d, want the one the shaft lit", n)
	}
	servertest.ExpectBlockNamed(t, srv, 6, 65, 4, "minecraft:campfire", map[string]string{"lit": "true"})
}

// TestABurningArrowNamesItsShooterAsThePrimedTNTsSource covers vanilla
// passing the arrow's owner on as the primed TNT's source, so the blast's
// kill credit and damage attribution name the shooter.
func TestABurningArrowNamesItsShooterAsThePrimedTNTsSource(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)

	if !srv.SetBlock(6, 65, 0, servertest.BlockState(t, "minecraft:tnt")) {
		t.Fatal("could not place the TNT")
	}
	id := srv.ShootPlayerArrow("Actor", 8.0, 65.5, 0.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	servertest.DriveTargetTicks(srv, 8)

	tnts := srv.TNTsInDimension(world.DimensionName)
	if len(tnts) != 1 {
		t.Fatalf("primed TNT in the overworld = %d, want the one the shaft lit", len(tnts))
	}
	if tnts[0].OwnerID != c.PlayerEntityID {
		t.Errorf("the primed TNT's owner = %d, want the shooter %d", tnts[0].OwnerID, c.PlayerEntityID)
	}
}

// TestABurningArrowDoesNotLightAWaterloggedBlock covers FlintLight's wet
// half: a waterlogged campfire or candle cannot be lit, however burning the
// shaft.
func TestABurningArrowDoesNotLightAWaterloggedBlock(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	wetCampfire := servertest.MustState(t, "minecraft:campfire", map[string]string{"lit": "false", "waterlogged": "true"})
	if !srv.SetBlock(6, 65, 0, wetCampfire) {
		t.Fatal("could not place the waterlogged campfire")
	}
	wetCandle := servertest.MustState(t, "minecraft:candle", map[string]string{"lit": "false", "waterlogged": "true"})
	if !srv.SetBlock(6, 65, 4, wetCandle) {
		t.Fatal("could not place the waterlogged candle")
	}
	id := srv.ShootArrow(8.0, 65.2, 0.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	id = srv.ShootArrow(8.0, 65.2, 4.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	servertest.DriveTargetTicks(srv, 8)

	servertest.ExpectBlockNamed(t, srv, 6, 65, 0, "minecraft:campfire", map[string]string{"lit": "false", "waterlogged": "true"})
	servertest.ExpectBlockNamed(t, srv, 6, 65, 4, "minecraft:candle", map[string]string{"lit": "false", "waterlogged": "true"})
}

// TestABurningArrowLeavesAnAlreadyLitBlockAlone covers canLight's lit gate:
// a block already burning is not touched again.
func TestABurningArrowLeavesAnAlreadyLitBlockAlone(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	litCampfire := servertest.MustState(t, "minecraft:campfire", map[string]string{"lit": "true"})
	if !srv.SetBlock(6, 65, 0, litCampfire) {
		t.Fatal("could not place the lit campfire")
	}
	litCandle := servertest.MustState(t, "minecraft:candle", map[string]string{"lit": "true"})
	if !srv.SetBlock(6, 65, 4, litCandle) {
		t.Fatal("could not place the lit candle")
	}
	id := srv.ShootArrow(8.0, 65.2, 0.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	id = srv.ShootArrow(8.0, 65.2, 4.5, -3, 0, 0)
	srv.IgniteArrow(id, 100)
	servertest.DriveTargetTicks(srv, 8)

	servertest.ExpectBlockNamed(t, srv, 6, 65, 0, "minecraft:campfire", map[string]string{"lit": "true"})
	servertest.ExpectBlockNamed(t, srv, 6, 65, 4, "minecraft:candle", map[string]string{"lit": "true"})
}

// TestABurningArrowLightsFromAbove covers a hit on the block's top face, the
// descending path the side-on shots do not make.
func TestABurningArrowLightsFromAbove(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	if !srv.SetBlock(6, 65, 0, servertest.MustState(t, "minecraft:campfire", map[string]string{"lit": "false"})) {
		t.Fatal("could not place the campfire")
	}
	id := srv.ShootArrow(6.5, 72.0, 0.5, 0, -3, 0)
	srv.IgniteArrow(id, 100)
	servertest.DriveTargetTicks(srv, 8)

	servertest.ExpectBlockNamed(t, srv, 6, 65, 0, "minecraft:campfire", map[string]string{"lit": "true"})
}

// TestAPlayerOwnedBurningArrowObeysSpawnProtection flies a non-op's burning
// shaft at a TNT and a campfire inside the spawn radius: Level.mayInteract
// refuses both. An op's shaft primes the TNT.
// vanilla: Projectile.mayInteract
func TestAPlayerOwnedBurningArrowObeysSpawnProtection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		ops  string
		hit  bool
	}{
		{"non-op", "Boss", false},
		{"op", "Actor", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, tc.ops), servertest.SpawnProtectionRadius(config.Default().SpawnProtection))
			srv.StopTick()
			tnt := servertest.BlockState(t, "minecraft:tnt")
			if !srv.SetBlock(6, 65, 0, tnt) {
				t.Fatal("could not place the TNT")
			}
			id := srv.ShootPlayerArrow("Actor", 8.0, 65.5, 0.5, -3, 0, 0)
			srv.IgniteArrow(id, 100)
			servertest.DriveTargetTicks(srv, 8)
			if primed := len(srv.TNTsInDimension(world.DimensionName)) == 1; primed != tc.hit {
				t.Errorf("TNT primed = %v, want %v", primed, tc.hit)
			}
			if got := srv.BlockAt(6, 65, 0); (got == tnt) == tc.hit {
				t.Errorf("TNT block = %d, want it kept only when refused", got)
			}
		})
	}
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

// TestAdventureBurningProjectilesRequireCanBreak checks the pickup stack,
// including tridents, rather than the shooter's held item or weapon.
// vanilla: TntBlock.onProjectileHit, AbstractArrow.getPickupItemStackOrigin
func TestAdventureBurningProjectilesRequireCanBreak(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"arrow", "trident"} {
		for _, predicate := range []string{"default", "stone", "tnt"} {
			t.Run(kind+"/"+predicate, func(t *testing.T) {
				srv, _ := flameScene(t)
				srv.SetPlayerGameMode("Actor", protocol.GameModeAdventure)
				tnt := servertest.BlockState(t, "minecraft:tnt")
				srv.SetBlock(6, 65, 0, tnt)
				item := ""
				if predicate != "default" {
					item = fmt.Sprintf(`,item:{id:"minecraft:%s",count:1,components:{"minecraft:can_break":{blocks:"minecraft:%s"}}}`, kind, predicate)
				}
				// A weapon's permission cannot authorize the pickup item.
				srv.RunLine(fmt.Sprintf(`summon minecraft:%s 8 65.5 0.5 {Owner:%s,Motion:[-3d,0d,0d],Fire:100s,NoGravity:1b,weapon:{id:"minecraft:bow",count:1,components:{"minecraft:can_break":{blocks:"minecraft:tnt"}}}%s}`, kind, actorOwner(), item))
				if arrows := srv.ArrowsInDimension(world.DimensionName); len(arrows) != 1 || !arrows[0].Burning {
					t.Fatalf("summoned projectiles = %+v, want one burning projectile", arrows)
				}
				servertest.DriveTargetTicks(srv, 8)
				allowed := predicate == "tnt"
				if got := srv.BlockAt(6, 65, 0); (got == 0) != allowed || (!allowed && got != tnt) {
					t.Errorf("TNT block = %d, want primed only with matching can_break (%v)", got, allowed)
				}
				want := 0
				if allowed {
					want = 1
				}
				if n := len(srv.TNTsInDimension(world.DimensionName)); n != want {
					t.Errorf("primed TNT = %d, want %d", n, want)
				}
			})
		}
	}
}

// TestAdventureFireballCannotPrimeTnt checks that a non-arrow projectile
// supplies an empty stack, even if the shooter's hand has can_break.
// vanilla: TntBlock.onProjectileHit
func TestAdventureFireballCannotPrimeTnt(t *testing.T) {
	t.Parallel()
	for _, adventure := range []bool{false, true} {
		t.Run(fmt.Sprint(adventure), func(t *testing.T) {
			srv, c := flameScene(t)
			if adventure {
				srv.SetPlayerGameMode("Actor", protocol.GameModeAdventure)
			}
			srv.GiveSlot("Actor", player.HotbarSlot, servertest.AdventuredStack(t, "minecraft:flint_and_steel", protocol.DataComponentCanBreak, "minecraft:tnt"))
			tnt := servertest.BlockState(t, "minecraft:tnt")
			srv.SetBlock(6, 65, 0, tnt)
			id := srv.LaunchFireball(entity.ThrownSmallFireball, 8, 65.5, 0.5, -1, 0, 0, c.PlayerEntityID, 1)
			if id == 0 {
				t.Fatal("could not launch the fireball")
			}
			servertest.DriveTargetTicks(srv, 8)
			want := uint16(0)
			if adventure {
				want = tnt
			}
			if got := srv.BlockAt(6, 65, 0); got != want {
				t.Errorf("TNT block = %d, want %d", got, want)
			}
		})
	}
}
