package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/registry"
)

// TestOverworldTreeSpecies sweeps a seeded world and asks each chunk for the
// species its own biome grows: the woods a birch among their oaks, the open
// country a fancy oak (a branch of its trunk standing off the column) and
// here and there a bee nest, and the snowy plains a spruce. A generator that
// plants the plain oak everywhere fails every one of them.
// vanilla: VegetationPlacements.TREES_BIRCH_AND_OAK_LEAF_LITTER, TREES_PLAINS,
// TREES_SNOWY; VegetationPlacements.java:290, 303, 312
func TestOverworldTreeSpecies(t *testing.T) {
	t.Parallel()

	var forestChunks, plainsChunks, snowyChunks int
	var birchInForest, fancyInPlains, nestsInForest, nestsInPlains, spruceInSnowy int
	// No single seed has woods, open country and snowy plains in reach of
	// the spawn, so the sweep pools three: forest (1), snowy plains (10),
	// plains (12).
	// The seeds sweep in parallel; the group returns once all three are counted.
	var mu sync.Mutex
	t.Run("seeds", func(t *testing.T) {
		for _, seed := range []string{"1", "10", "12"} {
			t.Run(seed, func(t *testing.T) {
				t.Parallel()
				cfg := config.Default()
				cfg.OnlineMode = false
				cfg.LevelType = "normal"
				cfg.LevelSeed = seed
				cfg.GenerateStructures = false

				srv, _ := servertest.Start(t, cfg)

				state := func(name string, props map[string]string) uint16 {
					s, ok := registry.FindState(name, props)
					if !ok {
						t.Fatalf("the block registry has no %s", name)
					}
					return s
				}
				oakLog := state("minecraft:oak_log", nil)
				birchLog := state("minecraft:birch_log", nil)
				spruceLog := state("minecraft:spruce_log", nil)
				// A trunk stands on dirt: TrunkPlacer.placeBelowTrunkBlock.
				dirt := state("minecraft:dirt", nil)
				beeNest := state("minecraft:bee_nest", map[string]string{"facing": "south"})

				// What a tree puts above its column, so the surface walk steps through
				// it to the ground the trunk stands on.
				grownOn := map[uint16]bool{
					oakLog: true, birchLog: true, spruceLog: true,
					state("minecraft:short_grass", nil): true,
					state("minecraft:snow", nil):        true,
				}
				for _, name := range []string{"minecraft:oak_leaves", "minecraft:birch_leaves", "minecraft:spruce_leaves"} {
					for d := 1; d <= 7; d++ {
						grownOn[state(name, map[string]string{"distance": strconv.Itoa(d)})] = true
					}
				}

				chunkOf := func(x, z int32) [2]int32 { return [2]int32{x >> 4, z >> 4} }
				const top = 140

				// A fancy oak's branch logs lie on their sides, axis x or z; a plain
				// blob oak's trunk and the spruce are bare columns of axis-y logs.
				oakLogX := state("minecraft:oak_log", map[string]string{"axis": "x"})
				oakLogZ := state("minecraft:oak_log", map[string]string{"axis": "z"})
				// A hive hangs beside a fancy oak's trunk under the canopy, so a branch
				// log can sit in its column: the walk has to step through those too.
				grownOn[oakLogX], grownOn[oakLogZ] = true, true

				type species struct {
					birch, spruce, fancy, nest bool
				}
				found := map[[2]int32]species{}
				biomes := map[[2]int32]string{}

				// These windows include birch, snowy spruce and the plains' fancy
				// oak and bee nest while retaining the minimum biome counts below.
				windows := map[string][][4]int32{
					"1":  {{16, 16, 4, 4}},
					"10": {{3, 0, 4, 4}},
					"12": {{4, 10, 5, 4}, {2, 22, 2, 2}},
				}[seed]
				for _, window := range windows {
					x0, z0 := window[0]*16, window[1]*16
					srv.ScanOverworldSurfaces(x0, z0, x0+window[2]*16-1, z0+window[3]*16-1, top, func(x, y, z int32, s uint16) bool {
						key := chunkOf(x, z)
						below := srv.BlockAt(x, y-1, z)
						sp := found[key]
						switch {
						case s == birchLog && below == dirt:
							sp.birch = true
						case s == spruceLog && below == dirt:
							sp.spruce = true
						case s == oakLogX || s == oakLogZ:
							sp.fancy = true
						case s == beeNest:
							sp.nest = true
						}
						found[key] = sp
						if x == key[0]*16+8 && z == key[1]*16+8 && !grownOn[s] {
							biomes[key] = srv.OverworldBiomeAt(x, y, z)
						}
						return grownOn[s]
					})
				}

				mu.Lock()
				defer mu.Unlock()
				for key, sp := range found {
					country := "other"
					switch biomes[key] {
					case "minecraft:snowy_plains":
						country = "snowy"
					case "minecraft:forest":
						country = "forest"
					case "minecraft:plains":
						country = "plains"
					}
					switch country {
					case "forest":
						forestChunks++
						if sp.birch {
							birchInForest++
						}
						if sp.nest {
							nestsInForest++
						}
					case "plains":
						plainsChunks++
						if sp.fancy {
							fancyInPlains++
						}
						if sp.nest {
							nestsInPlains++
						}
					case "snowy":
						snowyChunks++
						if sp.spruce {
							spruceInSnowy++
						}
					}
				}
			})
		}
	})
	t.Logf("forest %d chunks, %d with birch, %d with a nest", forestChunks, birchInForest, nestsInForest)
	t.Logf("plains %d chunks, %d with fancy oak, %d with a nest", plainsChunks, fancyInPlains, nestsInPlains)
	t.Logf("snowy plains %d chunks, %d with spruce", snowyChunks, spruceInSnowy)

	if forestChunks < 10 || plainsChunks < 20 || snowyChunks < 10 {
		t.Fatalf("the sweep found %d forest, %d plains and %d snowy-plains chunks, want enough to average over", forestChunks, plainsChunks, snowyChunks)
	}
	if birchInForest == 0 {
		t.Error("no birch in the woods, want about a fifth of their trees")
	}
	if fancyInPlains == 0 {
		t.Error("no fancy oak in the open country, want about a third of its trees")
	}
	if spruceInSnowy == 0 {
		t.Error("no spruce in the snowy plains, want the trees they grow")
	}
	if nestsInPlains == 0 {
		t.Error("no bee nest in the plains, want the odd tree to carry one")
	}
}
