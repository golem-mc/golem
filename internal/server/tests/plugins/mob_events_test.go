package plugins_test

import (
	"math"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
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

// TestPluginEventCauses checks the reason a spawn and the cause an effect
// carries for a command and for the plugin API.
// vanilla: SummonCommand, EffectCommands.giveEffect
func TestPluginEventCauses(t *testing.T) {
	var reasons []plugin.SpawnReason
	var causes []plugin.EffectCause
	var mob plugin.LivingEntity
	srv, c := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntitySpawnEvent) {
			reasons = append(reasons, e.Reason)
			mob, _ = e.Entity.(plugin.LivingEntity)
		})
		plugin.Listen(s, func(e *plugin.EntityPotionEffectEvent) { causes = append(causes, e.Cause) })
	}, servertest.OpsListing(t, "Victim"))
	run := func(cmd string, done func() bool) {
		t.Helper()
		c.ChatCommand(cmd)
		ticksUntil(t, srv, cmd, done)
	}
	run("summon minecraft:cow 3.5 64 3.5", func() bool { return len(reasons) == 1 })
	if reasons[0] != plugin.SpawnCommand {
		t.Errorf("/summon reason %v, want %v", reasons[0], plugin.SpawnCommand)
	}
	run("effect give @e[type=minecraft:cow] minecraft:speed 30", func() bool { return len(causes) == 1 })
	speed := plugin.Effect(registry.MustMobEffect("speed"))
	if !mob.AddEffect(plugin.EffectInstance{Effect: speed, Duration: 100, Amplifier: 1}) {
		t.Fatal("AddEffect refused")
	}
	if len(causes) != 2 || causes[0] != plugin.EffectCommand || causes[1] != plugin.EffectPlugin {
		t.Errorf("causes %v, want [%v %v]", causes, plugin.EffectCommand, plugin.EffectPlugin)
	}
}

// vanilla: AbstractArrow.playerTouch
func TestPluginPlayerPickupArrowEvent(t *testing.T) {
	cancel, asked := true, 0
	srv, c := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.PlayerPickupArrowEvent) {
			asked++
			_ = e.Player.Name() // reads the player: no lock may be held
			_ = e.Arrow.Type()
			e.SetCancelled(cancel)
		})
	})
	pos := srv.PlayerPosition("Victim")
	id := srv.SpawnStuckArrow("allowed", pos.X, pos.Y, pos.Z)
	for range 5 {
		srv.RunTick()
	}
	if asked == 0 || !srv.ArrowAlive(id) {
		t.Fatalf("a cancelled pickup: asked %d, arrow alive %v", asked, srv.ArrowAlive(id))
	}
	cancel = false
	ticksUntil(t, srv, "the allowed pickup", func() bool { return !srv.ArrowAlive(id) })
	_ = c
}

// vanilla: BreakDoorGoal.tick
func TestPluginEntityBreakDoorEvent(t *testing.T) {
	cancel, asked := true, 0
	srv, c := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityBreakDoorEvent) {
			asked++
			_ = e.Entity.Type() // reads the mob: no lock may be held
			e.SetCancelled(cancel)
		})
	}, func(s *server.Server) { s.SetDifficulty(3) })
	srv.SetSpawning(false)
	srv.SetDayTime(18000)
	pos := srv.PlayerPosition("Victim")
	px, py, pz := int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z))
	stone := registry.BlockStates["minecraft:stone"]
	for dx := int32(-2); dx <= 2; dx++ {
		for dz := int32(-2); dz <= 2; dz++ {
			if max(dx, -dx, dz, -dz) == 2 {
				srv.SetBlock(px+dx, py, pz+dz, stone)
				srv.SetBlock(px+dx, py+1, pz+dz, stone)
			}
		}
	}
	for half, y := range map[string]int32{"lower": py, "upper": py + 1} {
		srv.SetBlock(px+2, y, pz, servertest.BlockStateWith(t, "minecraft:oak_door",
			"facing=west,half="+half+",hinge=left,open=false,powered=false"))
	}
	id := srv.SpawnMobSeeded("minecraft:zombie", pos.X+3.5, pos.Y, pos.Z, 4)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	srv.MakeMobLeader(id)
	c.Attack(id)
	for range 600 {
		srv.RunTick()
		time.Sleep(time.Millisecond) // the attack packet needs a moment between ticks
		if asked > 0 {
			break
		}
	}
	if asked == 0 {
		t.Fatal("the door break was not put to listeners")
	}
	if srv.BlockAt(px+2, py, pz) == 0 {
		t.Fatal("a cancelled break took the door")
	}
	cancel = false
	ticksUntil(t, srv, "the allowed break", func() bool { return srv.BlockAt(px+2, py, pz) == 0 })
}
