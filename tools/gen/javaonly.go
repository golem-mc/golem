package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

// These write a file straight from a Java program rather than parsing what it
// prints: the program takes the output path as its argument. The program
// writes to a scratch file and the runner compares or moves it into place, so
// -check does not touch the tree.

func init() {
	register(generator{name: "poplar-golden", doc: "internal/server/world/testdata/poplar_golden.txt: 26.3 poplar trunk and foliage placers", run: func(e *env) error {
		return runGoldenFile(e, "PoplarGolden", "internal/server/world/testdata/poplar_golden.txt")
	}})

	register(generator{name: "overworld-terrain-golden", doc: "internal/server/world/terrain/testdata/overworld_terrain_golden.txt: 26.3 terrain with real overworld biomes", run: func(e *env) error {
		return runGoldenFile(e, "OverworldTerrainGolden", "internal/server/world/terrain/testdata/overworld_terrain_golden.txt")
	}})

	register(generator{name: "column-golden", doc: "internal/server/world/terrain/testdata/column_golden.txt: 26.3 base noise columns and heights", run: func(e *env) error {
		return runGoldenFile(e, "ColumnGolden", "internal/server/world/terrain/testdata/column_golden.txt")
	}})

	register(generator{name: "state-provider-golden", doc: "internal/server/world/testdata/state_provider_golden.txt: 26.3 block-state providers and noise codecs", run: func(e *env) error {
		return runGoldenFile(e, "StateProviderGolden", "internal/server/world/testdata/state_provider_golden.txt")
	}})

	register(generator{name: "smallports-golden", doc: "internal/server/world/testdata/smallports_golden.txt: vanilla small structure placements and starts", run: func(e *env) error {
		return runGoldenFile(e, "SmallPortsGolden", "internal/server/world/testdata/smallports_golden.txt")
	}})

	register(generator{name: "jungletemple-golden", doc: "internal/server/world/testdata/jungletemple_golden.txt: vanilla jungle temple placement, traps and loot", run: func(e *env) error {
		return runGoldenFile(e, "JungleTempleGolden", "internal/server/world/testdata/jungletemple_golden.txt")
	}})

	register(generator{
		name: "ruinedportal-golden",
		doc:  "internal/server/world/testdata/overworld_portal_starts.txt: vanilla ruined portal pieces for every overworld setup",
		run: func(e *env) error {
			return runGoldenFile(e, "RuinedPortalGolden", "internal/server/world/testdata/overworld_portal_starts.txt")
		},
	})

	register(generator{
		name: "overworld-biome-golden",
		doc:  "internal/server/world/levelgen/testdata/overworld_biome_golden.txt: the vanilla server's overworld biome source",
		run: func(e *env) error {
			return runGoldenFile(e, "OverworldBiomeGolden", "internal/server/world/terrain/testdata/overworld_biome_golden.txt")
		},
	})
	register(generator{
		name: "sampler-golden",
		doc:  "internal/server/world/terrain/testdata/sampler_golden.txt: the 26.3 server's noise, density samplers, router, aquifer and fill",
		run: func(e *env) error {
			return runGoldenFile(e, "SamplerGolden", "internal/server/world/terrain/testdata/sampler_golden.txt")
		},
	})
	register(generator{
		name: "material-golden",
		doc:  "internal/server/world/terrain/testdata/material_golden.txt: the 26.3 server's material system on synthetic terrain",
		run: func(e *env) error {
			return runGoldenFile(e, "MaterialGolden", "internal/server/world/terrain/testdata/material_golden.txt")
		},
	})
	register(generator{
		name: "terrain-golden",
		doc:  "internal/server/world/terrain/testdata/terrain_golden.txt: the 26.3 server's fill, surface and carvers on whole chunks",
		run: func(e *env) error {
			return runGoldenFile(e, "TerrainGolden", "internal/server/world/terrain/testdata/terrain_golden.txt")
		},
	})
	register(generator{
		name: "overworld-features",
		doc:  "internal/server/world/terrain/overworld_features.txt: the overworld's placed features in FeatureSorter order",
		run: func(e *env) error {
			return runGoldenFile(e, "OverworldFeatureOrder", "internal/server/world/terrain/overworld_features.txt")
		},
	})
	register(generator{
		name: "sculk-golden",
		doc:  "internal/server/world/testdata/sculk_golden.txt: 26.3 sculk compositions and random draws",
		run: func(e *env) error {
			return runGoldenFile(e, "OverworldFeatureGolden", "internal/server/world/testdata/sculk_golden.txt", "", "sculk_patch_deep_dark,sculk_patch_ancient_city")
		},
	})

	register(generator{
		name: "overworld-feature-golden",
		doc:  "internal/server/world/testdata/overworld_feature_golden.txt: the vanilla placed features run in a synthetic world",
		run: func(e *env) error {
			return runGoldenFile(e, "OverworldFeatureGolden", "internal/server/world/testdata/overworld_feature_golden.txt")
		},
	})
	register(generator{
		name: "overworld-surface-golden",
		doc:  "internal/server/world/terrain/testdata/overworld_surface_golden.txt: the 26.3 material system on synthetic terrain",
		run: func(e *env) error {
			return runGoldenFile(e, "OverworldMaterialGolden", "internal/server/world/terrain/testdata/overworld_surface_golden.txt", filepath.Join(e.root, "tools/java/overworld_surface_cases.txt"))
		},
	})
	register(generator{
		name: "monument-starts-golden",
		doc:  "internal/server/world/testdata/monument_starts.txt: vanilla monument biome checks and saved buildings",
		run: func(e *env) error {
			return runGoldenFile(e, "MonumentStartsGolden", "internal/server/world/testdata/monument_starts.txt")
		},
	})
	register(generator{
		name: "monument-rooms-golden",
		doc:  "internal/server/world/testdata/monument_rooms_golden.txt: vanilla monument interior blocks, fluid ticks and random draws",
		run: func(e *env) error {
			return runGoldenFile(e, "MonumentRoomsGolden", "internal/server/world/testdata/monument_rooms_golden.txt")
		},
	})
	register(generator{
		name: "monument-exterior-golden",
		doc:  "internal/server/world/testdata/monument_exterior_golden.txt: vanilla monument exterior blocks and fluid ticks",
		run: func(e *env) error {
			return runGoldenFile(e, "MonumentExteriorGolden", "internal/server/world/testdata/monument_exterior_golden.txt")
		},
	})
	register(generator{
		name: "monument-golden",
		doc:  "internal/server/world/levelgen/testdata/monument_golden.txt: vanilla monument room graphs and fitted pieces",
		run: func(e *env) error {
			return runGoldenFile(e, "MonumentGolden", "internal/server/world/levelgen/testdata/monument_golden.txt")
		},
	})
	register(generator{
		name: "mansion-golden",
		doc:  "internal/server/world/levelgen/testdata/mansion_golden.txt: the vanilla mansion grids and room collections",
		run: func(e *env) error {
			return runGoldenFile(e, "MansionGolden", "internal/server/world/levelgen/testdata/mansion_golden.txt")
		},
	})
	register(generator{
		name: "beardifier-golden",
		doc:  "internal/server/world/terrain/testdata/beardifier_golden.txt: 26.3 beardifier kernel, point ranges and volumes",
		run: func(e *env) error {
			return runGoldenFile(e, "BeardifierGolden", "internal/server/world/terrain/testdata/beardifier_golden.txt")
		},
	})

	register(generator{
		name: "structure-golden",
		doc:  "internal/server/world/levelgen/testdata/structure_golden.txt: the vanilla server's structure sets and placements",
		run: func(e *env) error {
			return runGoldenFile(e, "StructureGolden", "internal/server/world/levelgen/testdata/structure_golden.txt")
		},
	})
	register(generator{
		name: "overworld-trees-golden",
		doc:  "internal/server/world/levelgen/testdata/overworld_trees_golden.txt: the vanilla server's tree features on a synthetic world",
		run: func(e *env) error {
			scratch, cleanup, err := scratchFile("gen-golden-")
			if err != nil {
				return err
			}
			defer cleanup()
			names := filepath.Join(e.root, "internal", "server", "world", "levelgen", "overworld_trees.txt")
			if _, err := e.java("OverworldTreeGolden", vanilla.JavaOptions{Xmx: "1g", Args: []string{scratch, names}}); err != nil {
				return err
			}
			data, err := os.ReadFile(scratch)
			if err != nil {
				return err
			}
			return e.emitBytes("internal/server/world/levelgen/testdata/overworld_trees_golden.txt", data)
		},
	})
	register(generator{
		name: "unicodenames",
		doc:  "nbt/unicodenames.txt.gz: the \\N{name} table, from the JDK's Character names",
		run:  runUnicodeNames,
	})
}

