package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: ChunkMap.removeEntity, ChunkMap.updatePlayerStatus
func TestShortLivedEntityRejoin(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Watcher", servertest.OpsListing(t, "Watcher"))
	servertest.HoldTicks(t, srv)
	servertest.FreezeUntilHeld(t, srv)
	wanted := map[int32]string{}
	kinds := []string{"evoker_fangs", "eye_of_ender", "lightning_bolt"}
	for _, kind := range kinds {
		c.ChatCommand("summon " + kind + " 2.5 64 4.5 {Warmup:40}")
		typ, _ := registry.EntityTypeID("minecraft:" + kind)
		spawn := c.AwaitSpawn(typ)
		wanted[spawn.EntityID] = kind
		c.ExpectSystemChatPastMobs()
	}
	c.Disconnect()
	servertest.ReleaseTicks(t, srv)
	again := servertest.DialServer(t, srv)
	again.Login("Watcher")
	again.Configure()
	seen := map[int32]int{}
	collect := func(id int32, data []byte) {
		if id == protocol.IDPlaySpawnEntity {
			seen[protocol.NewReader(data).ReadVarInt()]++
		}
	}
	again.ReadJoin(collect)
	again.MarkLoaded()
	servertest.HoldTicks(t, srv)
	again.ReadTicks(srv, 2, func(id int32, data []byte) bool {
		collect(id, data)
		return false
	})
	for id, kind := range wanted {
		if seen[id] != 1 {
			t.Errorf("%s respawned %d times after rejoining, want 1", kind, seen[id])
		}
	}
}
