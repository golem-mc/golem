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

func fallingBlockDisplay(id uuid.UUID, blockKey string) protocol.Text {
	base := servertest.EntityDisplayComponent("minecraft:falling_block", "entity.minecraft.falling_block_type", id)
	base.With = []protocol.Text{{Translate: blockKey}}
	base.HoverEntity.Name.With = []protocol.Text{{Translate: blockKey}}
	return base
}

// runCmd executes a command and uses a ping barrier to collect its immediate
// System Chat reply and any Spawn Entity packets without wall-clock sleeps.
func runCmd(t *testing.T, c *servertest.Client, cmd string) (chats [][]byte, spawns []struct {
	uuid uuid.UUID
	typ  int32
}) {
	t.Helper()
	c.ChatCommand(cmd)
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySystemChat:
			chats = append(chats, data)
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			_ = r.ReadVarInt() // entity ID
			uid := r.ReadUUID()
			typ := r.ReadVarInt()
			spawns = append(spawns, struct {
				uuid uuid.UUID
				typ  int32
			}{uuid: uid, typ: typ})
		}
	})
	return chats, spawns
}

func runSummon(t *testing.T, c *servertest.Client, cmd string, wantType int32) (uuid.UUID, []byte) {
	t.Helper()
	chats, spawns := runCmd(t, c, cmd)
	var uid uuid.UUID
	var found bool
	for _, s := range spawns {
		if s.typ == wantType {
			uid, found = s.uuid, true
			break
		}
	}
	if !found {
		t.Fatalf("%s: no spawn packet of type %d arrived (chats: %d)", cmd, wantType, len(chats))
	}
	if len(chats) != 1 {
		t.Fatalf("%s: got %d system chats, want 1", cmd, len(chats))
	}
	return uid, chats[0]
}

func runChat(t *testing.T, c *servertest.Client, cmd string) []byte {
	t.Helper()
	chats, _ := runCmd(t, c, cmd)
	if len(chats) != 1 {
		t.Fatalf("%s: got %d system chats, want 1", cmd, len(chats))
	}
	return chats[0]
}

func checkCommandError(t *testing.T, data []byte, key string, args ...string) {
	t.Helper()
	with := make([]protocol.Text, 0, len(args))
	for _, arg := range args {
		with = append(with, protocol.Text{Text: arg})
	}
	var want protocol.Buffer
	want.PutText(protocol.Text{Color: protocol.ColorRed,
		Extra: []protocol.Text{{Translate: key, With: with}}})
	want.PutBool(false)
	if !bytes.Equal(data, want.Bytes()) {
		t.Errorf("command error = % X, want % X", data, want.Bytes())
	}
}

