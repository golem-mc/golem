package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: MobEffect.addAttributeModifiers; Health Boost is MobEffects.HEALTH_BOOST

const savedBoostPigNBT = `{NoAI:1b,Health:18.0f,` +
	`attributes:[{id:"minecraft:max_health",base:10d,modifiers:[{id:"minecraft:effect.health_boost",amount:8d,operation:"add_value"}]}]}`

func effectModifierConfig() *config.Config {
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	return cfg
}

// attributeGet is the value `/attribute ... get` reports, read without a tick.
func attributeGet(c *servertest.Client, selector, attribute string) string {
	c.T.Helper()
	return attributeReply(c, "attribute "+selector+" "+attribute+" get", "commands.attribute.value.get.success")
}

// attributeReply runs an /attribute command and returns the last argument of
// its key's feedback, read without a tick.
func attributeReply(c *servertest.Client, command, key string) string {
	c.T.Helper()
	c.ChatCommand(command)
	var got string
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySystemChat {
			return
		}
		text, ok := protocol.ReadText(protocol.NewReader(data))
		if ok && text.Translate == key {
			got = text.With[len(text.With)-1].Text
		}
	})
	return got
}

func TestHealthBoostRaisesAMobsMaximumHealth(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, effectModifierConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon minecraft:pig 0.5 64 0.5 {NoAI:1b}")
	pig := c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand("effect give @e[type=minecraft:pig,limit=1] minecraft:health_boost 60 1")
	c.ChatCommand("effect give @e[type=minecraft:pig,limit=1] minecraft:regeneration 60 4")
	c.Settle()
	servertest.RunTicks(t, srv, 22)
	if got, ok := srv.MobHealth(pig.EntityID); !ok || got != 18 {
		t.Errorf("health = %v (%v), want 18", got, ok)
	}
}

func TestStrengthShowsAsAModifierWithoutATick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, effectModifierConfig(), "Keeper", servertest.OpsListing(t, "Keeper"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon minecraft:zombie 0.5 64 0.5 {NoAI:1b}")
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	c.ChatCommand("effect give @e[type=minecraft:zombie,limit=1] minecraft:strength 30 1")
	if got := attributeGet(c, "@e[type=minecraft:zombie,limit=1]", "minecraft:attack_damage"); got != "9.0" {
		t.Errorf("attack_damage = %q, want 9.0", got)
	}
}

func TestACommandModifierOutlivesNoEffect(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, effectModifierConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon minecraft:pig 0.5 64 0.5 {NoAI:1b}")
	c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand("attribute @e[type=minecraft:pig,limit=1] minecraft:max_health modifier add minecraft:effect.health_boost 8 add_value")
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if got := attributeGet(c, "@e[type=minecraft:pig,limit=1]", "minecraft:max_health"); got != "18.0" {
		t.Errorf("max_health = %q, want 18.0", got)
	}
}

func TestSavedEffectsDoNotRaiseMaximumHealth(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, effectModifierConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand(`summon minecraft:pig 0.5 64 0.5 {NoAI:1b,Health:18.0f,active_effects:[{id:"minecraft:health_boost",amplifier:1b,duration:600}]}`)
	pig := c.AwaitSpawn(protocol.EntityTypePig)
	if got, ok := srv.MobHealth(pig.EntityID); !ok || got != 10 {
		t.Errorf("effect-only health = %v (%v), want 10", got, ok)
	}
	if got := attributeGet(c, "@e[type=minecraft:pig,limit=1]", "minecraft:max_health"); got != "10.0" {
		t.Errorf("effect-only max_health = %q, want 10.0", got)
	}

	c.ChatCommand("summon minecraft:pig 1.5 64 0.5 " + savedBoostPigNBT)
	boosted := c.AwaitSpawn(protocol.EntityTypePig)
	if got, ok := srv.MobHealth(boosted.EntityID); !ok || got != 18 {
		t.Errorf("saved-modifier health = %v (%v), want 18", got, ok)
	}
	if got := attributeGet(c, "@e[type=minecraft:pig,x=1.5,y=64,z=0.5,distance=..0.4,limit=1]", "minecraft:max_health"); got != "18.0" {
		t.Errorf("saved-modifier max_health = %q, want 18.0", got)
	}
}

func TestEndedHealthBoostSendsTheTrimmedHealth(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, effectModifierConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon minecraft:pig 0.5 64 0.5 " + savedBoostPigNBT)
	pig := c.ExpectMobSpawn(protocol.EntityTypePig)
	if pig.Health != 18 {
		t.Fatalf("spawn health = %v, want 18", pig.Health)
	}
	c.ChatCommand("effect give @e[type=minecraft:pig,limit=1] minecraft:health_boost 600 1 true")
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	c.Settle()
	c.ChatCommand("effect clear @e[type=minecraft:pig,limit=1] minecraft:health_boost")
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if got := servertest.ExpectMobHealth(c, pig.EntityID); got != 10 {
		t.Errorf("health metadata = %v, want 10", got)
	}
}

func TestDurationNoticeKeepsACommandModifier(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, effectModifierConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon minecraft:pig 0.5 64 0.5 {NoAI:1b}")
	c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand("effect give @e[type=minecraft:pig,limit=1] minecraft:health_boost 601 1")
	c.ChatCommand("attribute @e[type=minecraft:pig,limit=1] minecraft:max_health modifier remove minecraft:effect.health_boost")
	c.ChatCommand("attribute @e[type=minecraft:pig,limit=1] minecraft:max_health modifier add minecraft:effect.health_boost 2 add_value")
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if got := attributeGet(c, "@e[type=minecraft:pig,limit=1]", "minecraft:max_health"); got != "12.0" {
		t.Errorf("max_health = %q, want 12.0", got)
	}
}

