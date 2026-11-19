package worldgen

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server"

	"github.com/golem-mc/golem/registry"
)

// overworldSurfaceFollowsBiome checks the block each column of a seeded
// overworld is capped with against the overworld surface rules (pinned to
// vanilla's own classes by internal/server/world's surface golden): the old-growth
// taigas lay podzol, a dark forest is grass and dirt, and the stony shore is
// bare stone and gravel. The square of seed 12345 around x 208..303, z 80..127
// holds all three.
func overworldSurfaceFollowsBiome(t *testing.T, srv *server.Server) {
	ground := map[string]bool{
		"minecraft:grass_block": true, "minecraft:dirt": true, "minecraft:coarse_dirt": true,
		"minecraft:podzol": true, "minecraft:mycelium": true, "minecraft:sand": true,
		"minecraft:red_sand": true, "minecraft:sandstone": true, "minecraft:gravel": true,
		"minecraft:stone": true, "minecraft:snow_block": true, "minecraft:mud": true,
		"minecraft:clay": true, "minecraft:water": true, "minecraft:ice": true,
		"minecraft:packed_ice": true, "minecraft:powder_snow": true, "minecraft:calcite": true,
		"minecraft:deepslate": true, "minecraft:terracotta": true,
	}
	tops := map[string]map[string]int{}
	srv.ScanOverworldSurfaces(208, 80, 303, 127, 200, func(x, y, z int32, state uint16) bool {
		name, _, _ := registry.BlockStateName(state)
		if !ground[name] && !strings.HasSuffix(name, "_terracotta") {
			return true // a tree, plant or snow layer: look under it
		}
		biome := srv.OverworldBiomeAt(x, y, z)
		if tops[biome] == nil {
			tops[biome] = map[string]int{}
		}
		tops[biome][name]++
		return false
	})
	for _, biome := range []string{"minecraft:old_growth_pine_taiga", "minecraft:old_growth_spruce_taiga"} {
		if tops[biome]["minecraft:podzol"] == 0 {
			t.Errorf("%s lays no podzol: %v", biome, tops[biome])
		}
	}
	// Only disk_sand puts sand in a dark forest, in patches beside water.
	forest := tops["minecraft:dark_forest"]
	if forest["minecraft:sand"]*10 > forest["minecraft:grass_block"]+forest["minecraft:dirt"] || len(forest) == 0 {
		t.Errorf("dark_forest tops %v, want mostly grass and dirt", forest)
	}
	shore := tops["minecraft:stony_shore"]
	if len(shore) == 0 || shore["minecraft:stone"]+shore["minecraft:gravel"] < 3*(shore["minecraft:grass_block"]+shore["minecraft:dirt"]) {
		t.Errorf("stony_shore tops %v, want mostly stone and gravel", shore)
	}
}
