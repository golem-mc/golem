package entities

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// vanilla: BlockState.CODEC, FallingBlockEntity.getTypeName
func TestFallingBlockStateCodecAndName(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))
	servertest.HoldTicks(t, srv)
	for i, tc := range []struct {
		block, input, output string
		state                uint16
	}{
		{"red_sand", `"minecraft:red_sand"`, `"minecraft:red_sand"`, registry.BlockRedSand.DefaultState()},
		{"oak_stairs", `{id:"minecraft:oak_stairs",properties:{facing:"east"}}`, `{id: "minecraft:oak_stairs", properties: {waterlogged: "false", half: "bottom", shape: "straight", facing: "east"}}`, registry.MustState("minecraft:oak_stairs", map[string]string{"facing": "east"})},
		{"oak_stairs", `{id:"minecraft:oak_stairs"}`, `"minecraft:oak_stairs"`, registry.BlockOakStairs.DefaultState()},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			c.ChatCommand(fmt.Sprintf(`summon falling_block 0.5 68 2.5 {NoGravity:1b,Tags:["case%d"],BlockState:%s}`, i, tc.input))
			spawn := c.AwaitFallingSpawn()
			if spawn.Data != int32(tc.state) {
				t.Errorf("spawn state = %d, want %d", spawn.Data, tc.state)
			}
			name := servertest.EntityDisplayComponent("minecraft:falling_block", "entity.minecraft.falling_block_type", spawn.Uuid)
			name.With = []protocol.Text{{Translate: "block.minecraft." + tc.block}}
			name.HoverEntity.Name.With = name.With
			servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", name)
			c.ChatCommand(fmt.Sprintf(`data get entity @e[tag=case%d,limit=1] BlockState`, i))
			servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query", name,
				blockStateFeedbackText(t, tc.output))
		})
	}
}

func TestEntityBlockStateCodecFields(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))
	servertest.HoldTicks(t, srv)
	for _, tc := range []struct {
		kind, field string
		typ         int32
	}{
		{"arrow", "inBlockState", protocol.EntityTypeArrow},
		{"tnt", "block_state", protocol.EntityTypeTnt},
		{"block_display", "block_state", protocol.EntityTypeBlockDisplay},
		{"enderman", "carriedBlockState", protocol.EntityTypeEnderman},
		{"minecart", "DisplayState", protocol.EntityTypeMinecart},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			c.ChatCommand(fmt.Sprintf(`summon %s 2.5 68 2.5 {NoAI:1b,NoGravity:1b,%s:"minecraft:oak_stairs"}`, tc.kind, tc.field))
			spawn := c.AwaitSpawn(tc.typ)
			if tc.kind == "tnt" {
				field := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexTNTBlockState)
				if field == nil || field.Typ != protocol.MetadataBlockState || field.VarintVal != int32(registry.BlockOakStairs.DefaultState()) {
					t.Errorf("TNT block metadata = %+v, want default oak stairs", field)
				}
			}
			if tc.kind == "block_display" {
				field := awaitFixtureMeta(c, spawn.EntityID)[23]
				if field.typ != protocol.MetadataBlockState || field.intV != int32(registry.BlockOakStairs.DefaultState()) {
					t.Fatalf("display block metadata = %+v, want default oak stairs", field)
				}
			}
			name := servertest.EntityDisplayComponent("minecraft:"+tc.kind, "entity.minecraft."+tc.kind, spawn.Uuid)
			c.ChatCommand(fmt.Sprintf(`data get entity %s %s`, spawn.Uuid, tc.field))
			servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query", name,
				servertest.FeedbackArgsForTest("commands.data.entity.query", []string{`"minecraft:oak_stairs"`})[0])
		})
	}
}

