package plugins_test

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// vanilla: Projectile.onHit, AbstractArrow.onHitEntity
func TestPluginProjectileHitEvent(t *testing.T) {
	cancel, asked := true, 0
	srv, c := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.ProjectileHitEvent) {
			if e.Hit.Entity == nil {
				return // the block hit the flight ends on
			}
			asked++
			_ = e.Hit.Entity.Type() // reads the body: no lock may be held
			if _, ok := e.Projectile.Shooter(); ok {
				t.Error("a dispensed arrow has no shooter")
			}
			e.SetCancelled(cancel)
		})
	})
	id := srv.SpawnMob("minecraft:cow", 4.5, 64, 4.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.SetMobNoAI(id)
	srv.ShootArrow(10.5, 65, 4.5, -2, 0, 0)
	for range 12 {
		srv.RunTick()
	}
	if asked == 0 {
		t.Fatal("the hit was not put to listeners")
	}
	if got := mobHealth(t, srv, id); got != 10 {
		t.Errorf("a cancelled hit took health: %v", got)
	}
	cancel = false
	srv.ShootArrow(10.5, 65, 4.5, -2, 0, 0)
	ticksUntil(t, srv, "the allowed hit", func() bool { return mobHealth(t, srv, id) < 10 })
}

// vanilla: Projectile.onHit, ThrowableProjectile.onHitEntity
func TestPluginProjectileHitEventThrown(t *testing.T) {
	cancel, asked := true, 0
	srv, c := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.ProjectileHitEvent) {
			if e.Hit.Entity == nil {
				return
			}
			asked++
			_ = e.Hit.Entity.Type()
			e.SetCancelled(cancel)
		})
	})
	id := srv.SpawnMob("minecraft:cow", 4.5, 64, 4.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.SetMobNoAI(id)
	srv.ThrowThrown(entity.ThrownSnowball, 10.5, 65, 4.5, 90, 0, 0, 1)
	for range 12 {
		srv.RunTick()
	}
	if asked != 1 {
		t.Fatalf("a cancelled hit was asked %d times, want 1", asked)
	}
	cancel = false
	srv.ThrowThrown(entity.ThrownSnowball, 10.5, 65, 4.5, 90, 0, 0, 1)
	for range 12 {
		srv.RunTick()
	}
	if asked != 2 {
		t.Errorf("asked %d times, want 2", asked)
	}
	if n := len(srv.ThrownInDimension("minecraft:overworld")); n != 0 {
		t.Errorf("%d thrown bodies left after the allowed hit", n)
	}
}

// A step a listener holds is undone, rolls included: the critical bonus an
// arrow deals is the one its seed gives with no listener.
// vanilla: Projectile.onHit, AbstractArrow.getBaseDamage
func TestPluginProjectileHitHeldStepKeepsRolls(t *testing.T) {
	// Speed 1.5 deals ceil(1.5*2) = 3, and a critical adds Intn(3/2+2); pick
	// a seed whose first two rolls differ so a replay that drew twice shows.
	var seed int64
	for seed = 1; ; seed++ {
		r := rand.New(rand.NewSource(seed))
		if r.Intn(3) != r.Intn(3) {
			break
		}
	}
	bonus := rand.New(rand.NewSource(seed)).Intn(3)
	asked := false
	srv := servertest.NewTestServerWithPlugins(t, servertest.SurvivalTestConfig(), []plugin.Registration{{Name: "hits", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.ProjectileHitEvent) {
			if e.Hit.Entity != nil {
				asked = true // allowed: the step is undone and replayed with the hit let through
			}
		})
		return nil
	}}}})
	c := servertest.DialServer(t, srv)
	c.Join("Victim")
	c.MoveToGround(0.5, 64, 6.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob("minecraft:cow", 4.5, 64, 4.5)
	srv.SetMobNoAI(id)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.SeedCritArrow(srv.ShootArrow(1.5, 65, 4.5, 1.5, 0, 0), seed)
	servertest.RunTicks(t, srv, 8)
	if !asked {
		t.Fatal("the hit was not put to listeners")
	}
	if got, want := mobHealth(t, srv, id), float64(10-3-bonus); got != want {
		t.Errorf("health after the hit = %v, want %v", got, want)
	}
}
