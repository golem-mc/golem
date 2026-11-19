package mobs

import (
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
