package combat

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// TestAPunchedLargeFireballFliesAlongTheLookAndChangesOwner checks the blow
// turns the fireball to the player's look, resets its acceleration and
// makes the player its owner.
// vanilla: Player.deflectProjectile, ProjectileDeflection.AIM_DEFLECT
func TestAPunchedLargeFireballFliesAlongTheLookAndChangesOwner(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)
	c.MoveTo(2.5, 64, 0.5, 90, 0)
	c.Settle()
	id := srv.LaunchFireball(entity.ThrownLargeFireball, 3.5, 64.5, 0.5, 1, 0, 0, 0, 1)
	if id == 0 {
		t.Fatal("could not launch")
	}

	c.Attack(id)
	c.Settle()

	_, owner, vx, vy, vz, ok := srv.ThrownTagOf(id)
	if !ok {
		t.Fatal("the fireball is gone")
	}
	if math.Abs(vx+1) > 1e-6 || math.Abs(vy) > 1e-6 || math.Abs(vz) > 1e-6 {
		t.Errorf("the fireball flies (%g, %g, %g), want the player's look (-1, 0, 0)", vx, vy, vz)
	}
	if owner == 0 {
		t.Error("the fireball has no owner, want the player who struck it")
	}
}

// TestAPunchedSmallFireballIsNotTurned checks small fireballs are not
// redirectable.
// vanilla: EntityTypeTags.REDIRECTABLE_PROJECTILE
func TestAPunchedSmallFireballIsNotTurned(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)
	c.MoveTo(2.5, 64, 0.5, 90, 0)
	c.Settle()
	id := srv.LaunchFireball(entity.ThrownSmallFireball, 3.5, 64.5, 0.5, 1, 0, 0, 0, 1)

	c.Attack(id)
	c.Settle()

	_, owner, vx, _, _, _ := srv.ThrownTagOf(id)
	if vx <= 0 || owner != 0 {
		t.Errorf("the small fireball flies %g owned by %d, want it untouched", vx, owner)
	}
}

// TestAFireballFarOutOfReachIsNotTurned checks the reach the blow verifies.
func TestAFireballFarOutOfReachIsNotTurned(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)
	c.MoveTo(2.5, 64, 0.5, 90, 0)
	c.Settle()
	id := srv.LaunchFireball(entity.ThrownLargeFireball, 20.5, 64.5, 0.5, 1, 0, 0, 0, 1)

	c.Attack(id)
	c.Settle()

	_, owner, vx, _, _, _ := srv.ThrownTagOf(id)
	if vx <= 0 || owner != 0 {
		t.Errorf("the far fireball flies %g owned by %d, want it untouched", vx, owner)
	}
}

// TestAPunchedFireballPlaysTheNoDamageSound checks the blow sounds as one
// that dealt no damage.
// vanilla: Player.deflectProjectile
func TestAPunchedFireballPlaysTheNoDamageSound(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)
	c.MoveTo(2.5, 64, 0.5, 90, 0)
	c.Settle()
	id := srv.LaunchFireball(entity.ThrownLargeFireball, 3.5, 64.5, 0.5, 1, 0, 0, 0, 1)

	c.Attack(id)

	servertest.ExpectSoundEvent(t, c, "minecraft:entity.player.attack.nodamage")
}

// TestAFireballAtCreativeReachIsTurnedOnlyForCreative checks a box 7 blocks
// from the eye is inside the creative verified reach (3 + 2 + 3 buffer) and
// outside the survival one (3 + 3).
// vanilla: Player.deflectProjectile
func TestAFireballAtCreativeReachIsTurnedOnlyForCreative(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		cfg       *config.Config
		turned    bool
		wantOwner string
	}{
		{"creative", servertest.BlockTestConfig(), true, "set"},
		{"survival", servertest.SurvivalTestConfig(), false, "unset"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, tc.cfg, "Actor", servertest.OpsListing(t, "Actor"))
			srv.StopTick()
			c.MoveTo(2.5, 64, 0.5, 90, 0)
			c.Settle()
			id := srv.LaunchFireball(entity.ThrownLargeFireball, 10.0, 64.5, 0.5, 1, 0, 0, 0, 1)

			c.Attack(id)
			c.Settle()

			_, owner, vx, _, _, _ := srv.ThrownTagOf(id)
			if turned := vx < 0 && owner != 0; turned != tc.turned {
				t.Errorf("fireball vx %g owner %d: turned = %v, want %v", vx, owner, turned, tc.turned)
			}
		})
	}
}

