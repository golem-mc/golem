package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
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

// fireballChunks is a server with its loop stopped and only the chunks
// holding the given block columns loaded, for a fireball's entity ticks.
func fireballChunks(t *testing.T, columns ...[2]int32) *server.Server {
	t.Helper()
	srv := servertest.NewTestServer(t, servertest.BlockTestConfig())
	srv.StopTick()
	for _, c := range columns {
		t.Cleanup(srv.HoldChunk(c[0], c[1]))
	}
	srv.SetGameRule("mob_griefing", false)
	return srv
}

// scale16 is the tag of a mob sixteen times its size.
var scale16 = nbt.Compound{"attributes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
	nbt.Compound{"id": nbt.String("minecraft:scale"), "base": nbt.Double(16)}}}}

// scaledCowFlight launches a small fireball along +x at z=fz, one tick short
// of a scale-16 cow centred at (24.5, 64, cz), and returns its health before
// and after that tick.
func scaledCowFlight(t *testing.T, fz, cz float64) (before, after float64) {
	t.Helper()
	srv := fireballChunks(t, [2]int32{17, int32(fz)}, [2]int32{24, int32(cz)})
	cow := srv.SpawnMobTagged("minecraft:cow", 24.5, 64, cz, scale16)
	if cow == 0 {
		t.Fatal("the cow was refused")
	}
	before, _ = srv.MobHealth(cow)
	if srv.LaunchFireball(entity.ThrownSmallFireball, 17.25, 65.0, fz, 1, 0, 0, 0, 1) == 0 {
		t.Fatal("could not launch")
	}
	srv.TickEntities(1)
	after, _ = srv.MobHealth(cow)
	return before, after
}

// TestAFireballHitsAScaledMobWhoseCentreIsOffTheFlight checks the box the
// flight is tested against is the mob's own: a scale-16 cow six blocks off
// the line, in the same chunk, is still struck.
// vanilla: EntitySection.getEntities, LivingEntity.getBoundingBox
func TestAFireballHitsAScaledMobWhoseCentreIsOffTheFlight(t *testing.T) {
	t.Parallel()
	if before, after := scaledCowFlight(t, 0.5, 6.5); after >= before {
		t.Errorf("health %v -> %v, want the fireball to hurt it", before, after)
	}
}

// TestAFireballSkipsAScaledMobFiledBeyondTheSectionPadding checks a mob whose
// centre is in the next chunk, past the two blocks of padding around the
// flight, is not offered even though its box overlaps the flight.
// vanilla: EntitySectionStorage.forEachAccessibleNonEmptySection
func TestAFireballSkipsAScaledMobFiledBeyondTheSectionPadding(t *testing.T) {
	t.Parallel()
	if before, after := scaledCowFlight(t, 12.8, 16.5); after != before {
		t.Errorf("health %v -> %v, want the cow skipped", before, after)
	}
}

// TestAFireballReachesAScaledMobFiledInTheChunkItsOwnBoxPadsInto checks the
// query uses the fireball's own 0.3125-wide box, so the padded bound just
// reaches the cow's chunk.
// vanilla: ProjectileUtil.getEntityHitResult, EntitySectionStorage.forEachAccessibleNonEmptySection
func TestAFireballReachesAScaledMobFiledInTheChunkItsOwnBoxPadsInto(t *testing.T) {
	t.Parallel()
	if before, after := scaledCowFlight(t, 12.86, 16.5); after >= before {
		t.Errorf("health %v -> %v, want the fireball to hurt it", before, after)
	}
}

// TestAFireballSkipsAScaledMobFiledInASectionBelowThePadding checks a mob
// whose feet are in a lower 16-block section than the padded flight is not
// offered even though its box overlaps the flight.
// vanilla: EntitySectionStorage.forEachAccessibleNonEmptySection
func TestAFireballSkipsAScaledMobFiledInASectionBelowThePadding(t *testing.T) {
	t.Parallel()
	srv := fireballChunks(t, [2]int32{17, 0}, [2]int32{24, 0})
	cow := srv.SpawnMobTagged("minecraft:cow", 24.5, 64, 0.5, scale16)
	if cow == 0 {
		t.Fatal("the cow was refused")
	}
	before, _ := srv.MobHealth(cow)
	// Cow box reaches y 64..86; the flight at y 84 pads down to 82, section 5.
	if srv.LaunchFireball(entity.ThrownSmallFireball, 17.25, 84.0, 0.5, 1, 0, 0, 0, 1) == 0 {
		t.Fatal("could not launch")
	}
	srv.TickEntities(1)
	if after, _ := srv.MobHealth(cow); after != before {
		t.Errorf("health %v -> %v, want the cow skipped", before, after)
	}
}

// TestAFireballWithSavedAccelerationReachesAMobInOneTick checks the sweep
// is queried over the velocity after the acceleration is applied.
// vanilla: AbstractHurtingProjectile.tick, ProjectileUtil.getEntityHitResult
func TestAFireballWithSavedAccelerationReachesAMobInOneTick(t *testing.T) {
	t.Parallel()
	srv := fireballChunks(t, [2]int32{0, 0})
	cow := srv.SpawnMob("minecraft:cow", 12.5, 64, 0.5)
	before, _ := srv.MobHealth(cow)

	srv.LaunchAcceleratedFireball(entity.ThrownSmallFireball, 3.5, 65.0, 0.5, 1, 0, 0, 10)
	srv.TickEntities(1)

	if after, _ := srv.MobHealth(cow); after >= before {
		t.Errorf("health %v -> %v, want the fireball to hurt it", before, after)
	}
}

// TestAFireballSparesItsOwnersFellowPassengerFiledOutOfReach checks the
// thrower stays excluded while the moved flight box still touches a mob in
// the thrower's boat whose centre is in a chunk the flight's own query does
// not reach: a scale-16 cow seated with the thrower is never struck.
// vanilla: Projectile.isOutsideOwnerCollisionRange, Projectile.checkLeftOwner
func TestAFireballSparesItsOwnersFellowPassengerFiledOutOfReach(t *testing.T) {
	t.Parallel()
	srv := fireballChunks(t, [2]int32{16, 12}, [2]int32{24, 16})
	boat := srv.SpawnBoat(24.5, 64, 16.5, "minecraft:oak_boat")
	owner := srv.SpawnMob("minecraft:cow", 24.5, 64, 16.5)
	cow := srv.SpawnMobTagged("minecraft:cow", 24.5, 64, 16.5, scale16)
	if boat == 0 || owner == 0 || cow == 0 || !srv.MountMobOnBoat(owner, boat) || !srv.MountMobOnBoat(cow, boat) {
		t.Fatal("could not seat the thrower and the cow")
	}
	before, _ := srv.MobHealth(cow)
	// From outside the cow's box (x 17.3..31.7) into it: the moved flight
	// box touches the cow from the first tick, while the query reaches the
	// cow's chunk (z 16..) only from the fourth, still short of the box.
	if srv.LaunchFireball(entity.ThrownSmallFireball, 16.1, 65.0, 12.0, 1, 0, 1, owner, 1) == 0 {
		t.Fatal("could not launch")
	}
	srv.TickEntities(10)
	if after, _ := srv.MobHealth(cow); after != before {
		t.Errorf("health %v -> %v, want the thrower's fellow passenger spared", before, after)
	}
}
