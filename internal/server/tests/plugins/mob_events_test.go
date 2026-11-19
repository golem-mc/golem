package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: NearestAttackableTargetGoal, Mob.setTarget
func TestPluginEntityTargetEvent(t *testing.T) {
	cancel, asked := true, 0
	var reason plugin.TargetReason
	srv, c := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityTargetEvent) {
			asked++
			reason = e.Reason
			_ = e.Entity.Health() // reads the mob: no lock may be held
			if e.Target == nil || e.Target.Type() != registry.EntityTypePlayer {
				t.Errorf("target %v", e.Target)
			}
			e.SetCancelled(cancel)
		})
	})
	id := srv.SpawnMob("minecraft:zombie", 3.5, 64, 3.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	for range 60 {
		srv.RunTick()
	}
	if asked == 0 || srv.MobTargetID(id) != 0 {
		t.Fatalf("refused: asked %d, target %d", asked, srv.MobTargetID(id))
	}
	if reason != plugin.TargetClosestPlayer {
		t.Errorf("reason %v", reason)
	}
	cancel = false
	for range 60 {
		srv.RunTick()
	}
	if srv.MobTargetID(id) == 0 {
		t.Error("an allowed target was not set")
	}
}

// vanilla: LivingEntity.addEffect
func TestPluginEntityPotionEffectEvent(t *testing.T) {
	cancel, asked := true, 0
	var cause plugin.EffectCause
	srv, c := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityPotionEffectEvent) {
			asked++
			cause = e.Cause
			_ = e.Entity.Type() // reads the body: no lock may be held
			if e.New != nil {
				e.New.Amplifier = 1
			}
			e.SetCancelled(cancel)
		})
	})
	id := srv.SpawnMob("minecraft:cow", 3.5, 64, 3.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.GiveEffect("Victim", "minecraft:speed", 200, 0, true)
	if _, _, ok := srv.PlayerEffect("Victim", "minecraft:speed"); ok || asked != 1 {
		t.Fatalf("a refused effect landed or was not asked: asked %d", asked)
	}
	srv.GiveMobEffect(id, "minecraft:speed", 200, 0)
	if asked != 2 || cause != plugin.EffectOther {
		t.Fatalf("mob effect asked %d, cause %v", asked, cause)
	}
	cancel = false
	srv.GiveEffect("Victim", "minecraft:speed", 200, 0, true)
	if _, amp, ok := srv.PlayerEffect("Victim", "minecraft:speed"); !ok || amp != 1 {
		t.Errorf("an allowed effect: amplifier %d, present %v (the listener raised it to 1)", amp, ok)
	}
}

// vanilla: SummonCommand, ServerLevel.addFreshEntity
func TestPluginEntitySpawnEvent(t *testing.T) {
	cancel, asked := true, 0
	var reason plugin.SpawnReason
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntitySpawnEvent) {
			asked++
			reason = e.Reason
			_ = e.Entity.Type() // reads the new body: no lock may be held
			e.SetCancelled(cancel)
		})
	})
	srv.SpawnMob("minecraft:cow", 3.5, 64, 3.5)
	if asked != 1 || len(srv.MobIDsOfKind("minecraft:cow")) != 0 {
		t.Fatalf("refused: asked %d, cows %d", asked, len(srv.MobIDsOfKind("minecraft:cow")))
	}
	cancel = false
	srv.SpawnMob("minecraft:cow", 3.5, 64, 3.5)
	if asked != 2 || len(srv.MobIDsOfKind("minecraft:cow")) != 1 || reason != plugin.SpawnDefault {
		t.Errorf("allowed: asked %d, cows %d, reason %v", asked, len(srv.MobIDsOfKind("minecraft:cow")), reason)
	}
}

// vanilla: FarmlandBlock.fallOn
func TestPluginEntityInteractEvent(t *testing.T) {
	cancel, asked := true, 0
	srv, c := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityInteractEvent) {
			asked++
			_ = e.Entity.Type() // reads the mob: no lock may be held
			e.SetCancelled(cancel)
		})
	})
	farmland := servertest.BlockState(t, "minecraft:farmland")
	for _, x := range []int32{4, 5, 6} {
		for _, z := range []int32{4, 5, 6} {
			srv.SetBlock(x, 63, z, farmland)
		}
	}
	srv.SpawnMob("minecraft:cow", 5.5, 74, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	for range 40 {
		srv.RunTick()
	}
	if asked == 0 || srv.BlockAt(5, 63, 5) != farmland {
		t.Fatalf("refused: asked %d, block %d", asked, srv.BlockAt(5, 63, 5))
	}
	cancel = false
	srv.SpawnMob("minecraft:cow", 5.5, 74, 5.5)
	for range 40 {
		srv.RunTick()
	}
	if srv.BlockAt(5, 63, 5) == farmland {
		t.Error("an allowed landing left the farmland")
	}
}
