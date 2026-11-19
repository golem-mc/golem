package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// fireballWall launches a hurting projectile of the kind at the block at
// (6, 65, 0) from three blocks west of it and runs the loop until it is gone.
func fireballWall(t *testing.T, srv *server.Server, kind entity.ThrownKind, power int) {
	t.Helper()
	if srv.LaunchFireball(kind, 3.5, 65.5, 0.5, 1, 0, 0, 0, power) == 0 {
		t.Fatal("could not launch the projectile")
	}
	servertest.DriveTargetTicks(srv, 30)
}

// TestASmallFireballLightsTheAirInFrontOfTheWall checks the fire it sets on
// the face it struck, and that it breaks nothing.
// vanilla: SmallFireball.onHitBlock
func TestASmallFireballLightsTheAirInFrontOfTheWall(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)
	stone := servertest.BlockState(t, "minecraft:stone")
	srv.SetBlock(6, 64, 0, stone)
	if srv.LaunchFireball(entity.ThrownSmallFireball, 3.5, 64.5, 0.5, 1, 0, 0, 0, 1) == 0 {
		t.Fatal("could not launch")
	}
	servertest.DriveTargetTicks(srv, 30)

	if name, _ := servertest.StateProps(srv.BlockAt(5, 64, 0)); name != "minecraft:fire" {
		t.Errorf("block in front of the wall = %q, want minecraft:fire", name)
	}
	if got := srv.BlockAt(6, 64, 0); got != stone {
		t.Errorf("the wall = %d, want it untouched", got)
	}
}

// TestALargeFireballBlowsAHoleInSoftGround checks the power 1 blast on a
// dirt block, and that mob_griefing off keeps the block.
// vanilla: LargeFireball.onHit
func TestALargeFireballBlowsAHoleInSoftGround(t *testing.T) {
	t.Parallel()
	for _, griefing := range []bool{true, false} {
		srv, _ := flameScene(t)
		dirt := servertest.BlockState(t, "minecraft:dirt")
		srv.SetBlock(6, 65, 0, dirt)
		srv.SetGameRule("mob_griefing", griefing)

		fireballWall(t, srv, entity.ThrownLargeFireball, 1)

		want := dirt
		if griefing {
			want = 0
		}
		if got := srv.BlockAt(6, 65, 0); got != want {
			t.Errorf("mob_griefing %v: the dirt = %d, want %d", griefing, got, want)
		}
	}
}

// TestALargeFireballsPowerSetsTheBlastRadius checks that a power 3 blast
// reaches the dirt two blocks sideways that a power 1 one leaves.
// vanilla: LargeFireball.explosionPower
func TestALargeFireballsPowerSetsTheBlastRadius(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		power int
		gone  bool
	}{{1, false}, {3, true}} {
		srv, _ := flameScene(t)
		dirt := servertest.BlockState(t, "minecraft:dirt")
		for dz := int32(-2); dz <= 2; dz++ {
			srv.SetBlock(6, 65, dz, dirt)
		}

		fireballWall(t, srv, entity.ThrownLargeFireball, tc.power)

		if gone := srv.BlockAt(6, 65, 2) != dirt; gone != tc.gone {
			t.Errorf("power %d: dirt two blocks sideways gone = %v, want %v", tc.power, gone, tc.gone)
		}
	}
}

// TestAWitherSkullBlastsPowerOneWithoutFire checks the skull's blast on soft
// ground and that it sets no fire.
// vanilla: WitherSkull.onHit
func TestAWitherSkullBlastsPowerOneWithoutFire(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)
	srv.SetBlock(6, 65, 0, servertest.BlockState(t, "minecraft:dirt"))
	srv.SetBlock(6, 64, 0, servertest.BlockState(t, "minecraft:dirt"))

	fireballWall(t, srv, entity.ThrownWitherSkull, 5)

	if got := srv.BlockAt(6, 65, 0); got != 0 {
		t.Errorf("the dirt = %d, want it blown away", got)
	}
	for _, y := range []int32{64, 65, 66} {
		if name, _ := servertest.StateProps(srv.BlockAt(5, y, 0)); name == "minecraft:fire" {
			t.Errorf("fire at y=%d", y)
		}
	}
}

