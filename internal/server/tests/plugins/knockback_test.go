package plugins_test

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
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

// vanilla: Player.attack, AbstractArrow.onHitEntity, LivingEntity.knockback
func TestPluginEntityKnockbackPlayer(t *testing.T) {
	t.Parallel()
	asked, cancel, wantPusher := 0, true, true
	var custom *plugin.Vec3
	var victimID, attackerID int32
	cfg := servertest.SurvivalTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "kbplayer", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.EntityKnockbackEvent) {
			asked++
			if e.Entity.Type() != registry.EntityTypePlayer || e.Entity.ID() != victimID || e.Knockback == (plugin.Vec3{}) {
				t.Errorf("entity %v (id %d) knockback %v", e.Entity.Type(), e.Entity.ID(), e.Knockback)
			}
			if wantPusher && (e.Pusher == nil || e.Pusher.ID() != attackerID) {
				t.Errorf("pusher = %v, want attacker %d", e.Pusher, attackerID)
			}
			if custom != nil {
				e.Knockback = *custom
			}
			e.SetCancelled(cancel)
		})
		return nil
	}}}})
	victim := servertest.DialServer(t, srv)
	victim.ViewAsk = config.MinDistance
	victimID = victim.JoinAs("Victim")
	victim.MoveToGround(2.5, 64, 0.5, true)
	victim.Settle()

	attacker := servertest.DialServer(t, srv)
	attacker.ViewAsk = config.MinDistance
	attackerID = attacker.JoinAs("Attacker")
	attacker.MoveToGround(0.5, 64, 0.5, true)
	attacker.Settle()

	servertest.HoldTicks(t, srv)
	victimMotion := func() (v [3]float64, got bool) {
		victim.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySetEntityMotion {
				return
			}
			r := protocol.NewReader(data)
			if r.ReadVarInt() == victimID {
				v[0], v[1], v[2] = r.ReadLpVec3()
				got = true
			}
		})
		return v, got
	}

	victim.Settle()
	attacker.Attack(victimID)
	fired(t, srv, attacker, "player melee knockback cancel", &asked, 1)
	if v, got := victimMotion(); got {
		t.Fatalf("cancelled melee sent motion %v to player", v)
	}
	if vx, vy, vz := srv.PlayerVelocity("Victim"); vx != 0 || vy > 0 || vz != 0 {
		t.Fatalf("cancelled melee moved player: (%v, %v, %v)", vx, vy, vz)
	}

	servertest.RunTicks(t, srv, 15)
	victim.Settle()
	cancel = false
	want := plugin.Vec3{X: 0, Y: 0.75, Z: 0}
	custom = &want
	attacker.Attack(victimID)
	fired(t, srv, attacker, "player melee knockback edit", &asked, 2)
	if v, got := victimMotion(); !got || math.Abs(v[0]) > 1e-3 || v[1] < 0.6 || math.Abs(v[2]) > 1e-3 {
		t.Errorf("edited melee knockback motion = %v (got=%v), want ~%v", v, got, want)
	}

	servertest.RunTicks(t, srv, 15)
	victim.Settle()
	wantPusher, cancel, custom = false, true, nil
	srv.ShootArrow(1.0, 64.8, 0.5, 1.5, 0, 0)
	for range 10 {
		servertest.RunTicks(t, srv, 1)
		if asked >= 3 {
			break
		}
	}
	if asked != 3 {
		t.Fatalf("arrow knockback on player: asked %d, want 3", asked)
	}
	if v, got := victimMotion(); got {
		t.Errorf("cancelled arrow sent motion %v to player", v)
	}
	if vx, vy, vz := srv.PlayerVelocity("Victim"); vx != 0 || vy > 0 || vz != 0 {
		t.Errorf("cancelled arrow moved player: (%v, %v, %v)", vx, vy, vz)
	}
}

// vanilla: AbstractWindCharge.explode, ServerExplosion.hurtEntities
func TestPluginEntityKnockbackWindChargeBurstPusher(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"thrown", "burst", "breeze"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			want := registry.EntityTypeWindCharge
			var types []registry.EntityType
			var pos []plugin.Vec3
			listen := func(s plugin.Server) {
				plugin.Listen(s, func(e *plugin.EntityKnockbackEvent) {
					if e.Entity.Type() != registry.EntityTypeCow || e.Pusher == nil {
						return
					}
					types = append(types, e.Pusher.Type())
					pos = append(pos, e.Pusher.Location().Pos)
				})
			}
			var srv *server.Server
			if kind == "thrown" {
				srv, _, _ = blockScene(t, "minecraft:stick", false, listen)
			} else {
				srv, _ = tickScene(t, listen)
			}
			cow := srv.SpawnMob("minecraft:cow", 10.5, 64, 0.5)
			srv.SetMobNoAI(cow)
			switch kind {
			case "thrown":
				srv.ThrowThrown(entity.ThrownWindCharge, 9.0, 64.8, 0.5, -90, 0, 0, 1)
			case "burst":
				srv.RunWindBurst(10.5, 64, 1.5)
			case "breeze":
				want = registry.EntityTypeBreezeWindCharge
				srv.RunBreezeWindBurst(10.5, 64, 1.5)
			}
			if kind == "thrown" {
				servertest.RunTicks(t, srv, 20)
			}
			if len(types) == 0 {
				t.Fatal("no EntityKnockbackEvent with a pusher")
			}
			for i, typ := range types {
				if typ != want {
					t.Errorf("pusher %d type = %v, want %v", i, typ, want)
				}
				if pos[i] == (plugin.Vec3{}) {
					t.Errorf("pusher %d has no location", i)
				}
			}
		})
	}
}
