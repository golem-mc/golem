package registry_test

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func TestVersionMatchesProtocol(t *testing.T) {
	if registry.Version != protocol.GameVersion {
		t.Errorf("registry version %s does not match protocol version %s",
			registry.Version, protocol.GameVersion)
	}
}

// TestRequiredEntries covers the entries a vanilla client insists on before it
// accepts the packets that end configuration and start play. Without them it
// disconnects while joining, so a regenerated registry set has to keep them.
func TestRequiredEntries(t *testing.T) {
	required := map[string][]string{
		// Needed to send a valid Login (play) packet at all.
		"minecraft:dimension_type": {"minecraft:overworld"},
		// The biome unloaded chunks fall back to.
		"minecraft:worldgen/biome": {"minecraft:plains"},
		// Damage types the client resolves on joining.
		"minecraft:damage_type": {
			"minecraft:cactus", "minecraft:campfire", "minecraft:cramming",
			"minecraft:dragon_breath", "minecraft:drown", "minecraft:dry_out",
			"minecraft:ender_pearl", "minecraft:fall", "minecraft:fly_into_wall",
			"minecraft:freeze", "minecraft:generic", "minecraft:generic_kill",
			"minecraft:hot_floor", "minecraft:in_fire", "minecraft:in_wall",
			"minecraft:lava", "minecraft:lightning_bolt", "minecraft:magic",
			"minecraft:on_fire", "minecraft:out_of_world", "minecraft:outside_border",
			"minecraft:stalagmite", "minecraft:starve", "minecraft:sweet_berry_bush",
			"minecraft:wither",
		},
		// Backing the default values of item components.
		"minecraft:instrument": {"minecraft:ponder_goat_horn"},
		"minecraft:jukebox_song": {
			"minecraft:11", "minecraft:13", "minecraft:5", "minecraft:blocks",
			"minecraft:bounce", "minecraft:cat", "minecraft:chirp", "minecraft:creator",
			"minecraft:creator_music_box", "minecraft:far", "minecraft:lava_chicken",
			"minecraft:mall", "minecraft:mellohi", "minecraft:otherside",
			"minecraft:pigstep", "minecraft:precipice", "minecraft:relic",
			"minecraft:stal", "minecraft:strad", "minecraft:tears", "minecraft:wait",
			"minecraft:ward",
		},
		"minecraft:trim_material": {
			"minecraft:amethyst", "minecraft:copper", "minecraft:diamond",
			"minecraft:emerald", "minecraft:gold", "minecraft:iron", "minecraft:lapis",
			"minecraft:netherite", "minecraft:quartz", "minecraft:redstone",
			"minecraft:resin",
		},
		"minecraft:chicken_variant": {"minecraft:cold", "minecraft:temperate", "minecraft:warm"},
	}

	for name, entries := range required {
		reg, ok := registry.Get(name)
		if !ok {
			t.Errorf("registry %s is missing", name)
			continue
		}
		for _, entry := range entries {
			if reg.ID(entry) < 0 {
				t.Errorf("registry %s is missing entry %s", name, entry)
			}
		}
	}
}

// TestRequiredNonEmpty covers the registries the client needs at least one
// entry in.
func TestRequiredNonEmpty(t *testing.T) {
	for _, name := range []string{
		"minecraft:painting_variant", "minecraft:sulfur_cube_archetype",
		"minecraft:cat_variant", "minecraft:cat_sound_variant",
		"minecraft:chicken_sound_variant", "minecraft:cow_variant",
		"minecraft:cow_sound_variant", "minecraft:frog_variant",
		"minecraft:pig_variant", "minecraft:pig_sound_variant",
		"minecraft:wolf_variant", "minecraft:wolf_sound_variant",
		"minecraft:zombie_nautilus_variant",
	} {
		reg, ok := registry.Get(name)
		if !ok {
			t.Errorf("registry %s is missing", name)
			continue
		}
		if len(reg.Entries) == 0 {
			t.Errorf("registry %s is empty", name)
		}
	}
}