// thrownOfKind is the entity ID of the first thrown body of the kind in the
// overworld, 0 when there is none.
func thrownOfKind(srv *server.Server, kind string) int32 {
	for _, it := range srv.ThrownInDimension("minecraft:overworld") {
		if it.Kind == kind {
			return it.ID
		}
	}
	return 0
}

// nextThrownMotion reads the next Set Entity Motion naming the body.
func nextThrownMotion(t *testing.T, c *servertest.Client, id int32) (vx, vy, vz float64) {
	t.Helper()
	for pid, data := range c.Packets(servertest.ExpectTimeout) {
		if pid != protocol.IDPlaySetEntityMotion {
			continue
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() == id {
			return r.ReadLpVec3()
		}
	}
	t.Fatalf("no motion was sent for body %d", id)
	return 0, 0, 0
}

// ownerOf is the Owner a /data get entity reads off the first body of the
// type.
func ownerOf(t *testing.T, srv *server.Server, typ string) string {
	t.Helper()
	for _, text := range srv.RunLine("data get entity @e[type=" + typ + ",limit=1] Owner") {
		if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
			return text.With[1].Plain()
		}
	}
	return ""
}

// actorOwner is the Owner /data prints for the player Actor.
func actorOwner() string {
	w := servertest.UuidWords(protocol.OfflineUUID("Actor"))
	return fmt.Sprintf("[I; %d, %d, %d, %d]", w[0], w[1], w[2], w[3])
}

// upWest is the look of yaw 90, pitch -30: west and up.
var upWest = [3]float64{-math.Cos(math.Pi / 6), 0.5, 0}

// TestAPunchedWindChargeFliesAlongTheLook checks a breeze's charge, which
// has no fresh-charge guard, turns at once to the player's look.
// vanilla: Player.deflectProjectile, EntityTypeTags.REDIRECTABLE_PROJECTILE
func TestAPunchedWindChargeFliesAlongTheLook(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)
	c.MoveTo(2.5, 64, 0.5, 90, -30)
	c.Settle()
	srv.RunLine("summon minecraft:breeze_wind_charge 4.5 64.8 0.5 {Motion:[-0.05d,0d,0d]}")
	id := thrownOfKind(srv, "minecraft:breeze_wind_charge")
	if id == 0 {
		t.Fatal("no breeze wind charge was summoned")
	}
	c.Settle()

	c.Attack(id)

	vx, vy, vz := nextThrownMotion(t, c, id)
	if math.Abs(vx-upWest[0]) > 0.01 || math.Abs(vy-upWest[1]) > 0.01 || math.Abs(vz) > 0.01 {
		t.Errorf("the charge flies (%g, %g, %g), want the look (%g, %g, 0)", vx, vy, vz, upWest[0], upWest[1])
	}
	// The motion's bundle carries the 0.1 the server now accelerates it by.
	// vanilla: ServerEntity.sendChanges
	power := -1.0
	for pid, data := range c.Packets(servertest.ExpectTimeout) {
		if pid != protocol.IDPlayProjectilePower {
			continue
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() == id {
			power = r.ReadF64()
			break
		}
	}
	if power != 0.1 {
		t.Errorf("the deflected charge's Projectile Power = %g, want 0.1", power)
	}
}

