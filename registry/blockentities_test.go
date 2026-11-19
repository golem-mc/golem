package registry

import (
	"strings"
	"testing"
)

// TestBlockEntityTypesAreBlocks pins the block-to-block-entity table: every
// key is a block the registry knows, every value is a registry name, and
// the pairs the setblock merge leans on answer the type a vanilla block
// entity carries its data under.
func TestBlockEntityTypesAreBlocks(t *testing.T) {
	for block, entityType := range BlockEntityTypes {
		if !strings.HasPrefix(block, "minecraft:") {
			t.Errorf("block %q is not a registry name", block)
		}
		if !strings.HasPrefix(entityType, "minecraft:") {
			t.Errorf("block entity type %q is not a registry name", entityType)
		}
		if _, ok := FindState(block, nil); !ok {
			t.Errorf("block entity type block %q is not a known block", block)
		}
	}

	// The pairs a command's NBT tag lands on: the pistons show a block
	// whose entity type is another block's name.
	pairs := map[string]string{
		"minecraft:spawner":       "minecraft:mob_spawner",
		"minecraft:trial_spawner": "minecraft:trial_spawner",
		"minecraft:oak_sign":      "minecraft:sign",
		"minecraft:player_head":   "minecraft:skull",
		"minecraft:moving_piston": "minecraft:piston",
	}
	for block, want := range pairs {
		if BlockEntityTypes[block] != want {
			t.Errorf("BlockEntityTypes[%q] = %q, want %q", block, BlockEntityTypes[block], want)
		}
	}

	// The piston is not its own block entity, so a tag on one must be
	// ignored, and the table must not map it.
	if _, ok := BlockEntityTypes["minecraft:piston"]; ok {
		t.Error("the piston is mapped to a block entity type, and vanilla gives it none")
	}
}

// TestUpdatePacketTypesMatchTheVanillaClasses pins which block entity types
// a command-placed block tells the clients about: the ones whose vanilla
// class overrides getUpdatePacket, and no others. The containers that were
// once listed as silent by name - trapped_chest is a type of its own, not
// the chest entry - are the interesting rows.
func TestUpdatePacketTypesMatchTheVanillaClasses(t *testing.T) {
	for _, sent := range []string{
		"minecraft:banner", "minecraft:beacon", "minecraft:brushable_block",
		"minecraft:campfire", "minecraft:conduit", "minecraft:copper_golem_statue",
		"minecraft:creaking_heart", "minecraft:decorated_pot", "minecraft:end_gateway",
		"minecraft:hanging_sign", "minecraft:jigsaw", "minecraft:mob_spawner",
		"minecraft:shelf", "minecraft:sign", "minecraft:skull",
		"minecraft:structure_block", "minecraft:test_block",
		"minecraft:test_instance_block", "minecraft:trial_spawner", "minecraft:vault",
	} {
		if !UpdatePacketBlockEntityTypes[sent] {
			t.Errorf("UpdatePacketBlockEntityTypes[%q] = false, want true", sent)
		}
	}
	for _, silent := range []string{
		"minecraft:barrel", "minecraft:beehive", "minecraft:bell", "minecraft:blast_furnace",
		"minecraft:brewing_stand", "minecraft:calibrated_sculk_sensor", "minecraft:chest",
		"minecraft:chiseled_bookshelf", "minecraft:command_block", "minecraft:comparator",
		"minecraft:crafter", "minecraft:daylight_detector", "minecraft:dispenser",
		"minecraft:dropper", "minecraft:enchanting_table", "minecraft:end_portal",
		"minecraft:ender_chest", "minecraft:furnace", "minecraft:hopper",
		"minecraft:jukebox", "minecraft:lectern", "minecraft:piston",
		"minecraft:sculk_catalyst", "minecraft:sculk_sensor", "minecraft:sculk_shrieker",
		"minecraft:shulker_box", "minecraft:smoker", "minecraft:trapped_chest",
	} {
		if UpdatePacketBlockEntityTypes[silent] {
			t.Errorf("UpdatePacketBlockEntityTypes[%q] = true, want false", silent)
		}
	}
}
