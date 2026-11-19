package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
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
