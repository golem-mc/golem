package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
)

// vanilla: Shulker.readAdditionalSaveData, setRawPeekAmount
func TestSummonedShulkerTakesFullDamageBeforeItsShellCloses(t *testing.T) {
	t.Parallel()
	// Generic bypasses armour; player_attack also checks that no covered
	// modifier reduces an armour-sensitive hit.
	for _, damageType := range []string{"minecraft:generic", "minecraft:player_attack"} {
		t.Run(damageType, func(t *testing.T) {
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("summon shulker 8.5 70 8.5 {NoAI:1b}")
			spawn := c.AwaitSpawn(shulkerType(t))
			servertest.RunTicks(t, srv, 1)
			c.Settle()
			c.ChatCommand("damage @e[type=shulker,limit=1] 2 " + damageType)
			c.Settle()
			c.ChatCommand("data get entity @e[type=shulker,limit=1] Health")
			c.ExpectCommandFeedbackText("commands.data.entity.query",
				servertest.EntityDisplayComponent("minecraft:shulker", "entity.minecraft.shulker", spawn.Uuid), servertest.NBTArg("28.0f"))
		})
	}
}