// TestFireballsHurtAndIgniteTheMobTheyHit checks the 5 points and the burn
// of a small fireball and the 6 of a large one.
// vanilla: SmallFireball.onHitEntity, LargeFireball.onHitEntity
func TestFireballsHurtAndIgniteTheMobTheyHit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		kind   entity.ThrownKind
		damage float64
		burns  bool
	}{
		{"small", entity.ThrownSmallFireball, 5, true},
		{"large", entity.ThrownLargeFireball, 6, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := flameScene(t)
			srv.SetGameRule("mob_griefing", false)
			// x=8.5: a fireball whose tick starts inside the mob's box grown by
			// the hit margin does not hit it that tick (AABB.clip), so the cow
			// sits where a flight step begins outside that box.
			cow := srv.SpawnMob("minecraft:cow", 8.5, 64, 0.5)
			before, _ := srv.MobHealth(cow)

			if srv.LaunchFireball(tc.kind, 3.5, 65.0, 0.5, 1, 0, 0, 0, 1) == 0 {
				t.Fatal("could not launch")
			}
			servertest.DriveTargetTicks(srv, 30)

			after, _ := srv.MobHealth(cow)
			// A large fireball's blast also lands, so only the floor is pinned.
			if before-after < tc.damage {
				t.Errorf("health %v -> %v, want at least %v lost", before, after, tc.damage)
			}
			if burning := srv.MobFireTicks(cow) > 0; burning != tc.burns {
				t.Errorf("burning = %v, want %v", burning, tc.burns)
			}
		})
	}
}

// TestAFireballSparesAFireImmuneMob checks that a magma cube takes no
// is_fire blow and is not put alight.
// vanilla: Entity.isInvulnerableToBase
func TestAFireballSparesAFireImmuneMob(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)
	srv.SetGameRule("mob_griefing", false)
	// x=10.5, like the cow's 8.5: no tick may start inside the box grown by
	// the hit margin, since such a tick does not hit the mob (AABB.clip).
	cube := srv.SpawnMob("minecraft:magma_cube", 10.5, 64, 0.5)
	before, _ := srv.MobHealth(cube)

	if srv.LaunchFireball(entity.ThrownSmallFireball, 3.5, 65.0, 0.5, 1, 0, 0, 0, 1) == 0 {
		t.Fatal("could not launch")
	}
	servertest.DriveTargetTicks(srv, 30)

	if after, _ := srv.MobHealth(cube); after != before {
		t.Errorf("health %v -> %v, want unchanged", before, after)
	}
}

// TestAWitherSkullWithersByDifficulty checks the 8 magic-free points of a
// skull and its Wither: 10 seconds on normal, 40 on hard, none on easy.
// vanilla: WitherSkull.onHitEntity
func TestAWitherSkullWithersByDifficulty(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		difficulty uint8
		duration   int32
	}{
		{"easy", rules.DifficultyEasy, 0},
		{"normal", rules.DifficultyNormal, 200},
		{"hard", rules.DifficultyHard, 800},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := flameScene(t)
			srv.SetDifficulty(tc.difficulty)
			srv.SetGameRule("mob_griefing", false)
			cow := srv.SpawnMob("minecraft:cow", 8.5, 64, 0.5)

			if srv.LaunchFireball(entity.ThrownWitherSkull, 3.5, 65.0, 0.5, 1, 0, 0, 0, 1) == 0 {
				t.Fatal("could not launch")
			}
			servertest.DriveTargetTicks(srv, 30)

			duration, amplifier, ok := srv.MobEffectOf(cow, "minecraft:wither")
			if tc.duration == 0 {
				if ok {
					t.Errorf("withered on easy: %d", duration)
				}
				return
			}
			if !ok || duration < tc.duration-40 || duration > tc.duration || amplifier != 1 {
				t.Errorf("wither = %d x%d (%v), want about %d at level 2", duration, amplifier, ok, tc.duration)
			}
		})
	}
}

