package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: ItemEntity.readAdditionalSaveData, ItemEntity.addAdditionalSaveData.
func TestSummonedItemThrowerPersists(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon item 4.5 70 4.5 {NoGravity:1b,Item:{id:"minecraft:stone",count:1},Thrower:[I;1,2,3,4]}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	check := func(t *testing.T, client *servertest.Client) {
		t.Helper()
		client.ChatCommand(`data get entity @e[type=item,limit=1] Thrower`)
		line := servertest.FeedbackComponent(t, client)
		if line.Translate != "commands.data.entity.query" || len(line.With) != 2 ||
			!textMatches(line.With[1], servertest.NBTArg("[I; 1, 2, 3, 4]")) {
			t.Errorf("Thrower query = %s, want UUID [I; 1, 2, 3, 4]", line.Plain())
		}
	}
	// Reading before any /data write catches a summon that only restores the
	// thrower when the shared saved-data reader is called later.
	t.Run("summoned", func(t *testing.T) { check(t, c) })
	srv.FlushMobs()
	c.Disconnect()
	srv.Shutdown()

	again, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	servertest.FreezeUntilHeld(t, again)
	client := connect().Join("Alice")
	servertest.HoldTicks(t, again)
	servertest.UntilTicks(t, again, 20, "saved item wakes", func() bool {
		return again.ItemCount() == 1
	})
	t.Run("reloaded", func(t *testing.T) { check(t, client) })
}