// TestCommandBlockStateCodecRejectsLegacyForm verifies that 26.2-style
// {Name, Properties} block states typed into /summon, /data, /setblock, /fill
// and /particle fail the 26.3 BlockState.CODEC while {id, properties} works
// (and malformed properties default rather than dropping a valid modern id).
// vanilla: BlockState.CODEC
func TestCommandBlockStateCodecRejectsLegacyForm(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Tester", servertest.OpsListing(t, "Tester"))
	servertest.HoldTicks(t, srv)
	c.Settle()

	blockDisplayType, _ := registry.EntityTypeID("minecraft:block_display")
	endermanType, _ := registry.EntityTypeID("minecraft:enderman")

	// 1. /summon with legacy {Name, Properties} leaves each entity at its vanilla default.
	t.Run("summon_legacy", func(t *testing.T) {
		bd, _ := runSummon(t, c, `summon block_display 2.5 68 2.5 {block_state:{Name:"minecraft:oak_log",Properties:{axis:"x"}}}`, blockDisplayType)
		servertest.CheckCommandFeedback(t, runChat(t, c, fmt.Sprintf(`data get entity %s block_state`, bd)),
			"commands.data.entity.query",
			servertest.EntityDisplayComponent("minecraft:block_display", "entity.minecraft.block_display", bd),
			prettySNBT(t, `"minecraft:air"`))

		em, _ := runSummon(t, c, `summon enderman 3.5 68 2.5 {NoAI:1b,carriedBlockState:{Name:"minecraft:oak_log",Properties:{axis:"z"}}}`, endermanType)
		checkCommandError(t, runChat(t, c, fmt.Sprintf(`data get entity %s carriedBlockState`, em)),
			"arguments.nbtpath.nothing_found", "carriedBlockState")

		fb, fbChat := runSummon(t, c, `summon falling_block 4.5 68 2.5 {NoGravity:1b,BlockState:{Name:"minecraft:oak_log",Properties:{axis:"x"}}}`, protocol.EntityTypeFallingBlock)
		servertest.CheckCommandFeedback(t, fbChat, "commands.summon.success", fallingBlockDisplay(fb, "block.minecraft.sand"))
		servertest.CheckCommandFeedback(t, runChat(t, c, fmt.Sprintf(`data get entity %s BlockState`, fb)),
			"commands.data.entity.query",
			fallingBlockDisplay(fb, "block.minecraft.sand"),
			prettySNBT(t, `"minecraft:sand"`))

		tnt, _ := runSummon(t, c, `summon tnt 5.5 68 2.5 {fuse:1000s,NoGravity:1b,block_state:{Name:"minecraft:oak_log",Properties:{axis:"x"}}}`, protocol.EntityTypeTnt)
		servertest.CheckCommandFeedback(t, runChat(t, c, fmt.Sprintf(`data get entity %s block_state`, tnt)),
			"commands.data.entity.query",
			servertest.EntityDisplayComponent("minecraft:tnt", "entity.minecraft.tnt", tnt),
			prettySNBT(t, `"minecraft:tnt"`))

		cart, _ := runSummon(t, c, `summon minecart 6.5 68 2.5 {NoGravity:1b,DisplayState:{Name:"minecraft:oak_log",Properties:{axis:"x"}}}`, protocol.EntityTypeMinecart)
		checkCommandError(t, runChat(t, c, fmt.Sprintf(`data get entity %s DisplayState`, cart)),
			"arguments.nbtpath.nothing_found", "DisplayState")

		arrow, _ := runSummon(t, c, `summon arrow 7.5 68 2.5 {NoGravity:1b,inBlockState:{Name:"minecraft:oak_log",Properties:{axis:"x"}}}`, protocol.EntityTypeArrow)
		checkCommandError(t, runChat(t, c, fmt.Sprintf(`data get entity %s inBlockState`, arrow)),
			"arguments.nbtpath.nothing_found", "inBlockState")

		// Nested passenger with legacy {Name, Properties} also drops to default.
		rider, _ := runSummon(t, c, `summon pig 8.5 68 2.5 {NoAI:1b,Passengers:[{id:"minecraft:enderman",NoAI:1b,carriedBlockState:{Name:"minecraft:oak_log",Properties:{axis:"z"}}}]}`, endermanType)
		checkCommandError(t, runChat(t, c, fmt.Sprintf(`data get entity %s carriedBlockState`, rider)),
			"arguments.nbtpath.nothing_found", "carriedBlockState")
	})

	// 2. /summon with 26.3 {id, properties} (including malformed properties defaulting)
	// works, and /data merge|modify with legacy resets to default.
	t.Run("summon_codec_and_data_merge_modify", func(t *testing.T) {
		bd, _ := runSummon(t, c, `summon block_display 2.5 70 2.5 {block_state:{id:"minecraft:oak_log",properties:{axis:"x"}}}`, blockDisplayType)
		bdName := servertest.EntityDisplayComponent("minecraft:block_display", "entity.minecraft.block_display", bd)
		servertest.CheckCommandFeedback(t, runChat(t, c, fmt.Sprintf(`data get entity %s block_state`, bd)),
			"commands.data.entity.query", bdName,
			prettySNBT(t, `{id: "minecraft:oak_log", properties: {axis: "x"}}`))

		// /data modify with legacy form drops back to air.
		servertest.CheckCommandFeedback(t, runChat(t, c, fmt.Sprintf(`data modify entity %s block_state set value {Name:"minecraft:stone"}`, bd)),
			"commands.data.entity.modified", bdName)
		servertest.CheckCommandFeedback(t, runChat(t, c, fmt.Sprintf(`data get entity %s block_state`, bd)),
			"commands.data.entity.query", bdName,
			prettySNBT(t, `"minecraft:air"`))

		// Malformed properties field keeps the valid modern block id at its default state.
		bdDef, _ := runSummon(t, c, `summon block_display 2.5 72 2.5 {block_state:{id:"minecraft:oak_log",properties:1}}`, blockDisplayType)
		servertest.CheckCommandFeedback(t, runChat(t, c, fmt.Sprintf(`data get entity %s block_state`, bdDef)),
			"commands.data.entity.query",
			servertest.EntityDisplayComponent("minecraft:block_display", "entity.minecraft.block_display", bdDef),
			prettySNBT(t, `"minecraft:oak_log"`))

		fbDef, fbDefChat := runSummon(t, c, `summon falling_block 3.5 72 2.5 {NoGravity:1b,BlockState:{id:"minecraft:oak_log",properties:1}}`, protocol.EntityTypeFallingBlock)
		servertest.CheckCommandFeedback(t, fbDefChat, "commands.summon.success", fallingBlockDisplay(fbDef, "block.minecraft.oak_log"))
		servertest.CheckCommandFeedback(t, runChat(t, c, fmt.Sprintf(`data get entity %s BlockState`, fbDef)),
			"commands.data.entity.query",
			fallingBlockDisplay(fbDef, "block.minecraft.oak_log"),
			prettySNBT(t, `"minecraft:oak_log"`))

		em, _ := runSummon(t, c, `summon enderman 3.5 70 2.5 {NoAI:1b,carriedBlockState:{id:"minecraft:oak_log",properties:{axis:"z"}}}`, endermanType)
		emName := servertest.EntityDisplayComponent("minecraft:enderman", "entity.minecraft.enderman", em)
		servertest.CheckCommandFeedback(t, runChat(t, c, fmt.Sprintf(`data get entity %s carriedBlockState`, em)),
			"commands.data.entity.query", emName,
			prettySNBT(t, `{id: "minecraft:oak_log", properties: {axis: "z"}}`))

		// /data modify with legacy form drops back to carrying nothing.
		servertest.CheckCommandFeedback(t, runChat(t, c, fmt.Sprintf(`data modify entity %s carriedBlockState set value {Name:"minecraft:stone"}`, em)),
			"commands.data.entity.modified", emName)
		checkCommandError(t, runChat(t, c, fmt.Sprintf(`data get entity %s carriedBlockState`, em)),
			"arguments.nbtpath.nothing_found", "carriedBlockState")

		// TNT with bare block string, then /data merge with legacy compound resets to tnt.
		tnt, _ := runSummon(t, c, `summon tnt 4.5 70 2.5 {fuse:1000s,NoGravity:1b,block_state:"minecraft:stone"}`, protocol.EntityTypeTnt)
		tntName := servertest.EntityDisplayComponent("minecraft:tnt", "entity.minecraft.tnt", tnt)
		servertest.CheckCommandFeedback(t, runChat(t, c, fmt.Sprintf(`data get entity %s block_state`, tnt)),
			"commands.data.entity.query", tntName,
			prettySNBT(t, `"minecraft:stone"`))
		servertest.CheckCommandFeedback(t, runChat(t, c, fmt.Sprintf(`data merge entity %s {block_state:{Name:"minecraft:oak_log"}}`, tnt)),
			"commands.data.entity.modified", tntName)
		servertest.CheckCommandFeedback(t, runChat(t, c, fmt.Sprintf(`data get entity %s block_state`, tnt)),
			"commands.data.entity.query", tntName,
			prettySNBT(t, `"minecraft:tnt"`))

		// Arrow summoned with a valid inBlockState, then cleared by a legacy write.
		arrow, _ := runSummon(t, c, `summon arrow 5.5 70 2.5 {NoGravity:1b,inBlockState:{id:"minecraft:oak_log",properties:{axis:"x"}}}`, protocol.EntityTypeArrow)
		arrowName := servertest.EntityDisplayComponent("minecraft:arrow", "entity.minecraft.arrow", arrow)
		servertest.CheckCommandFeedback(t, runChat(t, c, fmt.Sprintf(`data get entity %s inBlockState`, arrow)),
			"commands.data.entity.query", arrowName,
			prettySNBT(t, `{id: "minecraft:oak_log", properties: {axis: "x"}}`))
		servertest.CheckCommandFeedback(t, runChat(t, c, fmt.Sprintf(`data modify entity %s inBlockState set value {Name:"minecraft:stone"}`, arrow)),
			"commands.data.entity.modified", arrowName)
		checkCommandError(t, runChat(t, c, fmt.Sprintf(`data get entity %s inBlockState`, arrow)),
			"arguments.nbtpath.nothing_found", "inBlockState")
	})

	// 3. Block-entity write paths (/data modify block, /setblock, /fill) reuse one
	// frozen moving piston in the same chunk.
	t.Run("moving_piston_block_writes", func(t *testing.T) {
		runChat(t, c, `setblock 2 66 2 piston[facing=east] replace`)
		runChat(t, c, `setblock 3 66 2 stone replace`)
		runChat(t, c, `setblock 2 66 1 redstone_block replace`)
		// Step only until the moving_piston block entity exists at (4, 66, 2).
		for step := range 4 {
			servertest.RunTicks(t, srv, 1)
			c.Settle()
			if chat := runChat(t, c, `data get block 4 66 2 blockState`); bytes.Contains(chat, []byte("commands.data.block.query")) {
				break
			}
			if step == 3 {
				t.Fatal("moving_piston block entity did not appear at (4, 66, 2)")
			}
		}

		dataPos := []protocol.Text{{Text: "4"}, {Text: "66"}, {Text: "2"}}
		setPos := []protocol.Text{servertest.NumberText(4), servertest.NumberText(66), servertest.NumberText(2)}
		query := func(wantSNBT string) {
			t.Helper()
			servertest.CheckCommandFeedback(t, runChat(t, c, `data get block 4 66 2 blockState`),
				"commands.data.block.query", dataPos[0], dataPos[1], dataPos[2], prettySNBT(t, wantSNBT))
		}

		// /data modify block: legacy resets to air, modern writes x-axis oak log.
		servertest.CheckCommandFeedback(t, runChat(t, c, `data modify block 4 66 2 blockState set value {Name:"minecraft:oak_log",Properties:{axis:"x"}}`),
			"commands.data.block.modified", dataPos...)
		query(`"minecraft:air"`)

		servertest.CheckCommandFeedback(t, runChat(t, c, `data modify block 4 66 2 blockState set value {id:"minecraft:oak_log",properties:{axis:"x"}}`),
			"commands.data.block.modified", dataPos...)
		query(`{id: "minecraft:oak_log", properties: {axis: "x"}}`)

		// /setblock on the same moving_piston state: legacy resets to air, modern writes x-axis oak log.
		servertest.CheckCommandFeedback(t, runChat(t, c, `setblock 4 66 2 moving_piston[facing=east,type=normal]{blockState:{Name:"minecraft:oak_log",Properties:{axis:"x"}}} replace`),
			"commands.setblock.success", setPos...)
		query(`"minecraft:air"`)

		servertest.CheckCommandFeedback(t, runChat(t, c, `setblock 4 66 2 moving_piston[facing=east,type=normal]{blockState:{id:"minecraft:oak_log",properties:{axis:"x"}}} replace`),
			"commands.setblock.success", setPos...)
		query(`{id: "minecraft:oak_log", properties: {axis: "x"}}`)

		// One-cell /fill on the same moving_piston state: legacy resets to air, modern writes x-axis oak log.
		servertest.CheckCommandFeedback(t, runChat(t, c, `fill 4 66 2 4 66 2 moving_piston[facing=east,type=normal]{blockState:{Name:"minecraft:oak_log",Properties:{axis:"x"}}} replace`),
			"commands.fill.success", servertest.NumberText(1))
		query(`"minecraft:air"`)

		servertest.CheckCommandFeedback(t, runChat(t, c, `fill 4 66 2 4 66 2 moving_piston[facing=east,type=normal]{blockState:{id:"minecraft:oak_log",properties:{axis:"x"}}} replace`),
			"commands.fill.success", servertest.NumberText(1))
		query(`{id: "minecraft:oak_log", properties: {axis: "x"}}`)
	})

	// 4. /particle refuses legacy {Name, Properties} with particle.invalidOptions and accepts {id, properties}.
	t.Run("particle", func(t *testing.T) {
		if chat := runChat(t, c, `particle block{block_state:{Name:"minecraft:oak_log",Properties:{axis:"x"}}} ~ ~1 ~`); !bytes.Contains(chat, []byte("particle.invalidOptions")) {
			t.Errorf("legacy particle feedback = %q, want particle.invalidOptions", chat)
		}

		servertest.CheckCommandFeedback(t, runChat(t, c, `particle block{block_state:{id:"minecraft:oak_log",properties:{axis:"x"}}} ~ ~1 ~`),
			"commands.particle.success", protocol.Text{Text: "minecraft:block"})
	})
}