// TestASmallFireballIgnitesAndHurtsAPlayer checks the 5 unattributed points
// and the burn a fireball leaves on a player.
// vanilla: SmallFireball.onHitEntity
func TestASmallFireballIgnitesAndHurtsAPlayer(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Actor")
	c.MoveTo(6.5, 64, 0.5, 0, 0)
	servertest.WaitFor(t, srv, 200, func() bool { return srv.PlayerPosition("Actor").X == 6.5 })
	srv.StopTick()
	srv.SetGameRule("mob_griefing", false)

	if srv.LaunchFireball(entity.ThrownSmallFireball, 3.5, 65.0, 0.5, 1, 0, 0, 0, 1) == 0 {
		t.Fatal("could not launch")
	}
	servertest.DriveTargetTicks(srv, 30)

	if hp := srv.PlayerHealth("Actor"); hp != 15 {
		t.Errorf("health = %v, want 15", hp)
	}
	if srv.PlayerFire("Actor") <= 0 {
		t.Error("the player is not burning")
	}
}

// TestADangerousWitherSkullBreaksWhatThePlainOneCannot checks the 0.8 cap on
// block resistance of a charged skull: it blows through obsidian, a plain
// one leaves it.
// vanilla: WitherSkull.getBlockExplosionResistance
func TestADangerousWitherSkullBreaksWhatThePlainOneCannot(t *testing.T) {
	t.Parallel()
	for _, dangerous := range []bool{false, true} {
		srv, _ := flameScene(t)
		obsidian := servertest.BlockState(t, "minecraft:obsidian")
		srv.SetBlock(6, 65, 0, obsidian)
		id := srv.LaunchFireball(entity.ThrownWitherSkull, 3.5, 65.5, 0.5, 1, 0, 0, 0, 1)
		if id == 0 {
			t.Fatal("could not launch")
		}
		if dangerous {
			srv.MakeSkullDangerousByID(id)
		}
		servertest.DriveTargetTicks(srv, 30)

		if gone := srv.BlockAt(6, 65, 0) == 0; gone != dangerous {
			t.Errorf("dangerous %v: obsidian gone = %v, want %v", dangerous, gone, dangerous)
		}
	}
}

