package combat

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
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

// TestAnArrowTurnsALargeFireball shoots the player's arrow into a hovering
// fireball: it turns along the player's look and becomes theirs.
// vanilla: Projectile.onRedirectProjectile
func TestAnArrowTurnsALargeFireball(t *testing.T) {
	t.Parallel()
	shotTurnsFireball(t, "arrow")
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
