package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// A flaming arrow whose hurt is refused puts back the burn the body had
// before the arrow lit it.
// vanilla: AbstractArrow.onHitEntity

// TestARefusedFlamingArrowLeavesAnInvulnerableMobUnlit shoots a burning shaft
// at an invulnerable cow: the hurt is refused and the cow is not left alight.
func TestARefusedFlamingArrowLeavesAnInvulnerableMobUnlit(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	id := srv.SpawnMob("minecraft:cow", 2.5, 65, 0.5)
	srv.SetMobInvulnerable(id)
	arrow := srv.ShootArrow(5.5, 65.5, 0.5, -3, 0, 0)
	srv.IgniteArrow(arrow, 100)
	servertest.DriveTargetTicks(srv, 6)

	if got := srv.MobFire(id); got != 0 {
		t.Errorf("the invulnerable cow's fire = %d, want 0", got)
	}
}

// TestARefusedFlamingArrowKeepsTheBurnAMobAlreadyHad lights a cow for 40
// ticks and shoots a burning shaft at it while invulnerable: the burn stays
// at what it was, not the arrow's five seconds.
func TestARefusedFlamingArrowKeepsTheBurnAMobAlreadyHad(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	id := srv.SpawnMob("minecraft:cow", 2.5, 65, 0.5)
	srv.SetMobInvulnerable(id)
	srv.SetMobFire(id, 40)
	arrow := srv.ShootArrow(5.5, 65.5, 0.5, -3, 0, 0)
	srv.IgniteArrow(arrow, 100)
	servertest.DriveTargetTicks(srv, 6)

	if got := srv.MobFire(id); got <= 0 || got > 40 {
		t.Errorf("the cow's fire = %d, want at most the 40 it had", got)
	}
}

// TestAFlamingArrowInTheHurtCooldownLeavesAMobUnlit hits a cow with a plain
// fast arrow, then a burning slow one inside the cooldown that the first
// blow's damage outweighs: the second is refused and lights nothing.
func TestAFlamingArrowInTheHurtCooldownLeavesAMobUnlit(t *testing.T) {
	t.Parallel()
	srv, _ := flameScene(t)

	id := srv.SpawnMob("minecraft:cow", 2.5, 65, 0.5)
	srv.ShootArrow(5.5, 65.5, 0.5, -3, 0, 0)
	servertest.DriveTargetTicks(srv, 4)
	arrow := srv.ShootArrow(5.5, 65.5, 0.5, -1, 0, 0)
	srv.IgniteArrow(arrow, 100)
	servertest.DriveTargetTicks(srv, 8)

	if got := srv.MobFire(id); got != 0 {
		t.Errorf("the cow's fire = %d after a refused burning arrow, want 0", got)
	}
}

// TestAFlamingArrowInTheHurtCooldownLeavesAPlayerUnlit hits the actor with a
// blow that opens the hurt window, then flies a burning shaft into it: the
// shaft is refused and the actor is not left alight.
func TestAFlamingArrowInTheHurtCooldownLeavesAPlayerUnlit(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))
	srv.StopTick()
	srv.SetDifficulty(2)

	pos := srv.PlayerPosition("Actor")
	before := srv.PlayerFire("Actor")
	if !srv.Hurt("Actor", 10) {
		t.Fatal("the opening blow did not land")
	}
	arrow := srv.ShootArrow(pos.X+3, pos.Y+1, pos.Z, -1, 0, 0)
	srv.IgniteArrow(arrow, 100)
	servertest.DriveTargetTicks(srv, 8)

	if got := srv.PlayerFire("Actor"); got != before {
		t.Errorf("the actor's fire = %d after a refused burning arrow, want the %d it had", got, before)
	}
}
