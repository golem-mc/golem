package registry_test

import (
	"slices"
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

// TestQualify covers the namespace a name gets: vanilla's ResourceLocation.parse
// defaults an absent or empty namespace to minecraft.
func TestQualify(t *testing.T) {
	for name, want := range map[string]string{
		"speed":           "minecraft:speed",
		":speed":          "minecraft:speed",
		"minecraft:speed": "minecraft:speed",
		"other:thing":     "other:thing",
	} {
		if got := registry.Qualify(name); got != want {
			t.Errorf("Qualify(%q) = %q, want %q", name, got, want)
		}
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

// TestFluidStates checks that the fluid count set covers the blocks the
// protocol counts as fluids: every state of water and lava, and every
// waterlogged state of any block — but not the dry states of the same block.
func TestFluidStates(t *testing.T) {
	// All 16 levels of water and lava are fluids.
	for _, name := range []string{"minecraft:water", "minecraft:lava"} {
		states, ok := registry.AllStates[name]
		if !ok {
			t.Fatalf("%s not in AllStates", name)
		}
		if len(states) != 16 {
			t.Errorf("%s has %d states, want 16", name, len(states))
		}
		for _, s := range states {
			if !registry.FluidStates[s.ID] {
				t.Errorf("state %d (%s %s) should be a fluid", s.ID, name, s.Props)
			}
		}
	}

	// A waterlogged state is a fluid; the same block's dry state is not.
	fence := registry.AllStates["minecraft:acacia_fence"]
	if len(fence) != 32 {
		t.Fatalf("acacia_fence has %d states, want 32", len(fence))
	}
	for _, s := range fence {
		if wet := registry.StateBool(s.ID, registry.PropWaterlogged); registry.FluidStates[s.ID] != wet {
			t.Errorf("acacia_fence state %d (waterlogged %v): fluid = %v", s.ID, wet, registry.FluidStates[s.ID])
		}
	}

	// A block with no waterlogged property and no fluid name is not a fluid.
	if registry.FluidStates[1] {
		t.Error("stone state 1 should not be a fluid")
	}
	if registry.FluidStates[0] {
		t.Error("air state 0 should not be a fluid")
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

// TestFenceDampening checks the dampening table's fence entries. Fences,
// walls and fence gates are not full cubes, so vanilla gives the dry states
// dampening 0: light passes straight through, and they seal no faces either.
// A waterlogged fence keeps the water's dampening of 1; stone is still a
// full block at 15.
func TestFenceDampening(t *testing.T) {
	for _, name := range []string{
		"minecraft:oak_fence", "minecraft:nether_brick_fence",
		"minecraft:cobblestone_wall", "minecraft:oak_fence_gate",
	} {
		state, ok := registry.BlockStates[name]
		if !ok {
			t.Errorf("%s not in BlockStates", name)
			continue
		}
		if got := registry.LightDampening(state); got != 0 {
			t.Errorf("LightDampening(%s) = %d, want 0", name, got)
		}
		wet, ok := registry.FindState(name, map[string]string{"waterlogged": "true"})
		if !ok {
			continue
		}
		_, wetProps, _ := registry.BlockStateName(wet)
		if !strings.Contains(wetProps, "waterlogged=true") {
			// oak_fence_gate has no waterlogged property, so
			// FindState fell back to the default state; nothing to check.
			continue
		}
		if got := registry.LightDampening(wet); got != 1 {
			t.Errorf("LightDampening(%s waterlogged) = %d, want 1", name, got)
		}
	}
	// A solid block (stone) still dampens fully.
	if got := registry.LightDampening(registry.BlockStates["minecraft:stone"]); got != 15 {
		t.Errorf("LightDampening(stone) = %d, want 15", got)
	}
}

// TestLightFacesSeal pins the face table's answers for the shapes whose faces
// behave in surprising ways: a bottom slab's underside is a full face and
// seals against anything below it, its top is empty, and two stacked bottom
// slabs seal against each other through the upper one's underside. States
// without shape occlusion (air, chests, torches) contribute empty faces, so
// they only seal against a full face, never against each other or against
// partial faces.
func TestLightFacesSeal(t *testing.T) {
	bottom := registry.BlockStates["minecraft:oak_slab"]
	top, ok := registry.FindState("minecraft:oak_slab", map[string]string{"type": "top"})
	if !ok {
		t.Fatal("no top oak_slab state")
	}
	chest := registry.BlockStates["minecraft:chest"]
	air := uint16(0)

	tests := []struct {
		desc     string
		from, to uint16
		dir      int
		want     bool
	}{
		// air's faces are all empty.
		{"air down to air", air, air, registry.LightDown, false},
		{"air down to chest", air, chest, registry.LightDown, false},
		{"chest down to air", chest, air, registry.LightDown, false},
		// A bottom slab's underside is full: it seals against anything below.
		{"bottom slab down to air", bottom, air, registry.LightDown, true},
		{"bottom slab down to chest", bottom, chest, registry.LightDown, true},
		{"bottom slab down to bottom slab", bottom, bottom, registry.LightDown, true},
		// Its top face is empty: it does not seal what sits on it.
		{"bottom slab up to air", bottom, air, registry.LightUp, false},
		{"air down to bottom slab", air, bottom, registry.LightDown, false},
		// A top slab's top face is full: it seals against anything above it.
		{"top slab up to air", top, air, registry.LightUp, true},
		// Its underside is empty: light passes out of the cell below it.
		{"top slab down to air", top, air, registry.LightDown, false},
		{"air up to top slab", air, top, registry.LightUp, false},
		// Two top slabs stacked: the upper cell is sealed from the lower one's
		// full top face.
		{"top slab down to top slab", top, top, registry.LightDown, true},
		// A bottom slab under a top slab: the touching faces are the top
		// slab's empty underside against the bottom slab's empty top, so
		// light passes between the two half slabs.
		{"top slab down to bottom slab", top, bottom, registry.LightDown, false},
		// Two bottom slabs stacked: the upper one's full underside seals.
		{"bottom slab down to bottom slab", bottom, bottom, registry.LightDown, true},
		// Partial faces do not seal against air, only against a union that
		// covers the plane. A stair's side face is partial.
		{"air east to stair", air, registry.BlockStates["minecraft:oak_stairs"], registry.LightEast, false},
	}
	for _, tc := range tests {
		if got := registry.LightFacesSeal(tc.from, tc.dir, tc.to); got != tc.want {
			t.Errorf("LightFacesSeal(%s): got %v, want %v", tc.desc, got, tc.want)
		}
	}
}

// TestFaceOcclusionFull pins which states fill a face: full cubes every way, a
// slab only the face it covers, and air, water and a chest none.
func TestFaceOcclusionFull(t *testing.T) {
	top, ok := registry.FindState("minecraft:oak_slab", map[string]string{"type": "top"})
	if !ok {
		t.Fatal("no top oak_slab state")
	}
	bottom := registry.BlockStates["minecraft:oak_slab"]
	tests := []struct {
		state uint16
		dir   int
		want  bool
	}{
		{registry.BlockStates["minecraft:stone"], registry.LightUp, true},
		{registry.BlockStates["minecraft:stone"], registry.LightEast, true},
		{0, registry.LightUp, false},
		{registry.BlockStates["minecraft:water"], registry.LightUp, false},
		{registry.BlockStates["minecraft:chest"], registry.LightUp, false},
		{bottom, registry.LightDown, true},
		{bottom, registry.LightUp, false},
		{top, registry.LightUp, true},
		{top, registry.LightDown, false},
	}
	for _, tc := range tests {
		if got := registry.FaceOcclusionFull(tc.state, tc.dir); got != tc.want {
			t.Errorf("FaceOcclusionFull(%d, %d) = %v, want %v", tc.state, tc.dir, got, tc.want)
		}
	}
}

// TestIsFence checks that IsFence recognises fences but not fence gates, walls
// or unrelated blocks, that IsFenceGate recognises fence gates, and that IsWall
// recognises walls but not wall-mounted blocks such as wall signs and wall
// torches.
func TestIsFence(t *testing.T) {
	for _, name := range []string{
		"minecraft:oak_fence", "minecraft:nether_brick_fence",
		"minecraft:acacia_fence", "minecraft:warped_fence",
	} {
		if !blockNamed(name).IsFence() {
			t.Errorf("IsFence(%s) = false, want true", name)
		}
	}
	for _, name := range []string{
		"minecraft:oak_fence_gate", "minecraft:acacia_fence_gate",
		"minecraft:cobblestone_wall", "minecraft:oak_log", "minecraft:stone",
	} {
		if blockNamed(name).IsFence() {
			t.Errorf("IsFence(%s) = true, want false", name)
		}
	}
	for _, name := range []string{
		"minecraft:oak_fence_gate", "minecraft:acacia_fence_gate",
		"minecraft:birch_fence_gate",
	} {
		if !blockNamed(name).IsFenceGate() {
			t.Errorf("IsFenceGate(%s) = false, want true", name)
		}
	}
	for _, name := range []string{
		"minecraft:oak_fence", "minecraft:cobblestone_wall",
		"minecraft:stone", "minecraft:oak_log",
	} {
		if blockNamed(name).IsFenceGate() {
			t.Errorf("IsFenceGate(%s) = true, want false", name)
		}
	}
	for _, name := range []string{
		"minecraft:cobblestone_wall", "minecraft:nether_brick_wall",
		"minecraft:andesite_wall",
	} {
		if !blockNamed(name).IsWall() {
			t.Errorf("IsWall(%s) = false, want true", name)
		}
	}
	for _, name := range []string{
		"minecraft:wall_sign", "minecraft:wall_torch",
		"minecraft:wall_banner", "minecraft:oak_fence", "minecraft:stone",
	} {
		if blockNamed(name).IsWall() {
			t.Errorf("IsWall(%s) = true, want false", name)
		}
	}
}

// TestConnectsToFence checks the vanilla FenceBlock.connectsTo logic: a fence
// connects to other fences of the same group (wooden to wooden, nether brick
// only to nether brick), fence gates from the sides, and blocks with
// sturdy faces (full-cube collision) that are not exceptions. Glass connects
// (it is sturdy); leaves, barriers, pumpkins and shulker boxes do not
// (exceptions); slabs, stairs, panes and chests do not (not sturdy). Fences do
// NOT connect to walls — vanilla FenceBlock.connectsTo has no wall check.
func TestConnectsToFence(t *testing.T) {
	const oak = "minecraft:oak_fence"
	const nether = "minecraft:nether_brick_fence"

	// Same-group fences connect from every direction.
	for _, fence := range []string{oak, nether} {
		for _, name := range []string{oak, nether} {
			state, ok := registry.BlockStates[name]
			if !ok {
				t.Errorf("%s not in BlockStates", name)
				continue
			}
			want := fence == name // same group iff same name (only 2 groups)
			for _, dir := range []string{"north", "south", "east", "west"} {
				if got := registry.ConnectsToFence(fenceBlock(t, fence), state, dir); got != want {
					t.Errorf("ConnectsToFence(%s, %s, %s) = %v, want %v", fence, name, dir, got, want)
				}
			}
		}
	}

	// Walls do NOT connect — vanilla FenceBlock.connectsTo has no wall
	// check (unlike IronBarsBlock.attachsTo and WallBlock.connectsTo).
	wallState := registry.BlockStates["minecraft:cobblestone_wall"]
	for _, dir := range []string{"north", "south", "east", "west"} {
		if registry.ConnectsToFence(registry.BlockOakFence, wallState, dir) {
			t.Errorf("ConnectsToFence(oak, wall, %s) = true, want false", dir)
		}
	}

	// Sturdy full-cube blocks connect; glass is the key case, transparent
	// but with a full-cube collision shape.
	for _, name := range []string{
		"minecraft:stone", "minecraft:glass", "minecraft:oak_planks",
		"minecraft:dirt", "minecraft:oak_log", "minecraft:ice",
		"minecraft:tinted_glass",
		// Full collision that the old name-based set got wrong.
		"minecraft:spawner", "minecraft:trial_spawner", "minecraft:vault",
		"minecraft:mangrove_roots", "minecraft:copper_grate",
		"minecraft:beacon", "minecraft:loom", "minecraft:chiseled_bookshelf",
		"minecraft:sculk_catalyst", "minecraft:tube_coral_block",
		// Partial collision, full support shape (getBlockSupportShape).
		"minecraft:mud", "minecraft:soul_sand",
	} {
		state, ok := registry.BlockStates[name]
		if !ok {
			t.Errorf("%s not in BlockStates", name)
			continue
		}
		for _, dir := range []string{"north", "south", "east", "west"} {
			if !registry.ConnectsToFence(registry.BlockOakFence, state, dir) {
				t.Errorf("ConnectsToFence(oak, %s, %s) = false, want true (sturdy)", name, dir)
			}
		}
	}

	// Exception blocks have sturdy faces but do NOT connect: leaves, barrier,
	// carved_pumpkin, jack_o_lantern, melon, pumpkin, shulker boxes.
	for _, name := range []string{
		"minecraft:oak_leaves", "minecraft:azalea_leaves",
		"minecraft:barrier", "minecraft:carved_pumpkin",
		"minecraft:jack_o_lantern", "minecraft:black_shulker_box",
	} {
		state, ok := registry.BlockStates[name]
		if !ok {
			t.Errorf("%s not in BlockStates", name)
			continue
		}
		for _, dir := range []string{"north", "south", "east", "west"} {
			if registry.ConnectsToFence(registry.BlockOakFence, state, dir) {
				t.Errorf("ConnectsToFence(oak, %s, %s) = true, want false (exception)", name, dir)
			}
		}
	}

	// Non-sturdy blocks (partial collision shapes) do not connect.
	for _, name := range []string{
		"minecraft:air", "minecraft:oak_slab",
		"minecraft:iron_bars", "minecraft:glass_pane",
		"minecraft:chest", "minecraft:torch",
		"minecraft:white_carpet",
		// Blocks that call noOcclusion but have non-full-cube collision,
		// so their faces are not sturdy. Confirmed against the decompiled
		// vanilla source.
		"minecraft:bush", "minecraft:dragon_egg",
		"minecraft:honey_block", "minecraft:light",
		"minecraft:pale_moss_carpet", "minecraft:sniffer_egg",
		"minecraft:sulfur_spike",
		// Partial collision that the old name-based set called full.
		"minecraft:moss_carpet",
	} {
		state, ok := registry.BlockStates[name]
		if !ok {
			t.Errorf("%s not in BlockStates", name)
			continue
		}
		for _, dir := range []string{"north", "south", "east", "west"} {
			if registry.ConnectsToFence(registry.BlockOakFence, state, dir) {
				t.Errorf("ConnectsToFence(oak, %s, %s) = true, want false (not sturdy)", name, dir)
			}
		}
	}

	// Water is in AllStates (16 levels), not BlockStates.
	waterStates := registry.AllStates["minecraft:water"]
	if len(waterStates) == 0 {
		t.Fatal("minecraft:water has no states")
	}
	for _, dir := range []string{"north", "south", "east", "west"} {
		if registry.ConnectsToFence(registry.BlockOakFence, waterStates[0].ID, dir) {
			t.Errorf("ConnectsToFence(oak, water, %s) = true, want false", dir)
		}
	}

	// A fence gate facing north (oriented along Z) connects only from east
	// and west — the sides where the posts are — not from north or south,
	// which are the front and back of the gate.
	gateNorth, ok := registry.FindState("minecraft:oak_fence_gate", map[string]string{"facing": "north"})
	if !ok {
		t.Fatal("oak_fence_gate facing=north not found")
	}
	if registry.ConnectsToFence(registry.BlockOakFence, gateNorth, "north") {
		t.Error("ConnectsToFence(oak, gate facing north, north) = true, want false (front)")
	}
	if registry.ConnectsToFence(registry.BlockOakFence, gateNorth, "south") {
		t.Error("ConnectsToFence(oak, gate facing north, south) = true, want false (back)")
	}
	if !registry.ConnectsToFence(registry.BlockOakFence, gateNorth, "east") {
		t.Error("ConnectsToFence(oak, gate facing north, east) = false, want true (side)")
	}
	if !registry.ConnectsToFence(registry.BlockOakFence, gateNorth, "west") {
		t.Error("ConnectsToFence(oak, gate facing north, west) = false, want true (side)")
	}

	// A fence gate facing east (oriented along X) connects only from north
	// and south.
	gateEast, ok := registry.FindState("minecraft:oak_fence_gate", map[string]string{"facing": "east"})
	if !ok {
		t.Fatal("oak_fence_gate facing=east not found")
	}
	if !registry.ConnectsToFence(registry.BlockOakFence, gateEast, "north") {
		t.Error("ConnectsToFence(oak, gate facing east, north) = false, want true (side)")
	}
	if !registry.ConnectsToFence(registry.BlockOakFence, gateEast, "south") {
		t.Error("ConnectsToFence(oak, gate facing east, south) = false, want true (side)")
	}
	if registry.ConnectsToFence(registry.BlockOakFence, gateEast, "east") {
		t.Error("ConnectsToFence(oak, gate facing east, east) = true, want false (front)")
	}
	if registry.ConnectsToFence(registry.BlockOakFence, gateEast, "west") {
		t.Error("ConnectsToFence(oak, gate facing east, west) = true, want false (back)")
	}
}

// TestIsBars checks that IsBars recognises iron bars, glass panes, stained
// glass panes, and copper bars (including waxed variants), and nothing else.
func TestIsBars(t *testing.T) {
	for _, name := range []string{
		"minecraft:iron_bars", "minecraft:glass_pane",
		"minecraft:black_stained_glass_pane", "minecraft:white_stained_glass_pane",
		"minecraft:copper_bars", "minecraft:exposed_copper_bars",
		"minecraft:waxed_copper_bars", "minecraft:waxed_oxidized_copper_bars",
	} {
		if !blockNamed(name).IsBars() {
			t.Errorf("IsBars(%s) = false, want true", name)
		}
	}
	for _, name := range []string{
		"minecraft:oak_fence", "minecraft:cobblestone_wall",
		"minecraft:glass", "minecraft:iron_block", "minecraft:stone",
	} {
		if blockNamed(name).IsBars() {
			t.Errorf("IsBars(%s) = true, want false", name)
		}
	}
}

// TestConnectsToBars checks the vanilla IronBarsBlock.attachsTo logic: bars
// connect to other bars (any kind), walls, and sturdy-faced blocks (not
// exceptions), but not to fences, fence gates, or non-sturdy blocks.
func TestConnectsToBars(t *testing.T) {
	const bars = "minecraft:iron_bars"

	// Other bars connect from every direction.
	for _, name := range []string{
		"minecraft:iron_bars", "minecraft:glass_pane",
		"minecraft:black_stained_glass_pane", "minecraft:copper_bars",
	} {
		state, ok := registry.BlockStates[name]
		if !ok {
			t.Errorf("%s not in BlockStates", name)
			continue
		}
		for _, dir := range []string{"north", "south", "east", "west"} {
			if !registry.ConnectsToBars(state, dir) {
				t.Errorf("ConnectsToBars(%s, %s, %s) = false, want true", bars, name, dir)
			}
		}
	}

	// Walls connect from every direction.
	wallState := registry.BlockStates["minecraft:cobblestone_wall"]
	for _, dir := range []string{"north", "south", "east", "west"} {
		if !registry.ConnectsToBars(wallState, dir) {
			t.Errorf("ConnectsToBars(%s, wall, %s) = false, want true", bars, dir)
		}
	}

	// Sturdy full-cube blocks connect (glass is the key case — transparent
	// but sturdy).
	for _, name := range []string{
		"minecraft:stone", "minecraft:glass", "minecraft:oak_planks",
		"minecraft:dirt", "minecraft:oak_log",
	} {
		state, ok := registry.BlockStates[name]
		if !ok {
			t.Errorf("%s not in BlockStates", name)
			continue
		}
		for _, dir := range []string{"north", "south", "east", "west"} {
			if !registry.ConnectsToBars(state, dir) {
				t.Errorf("ConnectsToBars(%s, %s, %s) = false, want true (sturdy)", bars, name, dir)
			}
		}
	}

	// Exception blocks do NOT connect.
	for _, name := range []string{
		"minecraft:oak_leaves", "minecraft:barrier",
		"minecraft:carved_pumpkin", "minecraft:black_shulker_box",
	} {
		state, ok := registry.BlockStates[name]
		if !ok {
			t.Errorf("%s not in BlockStates", name)
			continue
		}
		for _, dir := range []string{"north", "south", "east", "west"} {
			if registry.ConnectsToBars(state, dir) {
				t.Errorf("ConnectsToBars(%s, %s, %s) = true, want false (exception)", bars, name, dir)
			}
		}
	}

	// Fences and fence gates do NOT connect to bars.
	for _, name := range []string{
		"minecraft:oak_fence", "minecraft:nether_brick_fence",
		"minecraft:oak_fence_gate",
	} {
		state, ok := registry.BlockStates[name]
		if !ok {
			t.Errorf("%s not in BlockStates", name)
			continue
		}
		for _, dir := range []string{"north", "south", "east", "west"} {
			if registry.ConnectsToBars(state, dir) {
				t.Errorf("ConnectsToBars(%s, %s, %s) = true, want false (not bars)", bars, name, dir)
			}
		}
	}

	// Non-sturdy blocks do NOT connect.
	for _, name := range []string{
		"minecraft:air", "minecraft:oak_slab",
		"minecraft:chest", "minecraft:torch",
		"minecraft:bush", "minecraft:dragon_egg",
		"minecraft:honey_block",
	} {
		state, ok := registry.BlockStates[name]
		if !ok {
			t.Errorf("%s not in BlockStates", name)
			continue
		}
		for _, dir := range []string{"north", "south", "east", "west"} {
			if registry.ConnectsToBars(state, dir) {
				t.Errorf("ConnectsToBars(%s, %s, %s) = true, want false (not sturdy)", bars, name, dir)
			}
		}
	}
}

// TestConnectsToWall checks the vanilla WallBlock.connectsTo logic: walls
// connect to other walls, iron bars, fence gates (from the sides), and
// sturdy-faced blocks (not exceptions), but not to fences or non-sturdy blocks.
func TestConnectsToWall(t *testing.T) {
	const wall = "minecraft:cobblestone_wall"

	// Other walls connect from every direction.
	wallState := registry.BlockStates["minecraft:cobblestone_wall"]
	for _, dir := range []string{"north", "south", "east", "west"} {
		if !registry.ConnectsToWall(wallState, dir) {
			t.Errorf("ConnectsToWall(%s, wall, %s) = false, want true", wall, dir)
		}
	}

	// Iron bars and glass panes connect from every direction.
	for _, name := range []string{
		"minecraft:iron_bars", "minecraft:glass_pane",
		"minecraft:black_stained_glass_pane",
	} {
		state, ok := registry.BlockStates[name]
		if !ok {
			t.Errorf("%s not in BlockStates", name)
			continue
		}
		for _, dir := range []string{"north", "south", "east", "west"} {
			if !registry.ConnectsToWall(state, dir) {
				t.Errorf("ConnectsToWall(%s, %s, %s) = false, want true (bars)", wall, name, dir)
			}
		}
	}

	// Sturdy full-cube blocks connect.
	for _, name := range []string{
		"minecraft:stone", "minecraft:glass", "minecraft:oak_planks",
	} {
		state, ok := registry.BlockStates[name]
		if !ok {
			t.Errorf("%s not in BlockStates", name)
			continue
		}
		for _, dir := range []string{"north", "south", "east", "west"} {
			if !registry.ConnectsToWall(state, dir) {
				t.Errorf("ConnectsToWall(%s, %s, %s) = false, want true (sturdy)", wall, name, dir)
			}
		}
	}

	// Fence gates connect from the sides only.
	gateNorth, ok := registry.FindState("minecraft:oak_fence_gate", map[string]string{"facing": "north"})
	if !ok {
		t.Fatal("oak_fence_gate facing=north not found")
	}
	if registry.ConnectsToWall(gateNorth, "north") {
		t.Error("ConnectsToWall(gate facing north, north) = true, want false (front)")
	}
	if !registry.ConnectsToWall(gateNorth, "east") {
		t.Error("ConnectsToWall(gate facing north, east) = false, want true (side)")
	}

	// Exception blocks do NOT connect.
	for _, name := range []string{
		"minecraft:oak_leaves", "minecraft:barrier",
		"minecraft:carved_pumpkin", "minecraft:black_shulker_box",
	} {
		state, ok := registry.BlockStates[name]
		if !ok {
			t.Errorf("%s not in BlockStates", name)
			continue
		}
		for _, dir := range []string{"north", "south", "east", "west"} {
			if registry.ConnectsToWall(state, dir) {
				t.Errorf("ConnectsToWall(%s, %s, %s) = true, want false (exception)", wall, name, dir)
			}
		}
	}

	// Fences do NOT connect to walls (fences are not in WallBlock.connectsTo).
	for _, name := range []string{
		"minecraft:oak_fence", "minecraft:nether_brick_fence",
	} {
		state, ok := registry.BlockStates[name]
		if !ok {
			t.Errorf("%s not in BlockStates", name)
			continue
		}
		for _, dir := range []string{"north", "south", "east", "west"} {
			if registry.ConnectsToWall(state, dir) {
				t.Errorf("ConnectsToWall(%s, %s, %s) = true, want false (fence)", wall, name, dir)
			}
		}
	}

	// Non-sturdy blocks do NOT connect.
	for _, name := range []string{
		"minecraft:air", "minecraft:oak_slab",
		"minecraft:chest", "minecraft:torch", "minecraft:bush",
	} {
		state, ok := registry.BlockStates[name]
		if !ok {
			t.Errorf("%s not in BlockStates", name)
			continue
		}
		for _, dir := range []string{"north", "south", "east", "west"} {
			if registry.ConnectsToWall(state, dir) {
				t.Errorf("ConnectsToWall(%s, %s, %s) = true, want false (not sturdy)", wall, name, dir)
			}
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

// TestNetherBrickFenceGroup checks the WOODEN_FENCES half of vanilla's
// isSameFence: the nether brick fence is in FENCES but not WOODEN_FENCES, so it
// connects to itself and not to a wooden fence, and vice versa.
func TestNetherBrickFenceGroup(t *testing.T) {
	if blockNamed("minecraft:nether_brick_fence").IsWoodenFence() {
		t.Error("nether_brick_fence is in the wooden_fences tag, want false")
	}
	if !blockNamed("minecraft:nether_brick_fence").IsFence() {
		t.Error("nether_brick_fence is not in the fences tag")
	}
	if !blockNamed("minecraft:oak_fence").IsWoodenFence() {
		t.Error("oak_fence is not in the wooden_fences tag")
	}
	nether := registry.BlockStates["minecraft:nether_brick_fence"]
	oak := registry.BlockStates["minecraft:oak_fence"]
	for _, dir := range []string{"north", "south", "east", "west"} {
		if registry.ConnectsToFence(registry.BlockOakFence, nether, dir) {
			t.Errorf("oak fence connects to nether brick fence (%s), want false", dir)
		}
		if registry.ConnectsToFence(registry.BlockNetherBrickFence, oak, dir) {
			t.Errorf("nether brick fence connects to oak fence (%s), want false", dir)
		}
		if !registry.ConnectsToFence(registry.BlockNetherBrickFence, nether, dir) {
			t.Errorf("nether brick fence does not connect to itself (%s)", dir)
		}
	}
}

// TestFenceConnectsOnlyToSturdyFace checks that the face toward the fence
// decides: a north-facing stair is full on its north face only, so a fence
// connects when the stair is to its south and not when it is to its north.
func TestFenceConnectsOnlyToSturdyFace(t *testing.T) {
	stairs := registry.BlockStates["minecraft:oak_stairs"]
	for dir, want := range map[string]bool{"north": false, "south": true, "east": false, "west": false} {
		if got := registry.ConnectsToFence(registry.BlockOakFence, stairs, dir); got != want {
			t.Errorf("ConnectsToFence(oak, stairs, %s) = %v, want %v", dir, got, want)
		}
	}
	frosted := registry.AllStates["minecraft:frosted_ice"]
	if len(frosted) == 0 {
		t.Fatal("minecraft:frosted_ice has no states")
	}
	if !registry.ConnectsToFence(registry.BlockOakFence, frosted[0].ID, "north") {
		t.Error("fence does not connect to frosted ice, want true")
	}
}

// TestUndyedShulkerBoxIsException covers the block a name-suffix test
// misses: the undyed minecraft:shulker_box has no colour prefix, so a
// "_shulker_box" suffix check skips it even though it is in
// BlockTags.SHULKER_BOXES and vanilla's isExceptionForConnection therefore
// excludes it. It has a full-cube collision shape, so getting this wrong
// makes fences, walls and bars connect to it when vanilla leaves them
// unconnected.
func TestUndyedShulkerBoxIsException(t *testing.T) {
	for _, name := range []string{"minecraft:shulker_box", "minecraft:red_shulker_box"} {
		if !blockNamed(name).IsExceptionForConnection() {
			t.Errorf("IsExceptionForConnection(%s) = false, want true", name)
		}
		state := registry.BlockStates[name]
		if !registry.FaceSturdy(state, 2, registry.SupportFull) {
			t.Fatalf("%s is not sturdy-faced; the test no longer proves anything", name)
		}
		for _, dir := range []string{"north", "south", "east", "west"} {
			if registry.ConnectsToFence(registry.BlockOakFence, state, dir) {
				t.Errorf("fence connects to %s (%s), want false", name, dir)
			}
			if registry.ConnectsToWall(state, dir) {
				t.Errorf("wall connects to %s (%s), want false", name, dir)
			}
			if registry.ConnectsToBars(state, dir) {
				t.Errorf("bars connect to %s (%s), want false", name, dir)
			}
		}
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

// TestBarsAreNotTheBarsTag pins the one block group that must stay
// class-derived: minecraft:bars is a face-culling tag used by
// IronBarsBlock.skipRendering and holds only iron and copper bars, while
// attachsTo tests "instanceof IronBarsBlock", which covers glass panes too.
// Switching IsBars to the tag would silently stop every glass pane
// connecting. This reads BlockTags directly rather than going through a
// predicate, because minecraft:bars deliberately has none: the whole point
// is that no call site should be asking about that tag.
func TestBarsAreNotTheBarsTag(t *testing.T) {
	tagged := registry.BlockTags["minecraft:bars"]
	for _, name := range []string{"minecraft:glass_pane", "minecraft:black_stained_glass_pane"} {
		if slices.Contains(tagged, name) {
			t.Errorf("%s is in the minecraft:bars tag; the tag may now match IsBars", name)
		}
		if !blockNamed(name).IsBars() {
			t.Errorf("IsBars(%s) = false, want true", name)
		}
	}
	if !slices.Contains(tagged, "minecraft:iron_bars") {
		t.Error("iron_bars is not in the minecraft:bars tag")
	}
}

// TestBarsTagIsSubsetOfBarsBlocks is the guard the minecraft:bars tag can
// still provide even though BarsBlocks cannot be built from it. The tag is
// narrower than the class, but it is captured data rather than a
// hand-maintained list, so everything in it must be in BarsBlocks: a
// version that adds a bars block of a definition type missing from
// barsTypes in tools/gen/blocktables.go would otherwise slip through silently,
// and that block would stop connecting. The reverse containment is
// deliberately not asserted: glass panes are in BarsBlocks and in no tag at
// all - not minecraft:bars, not any other block tag, not any item tag -
// because vanilla groups them only through
// "StainedGlassPaneBlock extends IronBarsBlock".
func TestBarsTagIsSubsetOfBarsBlocks(t *testing.T) {
	tagged := registry.BlockTags["minecraft:bars"]
	if len(tagged) == 0 {
		t.Fatal("minecraft:bars is empty; the tag was renamed or dropped")
	}
	for _, name := range tagged {
		if !blockNamed(name).IsBars() {
			t.Errorf("%s is in minecraft:bars but IsBars(%s) = false; "+
				"barsTypes in tools/gen/blocktables.go is missing its definition type",
				name, name)
		}
	}
}

// TestIsDoublePlant pins the property-derived DoublePlantBlock family to the
// blocks whose report definition names that class or one built on it.
func TestIsDoublePlant(t *testing.T) {
	want := []string{
		"minecraft:large_fern", "minecraft:lilac", "minecraft:peony",
		"minecraft:pitcher_crop", "minecraft:pitcher_plant", "minecraft:rose_bush",
		"minecraft:small_dripleaf", "minecraft:sunflower", "minecraft:tall_grass",
		"minecraft:tall_seagrass",
	}
	var got []string
	for b := range registry.Block(registry.BlockCount) {
		if b.IsDoublePlant() {
			got = append(got, b.Name())
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("IsDoublePlant holds %v, want %v", got, want)
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
