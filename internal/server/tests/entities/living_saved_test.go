package entities

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func livingSavedConfig() *config.Config {
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	return cfg
}

func livingData(t *testing.T, c *servertest.Client, selector, path string) string {
	t.Helper()
	c.ChatCommand(strings.TrimSpace("data get entity " + selector + " " + path))
	line, ok := protocol.ReadText(protocol.NewReader(c.NextSystemChat()))
	if !ok || line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
		t.Fatalf("data query %s %s: %+v", selector, path, line)
	}
	return line.With[1].Plain()
}

// vanilla: LivingEntity.addAdditionalSaveData, TamableAnimal.addAdditionalSaveData
func TestLivingSavedDefaults(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, livingSavedConfig(), "Scribe", servertest.OpsListing(t, "Scribe"))
	servertest.HoldTicks(t, srv)
	c.Settle()
	for i, kind := range []string{"zombie", "skeleton", "cow", "villager", "creeper", "spider", "horse", "wolf", "armor_stand", "chicken", "iron_golem", "cat", "parrot", "nautilus", "zombie_nautilus"} {
		t.Run(kind, func(t *testing.T) {
			selector := fmt.Sprintf("@e[tag=body%d,limit=1]", i)
			c.ChatCommand(fmt.Sprintf(`summon %s 3.5 64 3.5 {NoAI:1b,Tags:["body%d"]}`, kind, i))
			c.Settle()
			data := livingData(t, c, selector, "")
			for _, field := range []string{"HurtTime: 0s", "DeathTime: 0s", "FallFlying: 0b", "current_impulse_context_reset_grace_time: 0", "Brain: {memories: {}}"} {
				if !strings.Contains(data, field) {
					t.Errorf("missing %s in %s", field, data)
				}
			}
			switch kind {
			case "wolf", "cat", "parrot", "nautilus", "zombie_nautilus":
				if got := livingData(t, c, selector, "Sitting"); got != "0b" {
					t.Errorf("Sitting = %s, want 0b", got)
				}
			case "armor_stand":
				if got := livingData(t, c, selector, "Pose"); got != "{}" {
					t.Errorf("Pose = %s, want {}", got)
				}
			}
		})
	}
}

// vanilla: LivingEntity.hurtServer, baseTick, readAdditionalSaveData
func TestLivingSavedHurtTimer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, livingSavedConfig(), "Scribe", servertest.OpsListing(t, "Scribe"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon cow 3.5 64 3.5 {NoAI:1b,Tags:["hurt"]}`)
	c.Settle()
	const cow = "@e[tag=hurt,limit=1]"
	c.ChatCommand("damage " + cow + " 1")
	c.Settle()
	if got := livingData(t, c, cow, "HurtTime"); got != "10s" {
		t.Fatalf("HurtTime after damage = %s, want 10s", got)
	}
	c.ChatCommand(`execute as @e[tag=hurt,nbt={HurtTime:10s}] run tag @s add matched`)
	c.Settle()
	if got := livingData(t, c, cow, "Tags"); !strings.Contains(got, "matched") {
		t.Fatalf("hurt selector did not match: %s", got)
	}
	servertest.RunTicks(t, srv, 1)
	if got := livingData(t, c, cow, "HurtTime"); got != "9s" {
		t.Errorf("HurtTime after one tick = %s, want 9s", got)
	}
	// A stronger hit inside the cooldown does not restart the animation.
	c.ChatCommand("damage " + cow + " 2")
	c.Settle()
	if got := livingData(t, c, cow, "HurtTime"); got != "9s" {
		t.Errorf("HurtTime after excess damage = %s, want 9s", got)
	}
	c.ChatCommand(`data merge entity ` + cow + ` {HurtTime:4s,DeathTime:3s,FallFlying:1b,current_impulse_context_reset_grace_time:4}`)
	c.Settle()
	for path, want := range map[string]string{"HurtTime": "4s", "DeathTime": "3s", "FallFlying": "1b", "current_impulse_context_reset_grace_time": "4"} {
		if got := livingData(t, c, cow, path); got != want {
			t.Errorf("loaded %s = %s, want %s", path, got, want)
		}
	}
	servertest.RunTicks(t, srv, 4)
	if got := livingData(t, c, cow, "HurtTime"); got != "0s" {
		t.Errorf("HurtTime after countdown = %s, want 0s", got)
	}
	if got := livingData(t, c, cow, "current_impulse_context_reset_grace_time"); got != "0" {
		t.Errorf("impulse grace after countdown = %s, want 0", got)
	}
}

// vanilla: ArmorStand.ArmorStandPose.CODEC
func TestArmorStandSavedPoseOmitsDefaults(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, livingSavedConfig(), "Scribe", servertest.OpsListing(t, "Scribe"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon armor_stand 3.5 64 3.5 {Tags:["pose"],Pose:{Head:[15.0f,0.0f,0.0f]}}`)
	c.Settle()
	const stand = "@e[tag=pose,limit=1]"
	if got := livingData(t, c, stand, "Pose"); got != "{Head: [15.0f, 0.0f, 0.0f]}" {
		t.Errorf("custom Pose = %s", got)
	}
	c.ChatCommand(`data merge entity ` + stand + ` {Pose:{Head:[0.0f,0.0f,0.0f]}}`)
	c.Settle()
	if got := livingData(t, c, stand, "Pose"); got != "{}" {
		t.Errorf("reset Pose = %s, want {}", got)
	}
}

