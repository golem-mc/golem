package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: FireworkRocketEntity.tick, onHitEntity
func TestACrossbowRocketBurstsOnAnArmorStandInItsPath(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Launcher", func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	stand := srv.SpawnArmorStand(3.5, 64, 3.5)
	rocket := srv.SpawnFireworkStackAtAngle(servertest.FireworkStack(0, 1), 0.5, 65, 3.5, 1, 0, 0, 100)
	// At one block per tick the third tick ends at the stand's x = 3.5.
	if events := readFireworkTraffic(c, srv, 2).eventsOf(rocket.ID); len(events) != 0 {
		t.Fatalf("rocket burst before reaching the stand: %v", events)
	}
	traffic := readFireworkTraffic(c, srv, 1)
	if events := traffic.eventsOf(rocket.ID); len(events) != 1 || events[0].Status != protocol.EntityStatusFireworkBurst {
		t.Errorf("rocket events at x = 3.5: %v, want event 17", events)
	}
	if !traffic.removedOnce(stand) || srv.ArmorStandCount() != 0 {
		t.Error("the stand was not broken by the rocket's burst")
	}
}

// vanilla: Projectile.canHitEntity, EnderDragonPart.isPickable
func TestACrossbowRocketHitsTheEnderDragon(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BowTestConfig(), "Launcher", servertest.OpsListing(t, "Launcher"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	c.MoveTo(0.5, 128, 0.5, 0, 0)
	c.ChatCommand(`summon minecraft:ender_dragon 8.0 128.0 8.0 {DragonPhase:6,Rotation:[0f,0f]}`)
	c.Settle()
	if ids := srv.MobIDsOfKind("minecraft:ender_dragon"); len(ids) != 1 {
		t.Fatalf("dragons = %v, want one", ids)
	}
	rocket := srv.SpawnFireworkAtAngle(0.5, 129, 7.5, 1, 0, 0, 100)
	if events := readFireworkTraffic(c, srv, 3).eventsOf(rocket.ID); len(events) != 0 {
		t.Fatalf("rocket burst before reaching the dragon: %v", events)
	}
	// The body spans x = 5.5..10.5; six ticks reach x = 6.5, inside it.
	traffic := readFireworkTraffic(c, srv, 3)
	if events := traffic.eventsOf(rocket.ID); len(events) != 1 || events[0].Status != protocol.EntityStatusFireworkBurst {
		t.Errorf("rocket events before passing the dragon: %v, want event 17", events)
	}
	if !traffic.removedOnce(rocket.ID) {
		t.Error("the rocket flew through the dragon's body")
	}
}
