package mobs

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestASummonedBatFliesUnderACeiling checks the initial resting bit under a
// ceiling, before the AI can randomly settle a flying bat.
// vanilla: SummonCommand, Bat.readAdditionalSaveData
func TestASummonedBatFliesUnderACeiling(t *testing.T) {
	t.Parallel()
	// Tracking still runs while frozen, but Bat.customServerAiStep cannot
	// replace the loaded flags with its one-in-a-hundred resting roll.
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Spelunker", servertest.OpsListing(t, "Spelunker"), func(srv *server.Server) {
		srv.SetTickFrozen(true)
	})
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	srv.SetBlock(6, 66, 0, stone)
	for _, tc := range []struct {
		name    string
		tag     string
		resting bool
	}{
		{name: "untagged"},
		{name: "empty tag", tag: " {}"},
		{name: "flying", tag: " {BatFlags:0b}"},
		{name: "resting", tag: " {BatFlags:1b}", resting: true},
	} {
		c.Run(t, tc.name, func(t *testing.T) {
			c.ChatCommand("summon minecraft:bat 6.5 65.1 0.5" + tc.tag)
			bat := c.ExpectMobSpawn(batTestType(t))
			resting := bat.HasGolemFlags && bat.GolemFlags&protocol.EntityFlagClimbing != 0
			if resting != tc.resting {
				t.Fatalf("summoned bat = %+v, want resting %v", bat, tc.resting)
			}
		})
	}
}

// glowSquidDarkTicks answers /data get for the glow squid's DarkTicksRemaining.
func glowSquidDarkTicks(t *testing.T, srv *server.Server, selector string) (int, bool) {
	t.Helper()
	for _, text := range srv.RunLine("data get entity " + selector + " DarkTicksRemaining") {
		if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
			n, err := strconv.Atoi(text.With[1].Plain())
			return n, err == nil
		}
	}
	return 0, false
}

// TestAGlowSquidSavesItsDarkTicks reads DarkTicksRemaining off a fresh glow
// squid (0) and off one summoned with 200, counted down by the ticks run.
// vanilla: GlowSquid.addAdditionalSaveData, GlowSquid.readAdditionalSaveData
func TestAGlowSquidSavesItsDarkTicks(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	summonSquid(t, c, "minecraft:glow_squid", protocol.EntityTypeGlowSquid, pondCentreX, -10.5)
	if n, ok := glowSquidDarkTicks(t, srv, "@e[type=minecraft:glow_squid,limit=1]"); !ok || n != 0 {
		t.Fatalf("a fresh glow squid answers DarkTicksRemaining %d (answered %v), want 0", n, ok)
	}
	spawn := summonSquidSpawn(t, c, "minecraft:glow_squid", protocol.EntityTypeGlowSquid, pondCentreX, -6.5, `{DarkTicksRemaining:200,Tags:["lit"]}`)
	if !spawn.HasGlowSquidDarkTicks || spawn.GlowSquidDarkTicks < 195 || spawn.GlowSquidDarkTicks > 200 {
		t.Fatalf("the spawn of a glow squid summoned with 200 dark ticks carries %d (sent %v), want 195-200", spawn.GlowSquidDarkTicks, spawn.HasGlowSquidDarkTicks)
	}
	srv.TickMobs(10)
	if n, ok := glowSquidDarkTicks(t, srv, "@e[type=minecraft:glow_squid,tag=lit,limit=1]"); !ok || n < 190 || n > 200 {
		t.Fatalf("a glow squid summoned with 200 dark ticks answers %d (answered %v) 10 ticks on, want 190-200", n, ok)
	}
}