// TestAShotBesideAFireballLeavesItUntouched flies the player's arrow past a
// hovering fireball by 0.6 blocks, outside the fireball's box.
// vanilla: ProjectileUtil.getEntityHitResult
func TestAShotBesideAFireballLeavesItUntouched(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)
	c.MoveTo(2.5, 64, 0.5, 90, -30)
	c.Settle()
	id := srv.LaunchFireball(entity.ThrownLargeFireball, 8.5, 64.5, 0.5, 0, 0, 0, 0, 1)
	if id == 0 {
		t.Fatal("could not launch")
	}
	// The fireball's box spans 8..9 by 64..65; the arrow's passes at z 1.7
	// to 1.1 clear of it.
	srv.RunLine(fmt.Sprintf("summon minecraft:arrow 5.5 64.5 1.7 {Owner:%s,Motion:[1d,0d,0d],NoGravity:1b}", actorOwner()))
	for range 8 {
		servertest.DriveTargetTicks(srv, 1)
	}
	if _, owner, vx, _, _, ok := srv.ThrownTagOf(id); !ok || owner != 0 || vx != 0 {
		t.Errorf("the fireball (vx %g, owner %d, present %v) was touched, want untouched", vx, owner, ok)
	}
}

// TestAFreshWindChargeIsNotTurned checks a player's wind charge refuses a
// blow in its first five ticks.
// vanilla: WindCharge.noDeflectTicks
func TestAFreshWindChargeIsNotTurned(t *testing.T) {
	t.Parallel()
	srv, c := flameScene(t)
	c.MoveTo(2.5, 64, 0.5, 90, -30)
	c.Settle()
	srv.RunLine("summon minecraft:wind_charge 4.5 64.8 0.5 {Motion:[-0.05d,0d,0d],acceleration_power:0d}")
	id := thrownOfKind(srv, "minecraft:wind_charge")
	if id == 0 {
		t.Fatal("no wind charge was summoned")
	}
	servertest.DriveTargetTicks(srv, 1)
	c.Settle()

	c.Attack(id)
	c.Settle()

	_, owner, vx, vy, vz, ok := srv.ThrownTagOf(id)
	if !ok {
		t.Fatal("the charge is gone")
	}
	if math.Abs(vx+0.05) > 1e-9 || vy != 0 || vz != 0 || owner != 0 {
		t.Errorf("the charge flies (%g, %g, %g) owned by %d, want (-0.05, 0, 0) and no owner", vx, vy, vz, owner)
	}

	// Five ticks old, it turns.
	servertest.DriveTargetTicks(srv, 4)
	c.Settle()
	c.Attack(id)
	c.Settle()
	if _, owner, vx, _, _, _ = srv.ThrownTagOf(id); vx > -0.5 || owner == 0 {
		t.Errorf("the five-tick charge flies %g owned by %d, want turned to the look and owned", vx, owner)
	}
}

// TestAnArrowTurnsALargeFireball shoots the player's arrow into a hovering
// fireball: it turns along the player's look and becomes theirs.
// vanilla: Projectile.onRedirectProjectile
func TestAnArrowTurnsALargeFireball(t *testing.T) {
	t.Parallel()
	shotTurnsFireball(t, "arrow")
}

// TestASnowballTurnsALargeFireball is the arrow's case for a thrown body.
// vanilla: Projectile.onRedirectProjectile
func TestASnowballTurnsALargeFireball(t *testing.T) {
	t.Parallel()
	shotTurnsFireball(t, "snowball")
}

