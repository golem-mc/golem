package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// Every effect's attribute modifier lands on a mob when the effect starts, so
// health boost raises the mob's maximum health and /attribute reads a
// strength modifier at once.
// vanilla: MobEffect.addAttributeModifiers, HealthBoostMobEffect

func TestHealthBoostRaisesAMobsMaximumHealth(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon minecraft:pig 0.5 64 0.5 {NoAI:1b}")
	pig := c.ExpectMobSpawn(protocol.EntityTypePig)
	c.ChatCommand("effect give @e[type=minecraft:pig,limit=1] minecraft:health_boost 60 1")
	c.ChatCommand("effect give @e[type=minecraft:pig,limit=1] minecraft:regeneration 60 4")
	servertest.RunTicks(t, srv, 80)
	if got, ok := srv.MobHealth(pig.EntityID); !ok || got != 18 {
		t.Errorf("health = %v (%v), want 18", got, ok)
	}
}

func TestStrengthShowsAsAModifierWithoutATick(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon minecraft:zombie 0.5 64 0.5 {NoAI:1b}")
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	c.ChatCommand("effect give @e[type=minecraft:zombie,limit=1] minecraft:strength 30 1")
	c.ChatCommand("attribute @e[type=minecraft:zombie,limit=1] minecraft:attack_damage get")
	if _, ok := c.SystemChatContaining(srv, 1, "9.0"); !ok {
		t.Error("attack_damage get did not answer 9.0 right after strength II")
	}
}
