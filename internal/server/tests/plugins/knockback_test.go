package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
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

// vanilla: ServerExplosion.explode, Mob.PushByBlast
func TestPluginEntityKnockbackMobBlast(t *testing.T) {
	t.Parallel()
	asked, cancel := 0, true
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityKnockbackEvent) {
			if e.Entity.Type() != registry.EntityTypeCow {
				return
			}
			asked++
			if e.Knockback == (plugin.Vec3{}) {
				t.Errorf("empty knockback on cow")
			}
			e.SetCancelled(cancel)
		})
	})
	cow := srv.SpawnMob("minecraft:cow", 10.5, 64, 10.5)
	srv.SetMobNoAI(cow)
	srv.RunWindBurst(11.0, 64.5, 10.5)
	vx, vy, vz, ok := srv.MobVelocity(cow)
	if !ok || asked != 1 || vx != 0 || vy != 0 || vz != 0 {
		t.Fatalf("cancelled mob blast: asked %d, v (%v, %v, %v)", asked, vx, vy, vz)
	}
	cancel = false
	srv.RunWindBurst(11.0, 64.5, 10.5)
	vx, vy, vz, ok = srv.MobVelocity(cow)
	if !ok || asked != 2 || vy <= 0 {
		t.Errorf("allowed mob blast: asked %d, v (%v, %v, %v)", asked, vx, vy, vz)
	}
}

// vanilla: Player.attack, LivingEntity.knockback
func TestPluginEntityKnockbackMelee(t *testing.T) {
	t.Parallel()
	asked, cancel := 0, true
	var custom *plugin.Vec3
	var attackerID int32
	srv, _, c := blockScene(t, "minecraft:stick", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityKnockbackEvent) {
			asked++
			if e.Entity.Type() != registry.EntityTypeCow || e.Pusher == nil || e.Pusher.ID() != attackerID {
				t.Errorf("entity %v pusher %v", e.Entity.Type(), e.Pusher)
			}
			if custom != nil {
				e.Knockback = *custom
			}
			e.SetCancelled(cancel)
		})
	})
	attackerID = c.PlayerEntityID
	cow := srv.SpawnMob("minecraft:cow", 1.5, 64, 0.5)
	srv.SetMobNoAI(cow)
	c.Attack(cow)
	fired(t, srv, c, "melee knockback cancel", &asked, 1)
	if vx, vy, vz, _ := srv.MobVelocity(cow); vx != 0 || vy != 0 || vz != 0 {
		t.Fatalf("cancelled melee moved cow: (%v, %v, %v)", vx, vy, vz)
	}
	servertest.RunTicks(t, srv, 15)
	cancel = false
	want := plugin.Vec3{X: 0, Y: 0.75, Z: 0}
	custom = &want
	c.Attack(cow)
	fired(t, srv, c, "melee knockback custom", &asked, 2)
	if vx, vy, vz, _ := srv.MobVelocity(cow); vx != 0 || vy != want.Y || vz != 0 {
		t.Errorf("edited melee knockback = (%v, %v, %v), want %v", vx, vy, vz, want)
	}
}

// vanilla: AbstractArrow.onHitEntity, AbstractWindCharge.onHitEntity
func TestPluginEntityKnockbackArrowAndWindCharge(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"arrow", "wind_charge"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			asked := 0
			srv, _, _ := blockScene(t, "minecraft:stick", false, func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.EntityKnockbackEvent) {
					if e.Entity.Type() != registry.EntityTypeCow {
						return
					}
					asked++
					e.SetCancelled(true)
				})
			})
			cow := srv.SpawnMob("minecraft:cow", 10.5, 64, 0.5)
			srv.SetMobNoAI(cow)
			if kind == "arrow" {
				srv.ShootArrow(9.0, 64.8, 0.5, 1.5, 0, 0)
			} else {
				srv.ThrowThrown(entity.ThrownWindCharge, 9.0, 64.8, 0.5, -90, 0, 0, 1)
			}
			for range 10 {
				servertest.RunTicks(t, srv, 1)
				if asked > 0 {
					break
				}
			}
			if asked == 0 {
				t.Fatalf("no EntityKnockbackEvent for %s", kind)
			}
			if vx, vy, vz, _ := srv.MobVelocity(cow); vx != 0 || vy != 0 || vz != 0 {
				t.Errorf("cancelled %s moved cow: (%v, %v, %v)", kind, vx, vy, vz)
			}
		})
	}
}