// shotTurnsFireball flies the player's shot of the type into a hovering
// fireball and checks the fireball turns along the player's look and
// becomes theirs.
func shotTurnsFireball(t *testing.T, shot string) {
	t.Helper()
	srv, c := flameScene(t)
	c.MoveTo(2.5, 64, 0.5, 90, -30)
	c.Settle()
	id := srv.LaunchFireball(entity.ThrownLargeFireball, 8.5, 64.5, 0.5, 0, 0, 0, 0, 1)
	if id == 0 {
		t.Fatal("could not launch")
	}
	srv.RunLine(fmt.Sprintf("summon minecraft:%s 5.5 65 0.5 {Owner:%s,Motion:[1d,0d,0d],NoGravity:1b}", shot, actorOwner()))

	var vx, vy, vz float64
	for range 5 {
		servertest.DriveTargetTicks(srv, 1)
		var owner int32
		if _, owner, vx, vy, vz, _ = srv.ThrownTagOf(id); owner != 0 {
			break
		}
	}
	speed := math.Sqrt(vx*vx + vy*vy + vz*vz)
	if speed == 0 || math.Abs(vx/speed-upWest[0]) > 1e-6 || math.Abs(vy/speed-upWest[1]) > 1e-6 || math.Abs(vz) > 1e-6 {
		t.Errorf("the fireball flies (%g, %g, %g), want along the look (%g, %g, 0)", vx, vy, vz, upWest[0], upWest[1])
	}
	if got, want := ownerOf(t, srv, "fireball"), actorOwner(); got != want {
		t.Errorf("the fireball's Owner = %q, want the shooter %q", got, want)
	}
}

// TestABlastMakesItsIgniterOwnTheFireball checks a blast hands a
// redirectable body in reach to the blast's cause.
// vanilla: ServerExplosion.hurtEntities
func TestABlastMakesItsIgniterOwnTheFireball(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)
	if srv.LaunchFireball(entity.ThrownLargeFireball, 8.5, 65.5, 6.5, 0, 0, 0, 0, 1) == 0 {
		t.Fatal("could not launch")
	}
	if srv.SpawnPlayerTNT("Actor", 8.5, 64, 4.5, 1) == 0 {
		t.Fatal("could not light the TNT")
	}
	for range 5 {
		if srv.TNTCount() == 0 {
			break
		}
		servertest.DriveTargetTicks(srv, 1)
	}
	if srv.TNTCount() != 0 {
		t.Fatal("the TNT never went off")
	}

	if got, want := ownerOf(t, srv, "fireball"), actorOwner(); got != want {
		t.Errorf("the fireball's Owner = %q, want the igniter %q", got, want)
	}
}

// TestASummonedWindChargeAcceleratesByDefault summons a wind charge without
// acceleration_power: it reads the default 0.1 and gains 0.1 a tick along its
// heading, while one summoned with 0 keeps its speed.
// vanilla: AbstractHurtingProjectile.readAdditionalSaveData, AbstractHurtingProjectile.tick, AbstractWindCharge.getInertia
func TestASummonedWindChargeAcceleratesByDefault(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		field     string
		wantPower float64
		wantGain  float64
	}{
		{"", 0.1, 0.1},
		{",acceleration_power:0d", 0, 0},
	} {
		srv, _ := flameScene(t)
		srv.RunLine("summon minecraft:wind_charge 0.5 70.5 8.5 {Motion:[0.1d,0d,0d]" + tc.field + "}")
		id := thrownOfKind(srv, "minecraft:wind_charge")
		if id == 0 {
			t.Fatalf("%q: no wind charge was summoned", tc.field)
		}
		tag, _, vx, _, _, _ := srv.ThrownTagOf(id)
		if power, ok := tag["acceleration_power"].(nbt.Double); !ok || math.Abs(float64(power)-tc.wantPower) > 1e-9 {
			t.Errorf("%q: acceleration_power = %v, want %g", tc.field, tag["acceleration_power"], tc.wantPower)
		}
		for tick := 1; tick <= 3; tick++ {
			servertest.DriveTargetTicks(srv, 1)
			_, _, next, vy, vz, ok := srv.ThrownTagOf(id)
			if !ok {
				t.Fatalf("%q: the charge is gone after tick %d", tc.field, tick)
			}
			if math.Abs(next-vx-tc.wantGain) > 1e-9 || vy != 0 || vz != 0 {
				t.Errorf("%q tick %d: motion (%g, %g, %g) from vx %g, want vx gaining %g", tc.field, tick, next, vy, vz, vx, tc.wantGain)
			}
			vx = next
		}
	}
}
