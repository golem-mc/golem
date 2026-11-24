package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestLlamaSpawnsTellStrengthAndVariant: a summoned llama or trader llama's
// metadata carries its Strength and Variant VarInts next to the inherited
// horse flags and chest fields, which the mob reader decodes.
// vanilla: Llama.defineSynchedData
func TestLlamaSpawnsTellStrengthAndVariant(t *testing.T) {
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
	for _, kind := range []struct {
		name string
		typ  int32
	}{
		{"minecraft:llama", protocol.EntityTypeLlama},
		{"minecraft:trader_llama", protocol.EntityTypeTraderLlama},
	} {
		c.ChatCommand("summon " + kind.name + " 4.5 64 0.5 {Strength:4,Variant:2,Tame:1b,ChestedHorse:1b,NoAI:1b}")
		got := c.ExpectMobSpawn(kind.typ)
		if !got.HasLlamaStrength || got.LlamaStrength != 4 {
			t.Errorf("%s strength = %d (told %v), want 4", kind.name, got.LlamaStrength, got.HasLlamaStrength)
		}
		if !got.HasLlamaVariant || got.LlamaVariant != 2 {
			t.Errorf("%s variant = %d (told %v), want 2", kind.name, got.LlamaVariant, got.HasLlamaVariant)
		}
	}
}
