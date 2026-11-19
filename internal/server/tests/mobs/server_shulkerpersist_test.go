package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"testing"
)

// TestAShulkerKeepsItsFaceAndPeekAcrossARestart exercises the entity file codec.
func TestAShulkerKeepsItsFaceAndPeekAcrossARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("summon minecraft:shulker 8.5 70 8.5 {NoAI:1b,AttachFace:4b,Peek:30b}")
	c.Settle()
	if ids := srv.MobIDsOfKind("minecraft:shulker"); len(ids) != 1 {
		t.Fatalf("summoned shulkers %v", ids)
	}
	srv.FlushMobs()
	srv.Shutdown()
	servertest.EntityFileIs(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, "minecraft:shulker", func(tag nbt.Compound) {
		if face, _ := nbt.ByteField(tag, "AttachFace"); face != 4 {
			t.Errorf("saved face %d, want 4", face)
		}
		if peek, _ := nbt.ByteField(tag, "Peek"); peek != 30 {
			t.Errorf("saved peek %d, want 30", peek)
		}
	})
	restarted := *cfg
	back, reconnect := servertest.Start(t, &restarted)
	servertest.FreezeUntilHeld(t, back)
	reconnect().Join("Keeper")
	servertest.HoldTicks(t, back)
	servertest.UntilTicks(t, back, 40, "shulker reloaded", func() bool { return len(back.MobIDsOfKind("minecraft:shulker")) > 0 })
	ids := back.MobIDsOfKind("minecraft:shulker")
	if len(ids) != 1 {
		t.Fatalf("reloaded shulkers %v", ids)
	}
	if face, peek, ok := back.ShulkerState(ids[0]); !ok || face != 4 || peek != 30 {
		t.Errorf("reloaded face/peek %d/%d (%v), want 4/30", face, peek, ok)
	}
}

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

// vanilla: Shulker.setRawPeekAmount, LivingEntity.addAdditionalSaveData
func TestShulkerSavedCoveredModifierSurvivesRestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon shulker 8.5 70 8.5 {NoAI:1b,attributes:[{id:"minecraft:armor",base:0d,modifiers:[{id:"minecraft:covered",amount:20d,operation:"add_value"}]}]}`)
	spawn := c.AwaitSpawn(shulkerType(t))
	servertest.RunTicks(t, srv, 1)
	c.Settle()
	srv.FlushMobs()
	srv.Shutdown()

	restarted := *cfg
	back, reconnect := servertest.Start(t, &restarted, servertest.OpsListing(t, "Keeper"))
	servertest.FreezeUntilHeld(t, back)
	reader := reconnect()
	reader.Join("Keeper")
	servertest.HoldTicks(t, back)
	servertest.UntilTicks(t, back, 40, "shulker reloaded", func() bool { return len(back.MobIDsOfKind("minecraft:shulker")) > 0 })
	servertest.RunTicks(t, back, 1)
	reader.Settle()
	reader.ChatCommand("damage @e[type=shulker,limit=1] 2 minecraft:player_attack")
	reader.Settle()
	reader.ChatCommand("data get entity @e[type=shulker,limit=1] Health")
	reader.ExpectCommandFeedbackText("commands.data.entity.query",
		servertest.EntityDisplayComponent("minecraft:shulker", "entity.minecraft.shulker", spawn.Uuid), servertest.NBTArg("29.52f"))
}
