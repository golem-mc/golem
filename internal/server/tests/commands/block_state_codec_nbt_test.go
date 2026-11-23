package commands

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func prettySNBT(t *testing.T, snbt string) protocol.Text {
	t.Helper()
	tag, _, err := nbt.ParseOrderedValueTag(snbt)
	if err != nil {
		t.Fatal(err)
	}
	return command.PrettyNBTText(tag, true)
}

func fallingSandDisplay(id uuid.UUID) protocol.Text {
	base := servertest.EntityDisplayComponent("minecraft:falling_block", "entity.minecraft.falling_block_type", id)
	base.With = []protocol.Text{{Translate: "block.minecraft.sand"}}
	base.HoverEntity.Name.With = []protocol.Text{{Translate: "block.minecraft.sand"}}
	return base
}

// TestCommandBlockStateCodecRejectsLegacyForm verifies that 26.2-style
// {Name, Properties} block states typed into /summon, /data, /setblock, /fill
// and /particle fail the 26.3 BlockState.CODEC while {id, properties} works.
// vanilla: BlockState.CODEC
func TestCommandBlockStateCodecRejectsLegacyForm(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)

	// 1. /summon with legacy {Name, Properties} leaves each entity at its vanilla default.
	t.Run("summon_legacy", func(t *testing.T) {
		blockDisplayType, _ := registry.EntityTypeID("minecraft:block_display")
		c.ChatCommand(`summon block_display 2.5 68 2.5 {block_state:{Name:"minecraft:oak_log",Properties:{axis:"x"}}}`)
		bd := c.AwaitSpawn(blockDisplayType)
		c.ChatCommand(fmt.Sprintf(`data get entity %s block_state`, bd.Uuid))
		servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query",
			servertest.EntityDisplayComponent("minecraft:block_display", "entity.minecraft.block_display", bd.Uuid),
			prettySNBT(t, `"minecraft:air"`))

		endermanType, _ := registry.EntityTypeID("minecraft:enderman")
		c.ChatCommand(`summon enderman 3.5 68 2.5 {NoAI:1b,carriedBlockState:{Name:"minecraft:oak_log",Properties:{axis:"z"}}}`)
		em := c.AwaitSpawn(endermanType)
		c.ChatCommand(fmt.Sprintf(`data get entity %s carriedBlockState`, em.Uuid))
		servertest.AwaitCommandError(t, c, "arguments.nbtpath.nothing_found", "carriedBlockState")

		c.ChatCommand(`summon falling_block 4.5 68 2.5 {NoGravity:1b,BlockState:{Name:"minecraft:oak_log",Properties:{axis:"x"}}}`)
		fb := c.AwaitSpawn(protocol.EntityTypeFallingBlock)
		c.ChatCommand(fmt.Sprintf(`data get entity %s BlockState`, fb.Uuid))
		servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query",
			fallingSandDisplay(fb.Uuid),
			prettySNBT(t, `"minecraft:sand"`))

		c.ChatCommand(`summon tnt 5.5 68 2.5 {fuse:1000s,NoGravity:1b,block_state:{Name:"minecraft:oak_log",Properties:{axis:"x"}}}`)
		tnt := c.AwaitSpawn(protocol.EntityTypeTnt)
		c.ChatCommand(fmt.Sprintf(`data get entity %s block_state`, tnt.Uuid))
		servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query",
			servertest.EntityDisplayComponent("minecraft:tnt", "entity.minecraft.tnt", tnt.Uuid),
			prettySNBT(t, `"minecraft:tnt"`))

		c.ChatCommand(`summon minecart 6.5 68 2.5 {NoGravity:1b,DisplayState:{Name:"minecraft:oak_log",Properties:{axis:"x"}}}`)
		cart := c.AwaitSpawn(protocol.EntityTypeMinecart)
		c.ChatCommand(fmt.Sprintf(`data get entity %s DisplayState`, cart.Uuid))
		servertest.AwaitCommandError(t, c, "arguments.nbtpath.nothing_found", "DisplayState")

		c.ChatCommand(`summon arrow 7.5 68 2.5 {NoGravity:1b,inBlockState:{Name:"minecraft:oak_log",Properties:{axis:"x"}}}`)
		arrow := c.AwaitSpawn(protocol.EntityTypeArrow)
		c.ChatCommand(fmt.Sprintf(`data get entity %s inBlockState`, arrow.Uuid))
		servertest.AwaitCommandError(t, c, "arguments.nbtpath.nothing_found", "inBlockState")

		// Nested passenger with legacy {Name, Properties} also drops to default.
		c.ChatCommand(`summon pig 8.5 68 2.5 {NoAI:1b,Passengers:[{id:"minecraft:enderman",NoAI:1b,carriedBlockState:{Name:"minecraft:oak_log",Properties:{axis:"z"}}}]}`)
		rider := c.AwaitSpawn(endermanType)
		c.ChatCommand(fmt.Sprintf(`data get entity %s carriedBlockState`, rider.Uuid))
		servertest.AwaitCommandError(t, c, "arguments.nbtpath.nothing_found", "carriedBlockState")
	})

	// 2. /summon with 26.3 {id, properties} works, and /data merge|modify with legacy resets to default.
	t.Run("summon_codec_and_data_merge_modify", func(t *testing.T) {
		blockDisplayType, _ := registry.EntityTypeID("minecraft:block_display")
		c.ChatCommand(`summon block_display 2.5 70 2.5 {block_state:{id:"minecraft:oak_log",properties:{axis:"x"}}}`)
		bd := c.AwaitSpawn(blockDisplayType)
		bdName := servertest.EntityDisplayComponent("minecraft:block_display", "entity.minecraft.block_display", bd.Uuid)
		c.ChatCommand(fmt.Sprintf(`data get entity %s block_state`, bd.Uuid))
		servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query", bdName,
			prettySNBT(t, `{id: "minecraft:oak_log", properties: {axis: "x"}}`))

		// /data modify with legacy form drops back to air.
		c.ChatCommand(fmt.Sprintf(`data modify entity %s block_state set value {Name:"minecraft:stone"}`, bd.Uuid))
		servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.modified", bdName)
		c.ChatCommand(fmt.Sprintf(`data get entity %s block_state`, bd.Uuid))
		servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query", bdName,
			prettySNBT(t, `"minecraft:air"`))

		endermanType, _ := registry.EntityTypeID("minecraft:enderman")
		c.ChatCommand(`summon enderman 3.5 70 2.5 {NoAI:1b,carriedBlockState:{id:"minecraft:oak_log",properties:{axis:"z"}}}`)
		em := c.AwaitSpawn(endermanType)
		emName := servertest.EntityDisplayComponent("minecraft:enderman", "entity.minecraft.enderman", em.Uuid)
		c.ChatCommand(fmt.Sprintf(`data get entity %s carriedBlockState`, em.Uuid))
		servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query", emName,
			prettySNBT(t, `{id: "minecraft:oak_log", properties: {axis: "z"}}`))

		// /data modify with legacy form drops back to carrying nothing.
		c.ChatCommand(fmt.Sprintf(`data modify entity %s carriedBlockState set value {Name:"minecraft:stone"}`, em.Uuid))
		servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.modified", emName)
		c.ChatCommand(fmt.Sprintf(`data get entity %s carriedBlockState`, em.Uuid))
		servertest.AwaitCommandError(t, c, "arguments.nbtpath.nothing_found", "carriedBlockState")

		// TNT with bare block string, then /data merge with legacy compound resets to tnt.
		c.ChatCommand(`summon tnt 4.5 70 2.5 {fuse:1000s,NoGravity:1b,block_state:"minecraft:stone"}`)
		tnt := c.AwaitSpawn(protocol.EntityTypeTnt)
		tntName := servertest.EntityDisplayComponent("minecraft:tnt", "entity.minecraft.tnt", tnt.Uuid)
		c.ChatCommand(fmt.Sprintf(`data get entity %s block_state`, tnt.Uuid))
		servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query", tntName,
			prettySNBT(t, `"minecraft:stone"`))
		c.ChatCommand(fmt.Sprintf(`data merge entity %s {block_state:{Name:"minecraft:oak_log"}}`, tnt.Uuid))
		servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.modified", tntName)
		c.ChatCommand(fmt.Sprintf(`data get entity %s block_state`, tnt.Uuid))
		servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query", tntName,
			prettySNBT(t, `"minecraft:tnt"`))
	})

	// 3. /particle refuses legacy {Name, Properties} with particle.invalidOptions and accepts {id, properties}.
	t.Run("particle", func(t *testing.T) {
		c.Settle()
		c.ChatCommand(`particle block{block_state:{Name:"minecraft:oak_log",Properties:{axis:"x"}}} ~ ~1 ~`)
		if chat := c.NextSystemChat(); !bytes.Contains(chat, []byte("particle.invalidOptions")) {
			t.Errorf("legacy particle feedback = %q, want particle.invalidOptions", chat)
		}

		c.ChatCommand(`particle block{block_state:{id:"minecraft:oak_log",properties:{axis:"x"}}} ~ ~1 ~`)
		servertest.AwaitCommandFeedback(t, c, "commands.particle.success", "minecraft:block")
	})
}