// The selector excludes corpses, so retain the public data target before the death.
// vanilla: LivingEntity.baseTick, tickDeath
func TestLivingSavedDeathTimer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, livingSavedConfig(), "Scribe", servertest.OpsListing(t, "Scribe"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon cow 3.5 64 3.5 {NoAI:1b,DeathTime:15s,Tags:["dying"]}`)
	c.Settle()
	selected := srv.SelectedEntities("minecraft:cow")
	if len(selected) != 1 {
		t.Fatalf("got %d cows, want one", len(selected))
	}
	body := selected[0].(command.DataTarget)
	c.ChatCommand("damage @e[tag=dying,limit=1] 100")
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	tag, ok := body.DataTag()
	if !ok || tag.Entries()["DeathTime"] != nbt.Short(16) {
		t.Fatalf("corpse DeathTime = %v, want 16s", tag.Entries()["DeathTime"])
	}
	if tag.Entries()["HurtTime"] != nbt.Short(9) {
		t.Errorf("corpse HurtTime = %v, want 9s", tag.Entries()["HurtTime"])
	}
	// A loaded death count shortens the remaining animation, as tickDeath's >=20 does.
	servertest.ExpectNoRemovalOfTicks(t, c, srv, 3, selected[0].EntityID())
	servertest.RunTicks(t, srv, 1)
	c.ExpectRemoval(selected[0].EntityID())
}

// A bare UUID is a direct lookup, not an @e sweep, so it reaches the corpse
// for as long as vanilla's level lookup keeps it.
// vanilla: EntitySelector.findEntities, LivingEntity.tickDeath
func TestDirectUUIDDataReachesDyingMob(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, livingSavedConfig(), "Scribe", servertest.OpsListing(t, "Scribe"))
	servertest.HoldTicks(t, srv)
	const cow = "000004d2-0000-162e-0000-10e10000223e"
	c.ChatCommand(`summon cow 3.5 64 3.5 {NoAI:1b,DeathTime:15s,UUID:[I;1234,5678,4321,8766]}`)
	c.Settle()

	c.ChatCommand("damage " + cow + " 100")
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if got := livingData(t, c, cow, "DeathTime"); got != "16s" {
		t.Fatalf("dying DeathTime after one tick = %s, want 16s", got)
	}
	if got := livingData(t, c, cow, "HurtTime"); got != "9s" {
		t.Fatalf("dying HurtTime after one tick = %s, want 9s", got)
	}

	servertest.RunTicks(t, srv, 3)
	if got := livingData(t, c, cow, "DeathTime"); got != "19s" {
		t.Fatalf("dying DeathTime after four ticks = %s, want 19s", got)
	}
	servertest.RunTicks(t, srv, 1)
	c.ChatCommand("data get entity " + cow + " DeathTime")
	servertest.ExpectCommandError(t, c, "argument.entity.notfound.entity")
}

// vanilla: Entity.baseTick's fire damage precedes LivingEntity.baseTick's timers.
func TestLivingSavedFireDeathTimer(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, livingSavedConfig(), "Scribe", servertest.OpsListing(t, "Scribe"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon cow 3.5 64 3.5 {NoAI:1b,Health:1.0f,Fire:20s}`)
	c.Settle()
	selected := srv.SelectedEntities("minecraft:cow")
	if len(selected) != 1 {
		t.Fatalf("got %d cows, want one", len(selected))
	}
	body := selected[0].(command.DataTarget)
	servertest.RunTicks(t, srv, 1)
	tag, ok := body.DataTag()
	if !ok || tag.Entries()["DeathTime"] != nbt.Short(1) || tag.Entries()["HurtTime"] != nbt.Short(9) {
		t.Fatalf("fire death: DeathTime = %v, HurtTime = %v, want 1s and 9s", tag.Entries()["DeathTime"], tag.Entries()["HurtTime"])
	}
}

// vanilla: LivingEntity.baseTick, tick; EnderDragon.aiStep, tickDeath
func TestDragonSavedLivingTimers(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, livingSavedConfig(), "Scribe", servertest.OpsListing(t, "Scribe"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon ender_dragon 3.5 128 3.5 {NoAI:1b,HurtTime:4s,current_impulse_context_reset_grace_time:4,Tags:["dragon"]}`)
	c.Settle()
	const dragon = "@e[tag=dragon,limit=1]"
	servertest.RunTicks(t, srv, 1)
	for path, want := range map[string]string{"HurtTime": "3s", "current_impulse_context_reset_grace_time": "3"} {
		if got := livingData(t, c, dragon, path); got != want {
			t.Errorf("living dragon %s = %s, want %s", path, got, want)
		}
	}
	selected := srv.SelectedEntities("minecraft:ender_dragon")
	if len(selected) != 1 {
		t.Fatalf("got %d dragons, want one", len(selected))
	}
	// The selector excludes corpses; retain its public target for tickDeath.
	body := selected[0].(command.DataTarget)
	c.ChatCommand(`data merge entity ` + dragon + ` {Health:0.0f,HurtTime:4s,current_impulse_context_reset_grace_time:4}`)
	c.Settle()
	for _, step := range []struct{ ticks, want int }{{1, 3}, {3, 0}, {1, 0}} {
		servertest.RunTicks(t, srv, step.ticks)
		want := step.want
		tag, ok := body.DataTag()
		if !ok {
			t.Fatal("dragon removed before its death animation finished")
		}
		if tag.Entries()["HurtTime"] != nbt.Short(want) || tag.Entries()["current_impulse_context_reset_grace_time"] != nbt.Int(want) {
			t.Errorf("dying dragon HurtTime = %v, impulse grace = %v, want %d", tag.Entries()["HurtTime"], tag.Entries()["current_impulse_context_reset_grace_time"], want)
		}
	}
}
