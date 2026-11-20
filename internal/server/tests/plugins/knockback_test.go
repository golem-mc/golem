package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ServerExplosion.explode, WindCharge
func TestPluginEntityKnockbackEvent(t *testing.T) {
	t.Parallel()
	asked, cancel := 0, true
	srv, c := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityKnockbackEvent) {
			asked++
			if e.Entity.Type() != registry.EntityTypePlayer || e.Knockback == (plugin.Vec3{}) {
				t.Errorf("entity %v knockback %v", e.Entity.Type(), e.Knockback)
			}
			e.SetCancelled(cancel)
		})
	})
	_ = c
	at := srv.PlayerPosition("Victim")
	srv.RunWindBurst(at.X+0.5, at.Y+0.5, at.Z)
	if _, vy, _ := srv.PlayerVelocity("Victim"); asked != 1 || vy != 0 {
		t.Fatalf("a cancelled push moved the player: asked %d, vy %v", asked, vy)
	}
	cancel = false
	srv.RunWindBurst(at.X+0.5, at.Y+0.5, at.Z)
	if _, vy, _ := srv.PlayerVelocity("Victim"); asked != 2 || vy <= 0 {
		t.Errorf("an allowed push: asked %d, vy %v", asked, vy)
	}
}