// vanilla: PrimedTnt.readAdditionalSaveData
func TestSummonTNTBlockStatePropertiesAndDefaults(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Summoner", servertest.OpsListing(t, "Summoner"))
	servertest.HoldTicks(t, srv)
	for _, tc := range []struct {
		name, field, output string
		state               uint16
		fuse                int
	}{
		{"properties", `block_state:{id:"minecraft:oak_stairs",properties:{facing:"east",half:"top"}}`,
			`{id: "minecraft:oak_stairs", properties: {waterlogged: "false", half: "top", shape: "straight", facing: "east"}}`,
			registry.MustState("minecraft:oak_stairs", map[string]string{"facing": "east", "half": "top"}), 80},
		{"absent", "", `"minecraft:tnt"`, registry.BlockTnt.DefaultState(), 1000},
		{"invalid", `block_state:"minecraft:missing_block"`, `"minecraft:tnt"`, registry.BlockTnt.DefaultState(), 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.ChatCommand(fmt.Sprintf(`summon tnt 2.5 68 2.5 {fuse:%ds,Pos:[9d,90d,9d],%s}`, tc.fuse, tc.field))
			spawn := c.AwaitSpawn(protocol.EntityTypeTnt)
			if spawn.X != 2.5 || spawn.Y != 68 || spawn.Z != 2.5 {
				t.Errorf("spawn position = (%g, %g, %g), want (2.5, 68, 2.5)", spawn.X, spawn.Y, spawn.Z)
			}
			meta := c.AwaitMetadataFor(spawn.EntityID)
			block := servertest.MetaOf(meta, protocol.MetadataIndexTNTBlockState)
			if tc.state == registry.BlockTnt.DefaultState() {
				if block != nil {
					t.Errorf("default TNT block metadata = %+v, want omitted", block)
				}
			} else if block == nil || block.Typ != protocol.MetadataBlockState || block.VarintVal != int32(tc.state) {
				t.Errorf("TNT block metadata = %+v, want state %d", block, tc.state)
			}
			name := servertest.EntityDisplayComponent("minecraft:tnt", "entity.minecraft.tnt", spawn.Uuid)
			c.ChatCommand(fmt.Sprintf(`data get entity %s block_state`, spawn.Uuid))
			servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query", name,
				blockStateFeedbackText(t, tc.output))
		})
	}
}

// vanilla: PrimedTnt.readAdditionalSaveData, StateDefinition.appendPropertyCodec
func TestDataTNTBlockStatePropertyFallback(t *testing.T) {
	t.Parallel()
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Stater", servertest.OpsListing(t, "Stater"))
	servertest.HoldTicks(t, srv)
	for _, tc := range []struct {
		name, props, half string
	}{
		{"unknown key", `facing:"east",half:"top",bogus:"true"`, "top"},
		{"invalid value", `facing:"east",half:"invalid"`, "bottom"},
		{"mixed", `facing:"east",half:"top",shape:"invalid",bogus:"true"`, "top"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.ChatCommand(`summon tnt 2.5 68 2.5 {NoGravity:1b,fuse:1000s}`)
			spawn := c.AwaitSpawn(protocol.EntityTypeTnt)
			c.AwaitMetadataFor(spawn.EntityID)
			name := servertest.EntityDisplayComponent("minecraft:tnt", "entity.minecraft.tnt", spawn.Uuid)
			c.ChatCommand(fmt.Sprintf(`data modify entity %s block_state set value {id:"minecraft:oak_stairs",properties:{%s}}`, spawn.Uuid, tc.props))
			servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.modified", name)
			meta := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexTNTBlockState)
			want := registry.MustState("minecraft:oak_stairs", map[string]string{"facing": "east", "half": tc.half})
			if meta == nil || meta.Typ != protocol.MetadataBlockState || meta.VarintVal != int32(want) {
				t.Errorf("block metadata = %+v, want state %d", meta, want)
			}
			c.ChatCommand(fmt.Sprintf(`data get entity %s block_state`, spawn.Uuid))
			output := fmt.Sprintf(`{id: "minecraft:oak_stairs", properties: {waterlogged: "false", half: "%s", shape: "straight", facing: "east"}}`, tc.half)
			servertest.AwaitCommandFeedbackText(t, c, "commands.data.entity.query", name,
				blockStateFeedbackText(t, output))
		})
	}
}

// blockStateFeedbackText retains the recorded vanilla compound traversal.
// waterlogged and half share a HashMap bucket; a plain map loses their order.
// vanilla: StateDefinition.appendPropertyCodec, TextComponentTagVisitor
func blockStateFeedbackText(t *testing.T, snbt string) protocol.Text {
	t.Helper()
	tag, _, err := nbt.ParseOrderedValueTag(snbt)
	if err != nil {
		t.Fatal(err)
	}
	return command.PrettyNBTText(tag, true)
}