// TestRequiredTags covers the tags the client resolves before it accepts
// Finish Configuration.
func TestRequiredTags(t *testing.T) {
	required := map[string][]string{
		"minecraft:banner_pattern": {
			"minecraft:pattern_item/bordure_indented", "minecraft:pattern_item/creeper",
			"minecraft:pattern_item/field_masoned", "minecraft:pattern_item/flow",
			"minecraft:pattern_item/flower", "minecraft:pattern_item/globe",
			"minecraft:pattern_item/guster", "minecraft:pattern_item/mojang",
			"minecraft:pattern_item/piglin", "minecraft:pattern_item/skull",
		},
		"minecraft:damage_type": {
			"minecraft:bypasses_shield", "minecraft:is_explosion", "minecraft:is_fire",
		},
	}

	for name, tags := range required {
		var defined map[string]bool
		for _, tagged := range registry.Tags {
			if tagged.Registry != name {
				continue
			}
			defined = make(map[string]bool, len(tagged.Tags))
			for _, tag := range tagged.Tags {
				defined[tag.Name] = true
			}
		}
		if defined == nil {
			t.Errorf("no tags for registry %s", name)
			continue
		}
		for _, tag := range tags {
			if !defined[tag] {
				t.Errorf("registry %s is missing tag %s", name, tag)
			}
		}
	}
}

// TestBuiltinTagsPresent covers the tags of built-in registries. They are not
// optional: the client resolves them while loading the entries of synchronized
// registries out of its own data pack, and a missing one aborts the join with
// "Missing tag: '<name>' in '<registry>'". These three are the ones the first
// entries to be loaded depend on.
func TestBuiltinTagsPresent(t *testing.T) {
	required := map[string]string{
		"minecraft:block":       "minecraft:infiniburn_overworld",
		"minecraft:item":        "minecraft:enchantable/head_armor",
		"minecraft:entity_type": "minecraft:sensitive_to_bane_of_arthropods",
	}

	for name, tag := range required {
		var found bool
		for _, tagged := range registry.BuiltinTags {
			if tagged.Registry != name {
				continue
			}
			for _, defined := range tagged.Tags {
				if defined.Name == tag {
					found = true
					if len(defined.IDs) == 0 {
						t.Errorf("tag %s in %s is empty", tag, name)
					}
				}
			}
		}
		if !found {
			t.Errorf("registry %s is missing tag %s", name, tag)
		}
	}
}

// TestTagEntriesResolve makes sure every tag refers to entries that exist, as
// the client disconnects over a reference it cannot resolve.
func TestTagEntriesResolve(t *testing.T) {
	for _, tagged := range registry.Tags {
		reg, ok := registry.Get(tagged.Registry)
		if !ok {
			t.Errorf("tags for unknown registry %s", tagged.Registry)
			continue
		}
		for _, tag := range tagged.Tags {
			for _, entry := range tag.Entries {
				if reg.ID(entry) < 0 {
					t.Errorf("tag %s refers to unknown entry %s", tag.Name, entry)
				}
			}
		}
	}
}

// TestItemIDs pins the numeric IDs of a few items against the vanilla
// server's registry report, which is what says the generated table is the
// right way round and lines up with the wire.
func TestItemIDs(t *testing.T) {
	for name, want := range map[string]int32{
		"minecraft:stone":   1,
		"minecraft:diamond": 1012,
		"minecraft:potion":  1271,
	} {
		if got, ok := registry.ItemID(name); !ok || got != want {
			t.Errorf("ItemID(%s) = %d, %v; want %d, true", name, got, ok, want)
		}
		if got, ok := registry.ItemName(want); !ok || got != name {
			t.Errorf("ItemName(%d) = %q, %v; want %q, true", want, got, ok, name)
		}
	}

	// Air is the first item, so an ID of zero is a real item rather than the
	// absence of one; an empty slot is written as a count of zero instead.
	if got, ok := registry.ItemID("minecraft:air"); !ok || got != 0 {
		t.Errorf("ItemID(minecraft:air) = %d, %v; want 0, true", got, ok)
	}

	// Every name resolves back to the ID it was found under.
	for id, name := range registry.Items {
		if got, ok := registry.ItemID(name); !ok || got != int32(id) {
			t.Errorf("ItemID(%s) = %d, %v; want %d, true", name, got, ok, id)
		}
	}

	// An item this version does not have reads as absent rather than as some
	// other item, which is what stops a file written by a newer server from
	// putting the wrong thing in a player's hand.
	if _, ok := registry.ItemID("minecraft:something_from_a_later_version"); ok {
		t.Error("ItemID resolved an item that does not exist")
	}
	for _, id := range []int32{-1, int32(len(registry.Items))} {
		if name, ok := registry.ItemName(id); ok {
			t.Errorf("ItemName(%d) = %q, want nothing", id, name)
		}
	}
}

