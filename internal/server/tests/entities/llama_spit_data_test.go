package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
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
