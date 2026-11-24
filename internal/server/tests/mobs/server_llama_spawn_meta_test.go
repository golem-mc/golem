package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestALlamaSpawnTellsItsStrengthAndVariant: a summoned llama's metadata
// carries its Strength and Variant VarInts, which the mob reader decodes.
// vanilla: Llama.defineSynchedData
func TestALlamaSpawnTellsItsStrengthAndVariant(t *testing.T) {
	t.Parallel()
	const name = "Watcher"
	srv, connect := servertest.Start(t, servertest.MobTestConfig(),
		servertest.OpsListing(t, name), func(s *server.Server) {
			servertest.FreezeUntilHeld(t, s)
		})
	c := connect().Join(name)
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.ChatCommand("summon minecraft:llama 4.5 64 0.5 {Strength:4,Variant:2,NoAI:1b}")
	got := c.ExpectMobSpawn(protocol.EntityTypeLlama)
	if !got.HasLlamaStrength || got.LlamaStrength != 4 {
		t.Errorf("llama strength = %d (told %v), want 4", got.LlamaStrength, got.HasLlamaStrength)
	}
	if !got.HasLlamaVariant || got.LlamaVariant != 2 {
		t.Errorf("llama variant = %d (told %v), want 2", got.LlamaVariant, got.HasLlamaVariant)
	}
}