// TestBlockTagsResolved checks that the block tags survived the trip from
// the wire dump (numeric block IDs) through the block report (names). A tag
// resolved against the wrong report would come out plausible but wrong, so
// this pins the membership of the handful the block logic actually tests.
func TestBlockTagsResolved(t *testing.T) {
	for _, c := range []struct {
		tag  string
		want int
	}{
		{registry.TagFences, 14},
		{registry.TagWoodenFences, 13},
		{registry.TagWalls, 32},
		{registry.TagShulkerBoxes, 17},
		{registry.TagReplaceable, 30},
		{registry.TagFenceGates, 13},
		{registry.TagLeaves, 14},
	} {
		if got := len(registry.BlockTags[c.tag]); got != c.want {
			t.Errorf("BlockTags[%s] has %d members, want %d", c.tag, got, c.want)
		}
	}
	if !blockNamed("minecraft:oak_fence").IsFence() {
		t.Error("oak_fence is not in the fences tag")
	}
	if blockNamed("minecraft:oak_fence_gate").IsFence() {
		t.Error("oak_fence_gate is in the fences tag, want false")
	}
}

// TestClassSetsMatchTags guards the two substitutions that are only safe
// because a tag happens to list the same blocks as the Java class vanilla
// actually tests: IsFenceGate reads minecraft:fence_gates where vanilla
// tests "instanceof FenceGateBlock", IsLeaves reads minecraft:leaves where
// vanilla tests "instanceof LeavesBlock". Both hold today; if a version
// bump breaks either, connections would change silently, so fail here
// instead. The definition types on the right are what the blocks generator would
// have to list to derive the sets from the class instead, and are the
// fallback if this test ever goes red.
func TestClassSetsMatchTags(t *testing.T) {
	for _, c := range []struct {
		tag     string
		members []string
		in      func(registry.Block) bool
	}{
		{registry.TagFenceGates, []string{
			"minecraft:oak_fence_gate", "minecraft:bamboo_fence_gate",
			"minecraft:crimson_fence_gate", "minecraft:warped_fence_gate",
		}, registry.Block.IsFenceGate},
		{registry.TagLeaves, []string{
			"minecraft:oak_leaves", "minecraft:mangrove_leaves",
			"minecraft:azalea_leaves", "minecraft:cherry_leaves",
		}, registry.Block.IsLeaves},
	} {
		for _, name := range c.members {
			if !c.in(blockNamed(name)) {
				t.Errorf("%s is not in %s; the tag no longer matches the vanilla class",
					name, c.tag)
			}
		}
	}
	// The block that proves the tag is doing the work rather than a name
	// suffix: mangrove_leaves has its own definition type, and every fence
	// gate would be caught by a "_fence_gate" suffix too.
	if !blockNamed("minecraft:mangrove_leaves").IsLeaves() {
		t.Error("IsLeaves(mangrove_leaves) = false, want true")
	}
	if blockNamed("minecraft:leaf_litter").IsLeaves() {
		t.Error("IsLeaves(leaf_litter) = true, want false")
	}
}

func TestEverySpawnEggNamesAnEntityType(t *testing.T) {
	eggs := 0
	for i, item := range registry.Items {
		if !strings.HasSuffix(item, "_spawn_egg") {
			continue
		}
		eggs++
		if _, ok := registry.SpawnEggEntity(registry.Item(i)); !ok {
			t.Errorf("%s names no entity type", item)
		}
	}
	// Vanilla registers 88 spawn eggs.
	if eggs != 88 {
		t.Errorf("found %d spawn eggs, want 88", eggs)
	}
	if got, ok := registry.SpawnEggEntity(registry.ItemCowSpawnEgg); !ok || got != "minecraft:cow" {
		t.Errorf("cow egg = %q, %v", got, ok)
	}
	if _, ok := registry.SpawnEggEntity(registry.ItemEgg); ok {
		t.Error("a chicken's egg is no spawn egg")
	}
}

func fenceBlock(t *testing.T, name string) registry.Block {
	t.Helper()
	b, ok := registry.BlockByName(name)
	if !ok {
		t.Fatalf("%s is not a block", name)
	}
	return b
}

// blockNamed is the block a name stands for, or an ID past the last block for
// a name that is none, which no predicate accepts.
func blockNamed(name string) registry.Block {
	b, ok := registry.BlockByName(name)
	if !ok {
		return registry.Block(registry.BlockCount)
	}
	return b
}
