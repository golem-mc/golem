package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The hoglin and the zoglin over the wire: the charge, the throw, the flight
// from warped fungus and the zombification.
// vanilla: Hoglin, HoglinAi, Zoglin

const (
	hoglin = "minecraft:hoglin"
	zoglin = "minecraft:zoglin"
)

func hoglinWorld(t *testing.T) (int32, *server.Server) {
	t.Helper()
	_, id, srv := polarWorld(t)
	srv.SetDifficulty(2)
	return id, srv
}

func mobKindCount(srv *server.Server, kind string) int {
	n := 0
	for _, m := range srv.Mobs() {
		if m.Kind == kind {
			n++
		}
	}
	return n
}

func TestAHoglinChargesAPlayerAndThrowsThem(t *testing.T) {
	t.Parallel()
	playerID, srv := hoglinWorld(t)
	h := srv.SpawnMob(hoglin, 7.5, 64, 0.5)
	servertest.UntilTicks(t, srv, 200, "the hoglin's target", func() bool { return srv.MobTargetID(h) == playerID })
	servertest.UntilTicks(t, srv, 400, "the hoglin's blow", func() bool { return srv.PlayerHealth("Slayer") < 20 })
	lost := 20 - srv.PlayerHealth("Slayer")
	// vanilla: HoglinBase.hurtAndThrowTarget, 6 / 2 + nextInt(6)
	if lost < 3 || lost > 8 {
		t.Errorf("the blow cost %v health, want 3 to 8", lost)
	}
	vx, vy, vz := srv.PlayerVelocity("Slayer")
	hx, _, hz, _ := srv.MobPos(h)
	at := srv.PlayerPosition("Slayer")
	dx, dz := at.X-hx, at.Z-hz
	distance := math.Hypot(dx, dz)
	// vanilla: HoglinBase.throwTarget passes nextInt(21)-10 to Vec3.yRot
	// as radians, so the throw can oppose the ordinary 0.4 knockback.
	throw := math.Hypot(vx-0.4*dx/distance, vz-0.4*dz/distance)
	if throw < 0.2-1e-6 || throw > 0.7+1e-6 {
		t.Errorf("the horizontal throw is %v, want 0.2 to 0.7", throw)
	}
	// The plain knockback lifts a grounded body by exactly 0.4; the throw
	// adds up to 0.5 on top.
	if vy < 0.4 || vy > 0.9 {
		t.Errorf("the vertical velocity is %v, want 0.4 to 0.9", vy)
	}
}

func TestAHoglinNearWarpedFungusFleesInsteadOfAttacking(t *testing.T) {
	t.Parallel()
	_, srv := hoglinWorld(t)
	fungus := servertest.MustState(t, "minecraft:warped_fungus", nil)
	if !srv.SetBlock(9, 64, 0, fungus) {
		t.Fatal("the fungus was not placed")
	}
	h := srv.SpawnMob(hoglin, 8.5, 64, 0.5)
	distance := func() float64 {
		for _, m := range srv.Mobs() {
			if m.ID == h {
				return math.Hypot(m.X-9.5, m.Z-0.5)
			}
		}
		t.Fatal("the hoglin is gone")
		return 0
	}
	// Within eight blocks of the fungus the hoglin neither hunts nor stays.
	// vanilla: HoglinAi.initIdleActivity, BecomePassiveIfMemoryPresent
	for range 4 {
		servertest.RunTicks(t, srv, 10)
		if got := srv.MobTargetID(h); got != 0 {
			t.Fatalf("a hoglin beside warped fungus targets %d, want nobody", got)
		}
	}
	if hp := srv.PlayerHealth("Slayer"); hp < 20 {
		t.Errorf("the player lost health (%v) to a hoglin that should be repelled", hp)
	}
	if d := distance(); d < 3 {
		t.Errorf("the hoglin is %v blocks from the fungus after 40 ticks, want it walking away", d)
	}
}

func TestAHoglinTurnsIntoAZoglinInTheOverworldAfterThreeHundredTicks(t *testing.T) {
	t.Parallel()
	_, srv := hoglinWorld(t)
	srv.SpawnMob(hoglin, 24.5, 64, 24.5)
	servertest.RunTicks(t, srv, 300)
	if mobKindCount(srv, hoglin) != 1 || mobKindCount(srv, zoglin) != 0 {
		t.Fatalf("the hoglin changed within its 300 ticks: %v hoglins, %v zoglins", mobKindCount(srv, hoglin), mobKindCount(srv, zoglin))
	}
	servertest.RunTicks(t, srv, 1)
	if mobKindCount(srv, hoglin) != 0 || mobKindCount(srv, zoglin) != 1 {
		t.Errorf("after 301 ticks: %v hoglins, %v zoglins, want 0 and 1", mobKindCount(srv, hoglin), mobKindCount(srv, zoglin))
	}
}

// A blow on one adult hoglin sends the adult hoglins that see it after the
// attacker.
// vanilla: HoglinAi.wasHurtBy, broadcastAttackTarget
func TestHittingAHoglinSendsTheAdultsThatSeeItAfterThePlayer(t *testing.T) {
	t.Parallel()
	c, playerID, srv := hoglinWorld2(t)
	struck := srv.SpawnMob(hoglin, 4.5, 64, 0.5)
	a := srv.SpawnMob(hoglin, 6.5, 64, 2.5)
	b := srv.SpawnMob(hoglin, 6.5, 64, -1.5)
	c.Settle()
	c.Attack(struck)
	servertest.UntilTicks(t, srv, 40, "both other hoglins' target", func() bool {
		return srv.MobTargetID(a) == playerID && srv.MobTargetID(b) == playerID
	})
}

func hoglinWorld2(t *testing.T) (*servertest.Client, int32, *server.Server) {
	t.Helper()
	c, id, srv := polarWorld(t)
	srv.SetDifficulty(2)
	return c, id, srv
}