// TestAWitherSkullHealsItsOwnerWhenItKills checks the 5 points a living
// owner, mob or player, regains when the blow kills the target, mob or
// player, and none when the target lives.
// vanilla: WitherSkull.onHitEntity
func TestAWitherSkullHealsItsOwnerWhenItKills(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                      string
		playerOwner, playerVictim bool
		kills                     bool
	}{
		{"mob owner, mob survives", false, false, false},
		{"mob owner kills a mob", false, false, true},
		{"mob owner kills a player", false, true, true},
		{"player owner kills a mob", true, false, true},
		{"player owner kills a player", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
			c := connect()
			ownerPlayerID := c.JoinAs("Owner")
			c.MoveTo(0.5, 64, 10.5, 0, 0)
			servertest.WaitFor(t, srv, 200, func() bool { return srv.PlayerPosition("Owner").Z == 10.5 })
			if tc.playerVictim {
				v := connect()
				v.JoinAs("Victim")
				v.MoveTo(6.5, 64, 0.5, 0, 0)
				servertest.WaitFor(t, srv, 200, func() bool { return srv.PlayerPosition("Victim").X == 6.5 })
			}
			srv.StopTick()
			srv.SetGameRule("mob_griefing", false)
			srv.SetGameRule("natural_health_regeneration", false)

			ownerID := ownerPlayerID
			if tc.playerOwner {
				srv.SetHealth("Owner", 4)
			} else {
				// A cow, as it neither hunts the players nor heals on its own.
				ownerID = srv.SpawnMob("minecraft:cow", 20.5, 64, 10.5)
				srv.SetMobHealth(ownerID, 4)
			}
			victimHealth := 20.0
			if tc.kills {
				victimHealth = 1
			}
			cow := int32(0)
			if tc.playerVictim {
				srv.SetHealth("Victim", victimHealth)
			} else {
				cow = srv.SpawnMob("minecraft:cow", 8.5, 64, 0.5)
				srv.SetMobHealth(cow, min(victimHealth, 10))
			}
			if srv.LaunchFireball(entity.ThrownWitherSkull, 3.5, 65.0, 0.5, 1, 0, 0, ownerID, 1) == 0 {
				t.Fatal("could not launch")
			}
			servertest.DriveTargetTicks(srv, 30)

			if tc.playerVictim {
				if hp := srv.PlayerHealth("Victim"); hp > 0 {
					t.Fatalf("the victim lived with %v health", hp)
				}
			} else if hp, _ := srv.MobHealth(cow); (hp > 0) == tc.kills {
				t.Fatalf("the cow's health = %v, want it killed %v", hp, tc.kills)
			}
			var got float64
			if tc.playerOwner {
				got = srv.PlayerHealth("Owner")
			} else {
				got, _ = srv.MobHealth(ownerID)
			}
			want := 4.0
			if tc.kills {
				want = 9
			}
			if got != want {
				t.Errorf("owner health = %v, want %v", got, want)
			}
		})
	}
}

// TestALargeFireballPrimesTNTBeforeItsBlast checks that the TNT it strikes is
// primed with a full fuse and still burning 40 ticks later, not already blown
// up by the fireball's own blast.
// vanilla: LargeFireball.onHit
func TestALargeFireballPrimesTNTBeforeItsBlast(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)
	srv.SetBlock(6, 65, 0, servertest.BlockState(t, "minecraft:tnt"))
	srv.SetGameRule("mob_griefing", true)

	fireballWall(t, srv, entity.ThrownLargeFireball, 1)
	servertest.DriveTargetTicks(srv, 40)

	servertest.SoleTNTY(t, srv)
}

// TestHurtingProjectilesFlyOnAfterTheirOwnerIsRemoved checks a breeze's
// charge and a fireball outlive the breeze that owns them: getOwner never
// returns a removed entity, so tick's owner.isRemoved() discard never fires.
// vanilla: AbstractHurtingProjectile.tick, EntityReference.getEntity
func TestHurtingProjectilesFlyOnAfterTheirOwnerIsRemoved(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)
	breeze := srv.SpawnMob("minecraft:breeze", 6.5, 64, 6.5)
	if breeze == 0 {
		t.Fatal("could not spawn the breeze")
	}
	// Still in the air, so neither meets anything.
	charge := srv.LaunchFireball(entity.ThrownBreezeWindCharge, 0.5, 72.5, 3.5, 0, 0, 0, breeze, 1)
	fireball := srv.LaunchFireball(entity.ThrownSmallFireball, 3.5, 72.5, 0.5, 0, 0, 0, breeze, 1)
	if charge == 0 || fireball == 0 {
		t.Fatal("could not launch")
	}
	if !srv.KillMob(breeze) {
		t.Fatal("could not kill the breeze")
	}
	// The body lies for its twenty-tick death animation before removal.
	for range 30 {
		srv.TickMobs(1)
		srv.TickEntities(1)
	}
	if _, _, _, ok := srv.MobPos(breeze); ok {
		t.Fatal("the breeze is still there after its death animation")
	}
	for name, id := range map[string]int32{"breeze wind charge": charge, "small fireball": fireball} {
		if _, _, _, _, _, ok := srv.ThrownTagOf(id); !ok {
			t.Errorf("the %s vanished with its owner, want it flying on", name)
		}
	}
}