func TestPromotedEffectRewritesItsModifier(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, effectModifierConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon minecraft:pig 0.5 64 0.5 {NoAI:1b}")
	c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand("effect give @e[type=minecraft:pig,limit=1] minecraft:health_boost 1 1")
	c.ChatCommand("effect give @e[type=minecraft:pig,limit=1] minecraft:health_boost 100 0")
	c.Settle()
	servertest.RunTicks(t, srv, 20)
	if got := attributeGet(c, "@e[type=minecraft:pig,limit=1]", "minecraft:max_health"); got != "14.0" {
		t.Errorf("max_health = %q, want 14.0", got)
	}
}

func TestRefreshedSpeedMovesBehindSlowness(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, effectModifierConfig(), "Keeper", servertest.OpsListing(t, "Keeper"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon minecraft:pig 0.5 64 0.5 {NoAI:1b}")
	c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand("effect give @e[type=minecraft:pig,limit=1] minecraft:speed 5")
	c.ChatCommand("effect give @e[type=minecraft:pig,limit=1] minecraft:slowness 10")
	c.ChatCommand("effect give @e[type=minecraft:pig,limit=1] minecraft:speed 10")
	c.ChatCommand("data get entity @e[type=minecraft:pig,limit=1] attributes[{id:\"minecraft:movement_speed\"}].modifiers[0].id")
	var got string
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySystemChat {
			return
		}
		if text, ok := protocol.ReadText(protocol.NewReader(data)); ok && text.Translate == "commands.data.entity.query" {
			for _, e := range text.With[len(text.With)-1].Extra {
				got += e.Text
			}
		}
	})
	if got != `"minecraft:effect.slowness"` {
		t.Errorf("first modifier = %q, want slowness", got)
	}
}

func TestFirstEffectApplicationMovesAnIdenticalModifierToTheEnd(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, effectModifierConfig(), "Keeper", servertest.OpsListing(t, "Keeper"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)

	const pig = "@e[type=minecraft:pig,limit=1]"
	c.ChatCommand("summon minecraft:pig 0.5 64 0.5 {NoAI:1b}")
	c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand("attribute " + pig + " minecraft:max_health modifier add minecraft:effect.health_boost 8 add_value")
	c.ChatCommand("attribute " + pig + " minecraft:max_health modifier add minecraft:other 1 add_value")
	c.ChatCommand("effect give " + pig + " minecraft:health_boost 60 1")
	c.ChatCommand("data get entity " + pig + " attributes[{id:\"minecraft:max_health\"}].modifiers[0].id")
	var got string
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySystemChat {
			return
		}
		if text, ok := protocol.ReadText(protocol.NewReader(data)); ok && text.Translate == "commands.data.entity.query" {
			for _, e := range text.With[len(text.With)-1].Extra {
				got += e.Text
			}
		}
	})
	if got != `"minecraft:other"` {
		t.Errorf("first modifier = %q, want minecraft:other", got)
	}
}

func TestBabyPiglinKeepsItsSpeedWhenAnEffectInstallsTheMap(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, effectModifierConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon minecraft:piglin 0.5 64 0.5 {NoAI:1b,IsBaby:1b}")
	piglin := c.AwaitSpawn(protocol.EntityTypePiglin)
	before := srv.MobPace(piglin.EntityID)
	c.ChatCommand("effect give @e[type=minecraft:piglin,limit=1] minecraft:health_boost 60 1")
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if got := srv.MobPace(piglin.EntityID); got != before {
		t.Errorf("pace = %v, want %v", got, before)
	}
}

// vanilla: Piglin.setBaby adds the baby modifier only when the state is written
func TestBabyPiglinKeepsACommandsBabyModifier(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, effectModifierConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)

	const piglin = "@e[type=minecraft:piglin,limit=1]"
	const babyGet = "attribute " + piglin + " minecraft:movement_speed modifier value get minecraft:baby"
	const valueKey = "commands.attribute.modifier.value.get.success"
	c.ChatCommand("summon minecraft:piglin 0.5 64 0.5 {NoAI:1b,IsBaby:1b}")
	c.AwaitSpawn(protocol.EntityTypePiglin)
	c.ChatCommand("effect give " + piglin + " minecraft:health_boost 60 1")
	if got := attributeReply(c, babyGet, valueKey); got != "0.20000000298023224" {
		t.Errorf("baby modifier on install = %q, want 0.20000000298023224", got)
	}
	c.ChatCommand("attribute " + piglin + " minecraft:movement_speed modifier remove minecraft:baby")
	c.ChatCommand("attribute " + piglin + " minecraft:movement_speed modifier add minecraft:baby 0.5 add_multiplied_base")
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	if got := attributeReply(c, babyGet, valueKey); got != "0.5" {
		t.Errorf("baby modifier after a tick = %q, want 0.5", got)
	}
}

// vanilla: LivingEntity.calculateFallPower reads the installed safe_fall_distance
func TestJumpBoostLandingUsesACommandModifier(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, effectModifierConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)

	const pig = "@e[type=minecraft:pig,limit=1]"
	c.ChatCommand("summon minecraft:pig 0.5 64 0.5 {fall_distance:10d}")
	p := c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand("effect give " + pig + " minecraft:jump_boost 60 0")
	c.ChatCommand("attribute " + pig + " minecraft:safe_fall_distance modifier remove minecraft:effect.jump_boost")
	c.ChatCommand("attribute " + pig + " minecraft:safe_fall_distance modifier add minecraft:effect.jump_boost 8 add_value")
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	if got, ok := srv.MobHealth(p.EntityID); !ok || got != 10 {
		t.Errorf("health = %v (%v), want 10", got, ok)
	}
}