func scratchFile(pattern string) (string, func(), error) {
	dir, err := os.MkdirTemp("", pattern)
	if err != nil {
		return "", nil, err
	}
	return filepath.Join(dir, "out"), func() { os.RemoveAll(dir) }, nil
}

func runGoldenFile(e *env, helper, rel string, inputs ...string) error {
	scratch, cleanup, err := scratchFile("gen-golden-")
	if err != nil {
		return err
	}
	defer cleanup()
	if _, err := e.java(helper, vanilla.JavaOptions{Xmx: "1g", Args: append([]string{scratch}, inputs...)}); err != nil {
		return err
	}
	data, err := os.ReadFile(scratch)
	if err != nil {
		return fmt.Errorf("%s wrote no file: %w", helper, err)
	}
	return e.emitBytes(rel, data)
}

// The names come from the JDK the program runs on, not from the server, so
// there is no classpath.
func runUnicodeNames(e *env) error {
	if _, err := exec.LookPath("java"); err != nil {
		return fmt.Errorf("java is not on the PATH; the gen tasks provide it through mise (java = temurin-25)")
	}
	scratch, cleanup, err := scratchFile("gen-unicode-")
	if err != nil {
		return err
	}
	defer cleanup()
	cmd := exec.Command("java", filepath.Join(e.root, "tools", "java", "UnicodeNames.java"), scratch)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("UnicodeNames.java failed: %w\n%s", err, stderr.String())
	}
	data, err := os.ReadFile(scratch)
	if err != nil {
		return err
	}
	return e.emitBytes("nbt/unicodenames.txt.gz", data)
}
