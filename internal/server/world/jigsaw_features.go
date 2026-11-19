package world

import (
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

// The village and abandoned camp pools' feature elements: placed features
// from the data generator's table, and the tree table's trees under the names
// the pools use.
func init() {
	for _, name := range []string{
		"pile_hay", "pile_ice", "pile_melon", "pile_pumpkin", "pile_snow",
		"flower_plain", "patch_taiga_grass", "patch_berry_bush", "patch_cactus",
	} {
		jigsawFeatures["minecraft:"+name] = lazyPlaced(func() featureFn {
			return nestedPlaced(parseJSON(levelgen.PlacedFeatureJSON(name)))
		})
	}
	for _, name := range []string{"oak", "spruce", "pine", "acacia"} {
		jigsawFeatures["minecraft:"+name] = lazyPlaced(func() featureFn {
			f := treePlaced("minecraft:"+name+"_checked", 0)
			return func(r *decorRegion, p [3]int32) bool { return f.run(r, p) }
		})
	}
	for _, name := range []string{
		"acacia_checked", "bamboo_in_structure", "birch_bees_002", "birch_checked", "cherry_bees_005",
		"cherry_checked", "fancy_oak_bees_002", "fancy_oak_checked", "jungle_tree", "mega_jungle_tree_checked",
		"mega_pine_checked", "mega_spruce_checked", "oak_checked", "orange_poplar", "pale_oak_checked",
		"pine_checked", "red_poplar", "spruce_checked", "spruce_on_snow", "super_birch_bees_0002", "yellow_poplar",
	} {
		jigsawFeatures["minecraft:"+name] = lazyPlaced(func() featureFn {
			f := treePlaced("minecraft:"+name, 0)
			return func(r *decorRegion, p [3]int32) bool { return f.run(r, p) }
		})
	}
}

// lazyPlaced compiles a feature on first use.
func lazyPlaced(build func() featureFn) func(*decorRegion, int32, int32, int32) bool {
	var once sync.Once
	var f featureFn
	return func(r *decorRegion, x, y, z int32) bool {
		once.Do(func() { f = build() })
		return f(r, [3]int32{x, y, z})
	}
}
