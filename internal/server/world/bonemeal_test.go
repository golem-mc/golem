package world

import (
	"math/rand"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestBoneMealStatesResolve pins every block name the bone meal switches on
// or writes, with the property keys and values the code sets, to a state the
// registry actually has: a renamed block or property would otherwise turn a
// branch into a silent no-op.
func TestBoneMealStatesResolve(t *testing.T) {
	for _, c := range []struct {
		name  string
		props map[string]string
	}{
		{"minecraft:oak_sapling", map[string]string{"stage": "1"}},
		{"minecraft:oak_log", nil},
		{"minecraft:oak_leaves", nil},
		{"minecraft:oak_leaves", map[string]string{"distance": "1", "persistent": "false"}},
		{"minecraft:oak_leaves", map[string]string{"distance": "2", "persistent": "false"}},
		{"minecraft:oak_leaves", map[string]string{"distance": "3", "persistent": "false"}},
		{"minecraft:oak_leaves", map[string]string{"distance": "4", "persistent": "false"}},
		{"minecraft:oak_leaves", map[string]string{"distance": "5", "persistent": "false"}},
		{"minecraft:oak_leaves", map[string]string{"distance": "6", "persistent": "false"}},
		{"minecraft:oak_leaves", map[string]string{"waterlogged": "true"}},
		{"minecraft:birch_sapling", map[string]string{"stage": "1"}},
		{"minecraft:birch_log", nil},
		{"minecraft:birch_leaves", map[string]string{"distance": "1", "persistent": "false"}},
		{"minecraft:birch_leaves", map[string]string{"distance": "6", "persistent": "false"}},
		{"minecraft:birch_leaves", map[string]string{"waterlogged": "true"}},
		{"minecraft:spruce_sapling", map[string]string{"stage": "1"}},
		{"minecraft:spruce_log", nil},
		{"minecraft:spruce_leaves", map[string]string{"distance": "1", "persistent": "false"}},
		{"minecraft:spruce_leaves", map[string]string{"distance": "6", "persistent": "false"}},
		{"minecraft:spruce_leaves", map[string]string{"waterlogged": "true"}},
		{"minecraft:jungle_sapling", map[string]string{"stage": "1"}},
		{"minecraft:jungle_log", nil},
		{"minecraft:jungle_leaves", map[string]string{"distance": "1", "persistent": "false"}},
		{"minecraft:jungle_leaves", map[string]string{"distance": "6", "persistent": "false"}},
		{"minecraft:jungle_leaves", map[string]string{"waterlogged": "true"}},
		{"minecraft:acacia_sapling", map[string]string{"stage": "1"}},
		{"minecraft:acacia_log", nil},
		{"minecraft:acacia_leaves", map[string]string{"distance": "1", "persistent": "false"}},
		{"minecraft:acacia_leaves", map[string]string{"distance": "6", "persistent": "false"}},
		{"minecraft:acacia_leaves", map[string]string{"waterlogged": "true"}},
		{"minecraft:azalea", nil},
		{"minecraft:flowering_azalea", nil},
		{"minecraft:grass_block", nil},
		{"minecraft:short_grass", nil},
		{"minecraft:fern", nil},
		{"minecraft:tall_grass", map[string]string{"half": "lower"}},
		{"minecraft:large_fern", map[string]string{"half": "upper"}},
		{"minecraft:lilac", map[string]string{"half": "lower"}},
		{"minecraft:sweet_berry_bush", map[string]string{"age": "3"}},
		{"minecraft:cave_vines", map[string]string{"berries": "true"}},
		{"minecraft:cave_vines_plant", map[string]string{"berries": "true"}},
		{"minecraft:bush", nil},
		{"minecraft:firefly_bush", nil},
		{"minecraft:short_dry_grass", nil},
		{"minecraft:tall_dry_grass", nil},
		{"minecraft:seagrass", nil},
		{"minecraft:tall_seagrass", map[string]string{"half": "lower"}},
		{"minecraft:sea_pickle", map[string]string{"pickles": "4", "waterlogged": "true"}},
		{"minecraft:kelp", map[string]string{"age": "25"}},
		{"minecraft:kelp_plant", nil},
		{"minecraft:weeping_vines", map[string]string{"age": "25"}},
		{"minecraft:weeping_vines_plant", nil},
		{"minecraft:twisting_vines", map[string]string{"age": "25"}},
		{"minecraft:twisting_vines_plant", nil},
		{"minecraft:cocoa", map[string]string{"age": "2"}},
		{"minecraft:pumpkin_stem", map[string]string{"age": "7"}},
		{"minecraft:melon_stem", map[string]string{"age": "7"}},
		{"minecraft:pumpkin", nil},
		{"minecraft:melon", nil},
		{"minecraft:attached_pumpkin_stem", map[string]string{"facing": "north"}},
		{"minecraft:attached_melon_stem", map[string]string{"facing": "east"}},
		{"minecraft:torchflower_crop", map[string]string{"age": "1"}},
		{"minecraft:torchflower", nil},
		{"minecraft:pitcher_crop", map[string]string{"half": "lower", "age": "4"}},
		{"minecraft:pitcher_crop", map[string]string{"half": "upper", "age": "4"}},
		{"minecraft:pink_petals", map[string]string{"facing": "north", "flower_amount": "4"}},
		{"minecraft:wildflowers", map[string]string{"facing": "north", "flower_amount": "4"}},
		{"minecraft:small_dripleaf", map[string]string{"facing": "north", "half": "upper"}},
		{"minecraft:big_dripleaf", map[string]string{"facing": "north", "waterlogged": "true"}},
		{"minecraft:big_dripleaf_stem", map[string]string{"facing": "north", "waterlogged": "true"}},
		{"minecraft:rooted_dirt", nil},
		{"minecraft:hanging_roots", nil},
		{"minecraft:crimson_nylium", nil},
		{"minecraft:warped_nylium", nil},
		{"minecraft:netherrack", nil},
		{"minecraft:crimson_fungus", nil},
		{"minecraft:warped_fungus", nil},
		{"minecraft:brown_mushroom", nil},
		{"minecraft:red_mushroom", nil},
		{"minecraft:moss_block", nil},
		{"minecraft:pale_moss_block", nil},
		{"minecraft:mangrove_leaves", nil},
		{"minecraft:mangrove_propagule", map[string]string{"hanging": "true"}},
		{"minecraft:mangrove_propagule", map[string]string{"age": "4"}},
		{"minecraft:bamboo_sapling", nil},
		{"minecraft:bamboo", map[string]string{"age": "1", "leaves": "large", "stage": "1"}},
		{"minecraft:pale_moss_carpet", map[string]string{"bottom": "true", "north": "tall", "east": "low", "south": "none", "west": "none"}},
		{"minecraft:pale_hanging_moss", map[string]string{"tip": "true"}},
		{"minecraft:glow_lichen", map[string]string{
			"down": "true", "up": "true", "north": "true", "south": "true",
			"west": "true", "east": "true", "waterlogged": "true",
		}},
	} {
		// FindState falls back to the default state when the overrides do
		// not name a real one, so the resolution alone proves nothing: the
		// resolved state has to carry every key and value the code sets.
		id, ok := registry.FindState(c.name, c.props)
		if !ok {
			t.Errorf("the registry has no %s", c.name)
			continue
		}
		resolved, props, ok := registry.BlockStateName(id)
		if !ok || resolved != c.name {
			t.Errorf("%s resolved to %s", c.name, resolved)
			continue
		}
		got := registry.ParseStateProps(props)
		for k, v := range c.props {
			if got[k] != v {
				t.Errorf("%s carries %s=%s, want %s=%s", c.name, k, got[k], k, v)
			}
		}
	}
	for _, pair := range [][2]string{
		{"minecraft:farmland", "minecraft:supports_pumpkin_stem_fruit"},
		{"minecraft:farmland", "minecraft:supports_melon_stem_fruit"},
		{"minecraft:grass_block", "minecraft:supports_vegetation"},
		{"minecraft:oak_leaves", "minecraft:replaceable_by_trees"},
		{"minecraft:oak_log", "minecraft:logs"},
		{"minecraft:podzol", "minecraft:cannot_replace_below_tree_trunk"},
		{"minecraft:brain_coral_block", "minecraft:coral_blocks"},
	} {
		if !registry.BlockInTag(pair[0], pair[1]) {
			t.Errorf("%s is not in %s", pair[0], pair[1])
		}
	}
	for _, tag := range [2]string{"minecraft:wall_corals", "minecraft:underwater_bonemeals"} {
		members := registry.BlockTags[tag]
		if len(members) == 0 {
			t.Errorf("the tag %s is empty", tag)
		}
		for _, member := range members {
			if _, ok := registry.FindState(member, nil); !ok {
				t.Errorf("the tag %s holds %s, which the registry has no state for", tag, member)
			}
		}
	}
}

func TestBoneMealFlowerBedsCoverEveryFacingAndCount(t *testing.T) {
	for _, b := range []registry.Block{registry.BlockPinkPetals, registry.BlockWildflowers} {
		name := b.Name()
		r := rand.New(rand.NewSource(1))
		provider := flowerBed(b)
		seen := map[uint16]bool{}
		for range 256 {
			seen[provider(r, 0, 0, 0)] = true
		}
		if len(seen) != 16 {
			t.Fatalf("%s produced %d states, want 16", name, len(seen))
		}
		for _, facing := range []string{"north", "east", "south", "west"} {
			for amount := 1; amount <= 4; amount++ {
				want := registry.MustState(name, map[string]string{"facing": facing, "flower_amount": strconv.Itoa(amount)})
				if !seen[want] {
					t.Errorf("%s never produced facing=%s flower_amount=%d", name, facing, amount)
				}
			}
		}
	}
}

// TestBoneMealFlowerForestMatchesPlaceFeature pins the bone meal pick to what
// /place feature minecraft:flower_flower_forest puts on the same cell.
func TestBoneMealFlowerForestMatchesPlaceFeature(t *testing.T) {
	w, _ := newPhysicsWorld(t)
	kinds := map[uint16]bool{}
	for x := range int32(40) {
		w.SetBlock(x, 63, 3, registry.BlockGrassBlock.DefaultState())
		w.SetBlock(x, 64, 3, registry.BlockAir.DefaultState())
		seed := rand.New(rand.NewSource(int64(x))).Int63()
		want, _, ok := w.PlaceConfiguredFeature("minecraft:flower_flower_forest", seed, x, 64, 3)
		if !ok || len(want) != 1 {
			t.Fatalf("x=%d: /place feature changed %v (ok=%v), want one block", x, want, ok)
		}
		got := flowerForest(rand.New(rand.NewSource(int64(x))), x, 64, 3)
		if got != want[0].State {
			t.Errorf("x=%d: bone meal picks %d, the feature places %d", x, got, want[0].State)
		}
		kinds[got] = true
	}
	if len(kinds) < 2 {
		t.Errorf("40 cells gave %d flower kinds, want a mix across patches", len(kinds))
	}
}
