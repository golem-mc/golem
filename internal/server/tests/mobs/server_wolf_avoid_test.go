package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAWildWolfFleesLlamasButATameWolfStays exercises both llama kinds.
// vanilla: Wolf.registerGoals, Wolf.WolfAvoidEntityGoal
func TestAWildWolfFleesLlamasButATameWolfStays(t *testing.T) {
	for _, kind := range []string{"minecraft:llama", "minecraft:trader_llama"} {
		t.Run(kind, func(t *testing.T) {
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
			c.ChatCommand("summon " + kind + " 4.5 64 0.5 {Strength:5,NoAI:1b}")
			r := protocol.NewReader(c.ExpectPlay(protocol.IDPlaySpawnEntity))
			r.ReadVarInt()
			llamaUUID := r.ReadUUID()
			c.ExpectCommandFeedbackText("commands.summon.success",
				servertest.EntityDisplayComponent(kind, "entity.minecraft."+kind[10:], llamaUUID))
			c.Settle()
			wild := srv.SpawnMobSeeded(wolfType, 8.5, 64, 0.5, 1)
			tame := srv.SpawnMobSeeded(wolfType, 0.5, 64, 2.5, 1)
			srv.TameMobFor(tame, name)
			servertest.RunTicks(t, srv, 60)
			x, _, z, ok := srv.MobPos(wild)
			if !ok || dist(x, z, 4.5, 0.5) < 10 {
				t.Errorf("wild wolf at (%v, %v), want at least ten blocks from the llama", x, z)
			}
			x, _, z, ok = srv.MobPos(tame)
			if !ok || dist(x, z, 0.5, 2.5) > 3 {
				t.Errorf("tame wolf at (%v, %v), want it to stay by its owner", x, z)
			}
		})
	}
}
