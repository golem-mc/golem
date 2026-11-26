package entities

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: LlamaSpit, Projectile.addAdditionalSaveData, Projectile.readAdditionalSaveData
func TestLlamaSpitHasNoItemData(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Tester",
		servertest.OpsListing(t, "Tester"), func(srv *server.Server) {
			servertest.FreezeUntilHeld(t, srv)
		})
	servertest.HoldTicks(t, srv)
	const spit = "@e[type=minecraft:llama_spit,limit=1]"
	const item = `Item:{id:"minecraft:stone",count:1}`
	missingItem := servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "Item")

	c.ChatCommand("summon minecraft:llama_spit 4.5 70 4.5")
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.ChatCommand("data get entity " + spit + " Item")
	c.ExpectCommandFailure(missingItem)

	c.ChatCommand("data merge entity " + spit + " {" + item + "}")
	servertest.AwaitSystemChatSaying(t, c, "commands.data.entity.modified")
	c.ChatCommand("data get entity " + spit + " Item")
	c.ExpectCommandFailure(missingItem)

	c.ChatCommand(`summon minecraft:llama_spit 5.5 70 4.5 {Tags:["loaded"],` + item + "}")
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	c.ChatCommand("data get entity @e[type=minecraft:llama_spit,tag=loaded,limit=1] Item")
	c.ExpectCommandFailure(missingItem)
}

// vanilla: EntityType.LLAMA_SPIT, Projectile.addAdditionalSaveData
func TestLlamaSpitSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon minecraft:llama_spit 4.5 70.5 4.5 {NoGravity:1b}")
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	var saved nbt.Compound
	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{}, "minecraft:llama_spit", func(tag nbt.Compound) {
		saved = tag
		if _, ok := tag["Item"]; ok {
			t.Error("saved llama spit has an Item")
		}
	})
	c.Disconnect()
	srv.Shutdown()

	restarted := *cfg
	againSrv, connect := servertest.Start(t, &restarted, servertest.OpsListing(t, "Tester"))
	servertest.FreezeUntilHeld(t, againSrv)
	again := connect().Join("Tester")
	servertest.HoldTicks(t, againSrv)
	servertest.UntilTicks(t, againSrv, 20, "saved spit wakes", func() bool {
		return len(againSrv.SelectedEntities("minecraft:llama_spit")) == 1
	})
	servertest.RunTicks(t, againSrv, 2)
	again.ChatCommand("data get entity @e[type=minecraft:llama_spit,limit=1] Item")
	again.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "Item"))
	againSrv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{}, "minecraft:llama_spit", func(tag nbt.Compound) {
		if !reflect.DeepEqual(tag["UUID"], saved["UUID"]) || !reflect.DeepEqual(tag["Pos"], saved["Pos"]) {
			t.Errorf("restored UUID/Pos = %v %v, want %v %v", tag["UUID"], tag["Pos"], saved["UUID"], saved["Pos"])
		}
		if _, ok := tag["Item"]; ok {
			t.Error("restored llama spit has an Item")
		}
	})
}

// vanilla: LlamaSpit.tick applyGravity, Entity.isNoGravity, EntityType.LLAMA_SPIT
func TestLlamaSpitKeepsItsPlaceAcrossAChunkUnload(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Tester"))
	c := connect().Join("Tester")
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon minecraft:llama_spit 4.5 70.5 4.5 {NoGravity:1b}")
	servertest.AwaitSystemChatSaying(t, c, "commands.summon.success")
	servertest.RunTicks(t, srv, 2)
	var saved nbt.Compound
	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{}, "minecraft:llama_spit", func(tag nbt.Compound) { saved = tag })

	c.Disconnect()
	servertest.UntilTicks(t, srv, 400, "spit unloads", func() bool {
		return len(srv.SelectedEntities("minecraft:llama_spit")) == 0
	})
	connect().Join("Tester")
	servertest.UntilTicks(t, srv, 20, "spit wakes", func() bool {
		return len(srv.SelectedEntities("minecraft:llama_spit")) == 1
	})
	servertest.RunTicks(t, srv, 2)
	srv.FlushMobs()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{}, "minecraft:llama_spit", func(tag nbt.Compound) {
		if !reflect.DeepEqual(tag["UUID"], saved["UUID"]) || !reflect.DeepEqual(tag["Pos"], saved["Pos"]) {
			t.Errorf("reloaded UUID/Pos = %v %v, want %v %v", tag["UUID"], tag["Pos"], saved["UUID"], saved["Pos"])
		}
		if _, ok := tag["Item"]; ok {
			t.Error("reloaded llama spit has an Item")
		}
	})
}
