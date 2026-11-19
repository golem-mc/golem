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
