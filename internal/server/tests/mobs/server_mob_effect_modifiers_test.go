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
	c.ChatCommand("attribute " + selector + " " + attribute + " get")
	var got string
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlaySystemChat {
			return
		}
		text, ok := protocol.ReadText(protocol.NewReader(data))
		if ok && text.Translate == "commands.attribute.value.get.success" {
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
	servertest.RunTicks(t, srv, 2)
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

	c.ChatCommand("summon minecraft:pig 1.5 64 0.5 " + savedBoostPigNBT)
	boosted := c.AwaitSpawn(protocol.EntityTypePig)
	if got, ok := srv.MobHealth(boosted.EntityID); !ok || got != 18 {
		t.Errorf("saved-modifier health = %v (%v), want 18", got, ok)
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
