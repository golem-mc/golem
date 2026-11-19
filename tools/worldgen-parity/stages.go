package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// stageCategory attributes a block to the generation stage that places it,
// so that the wrong blocks of the whole range can be ranked by stage. The
// split is by block, not by an intermediate vanilla chunk status: a block
// of one kind that two stages can both place (stone from the noise and from
// an ore blob) lands in the category of the usual one.
type stageCategory struct {
	Name string
	// Stage is the pipeline step the blocks belong to.
	Stage string
}

// stageCategories lists the categories in the order the report prints them.
var stageCategories = []stageCategory{
	{"air", "terrain shape (noise, aquifers, carvers)"},
	{"stone", "terrain shape (noise, aquifers, carvers)"},
	{"water", "terrain shape (noise, aquifers, carvers)"},
	{"lava", "terrain shape (noise, aquifers, carvers)"},
	{"bedrock", "surface rules"},
	{"deepslate", "surface rules"},
	{"surface layer", "surface rules"},
	{"ice and snow", "surface rules and decoration"},
	{"ore", "decoration: ores"},
	{"ore blob", "decoration: ores (blobs)"},
	{"log and leaves", "decoration: trees"},
	{"plant", "decoration: vegetation"},
	{"underground feature", "decoration: underground"},
	{"structure block", "structures"},
	{"other", "other"},
}

var categoryIndex = func() map[string]int {
	m := map[string]int{}
	for i, c := range stageCategories {
		m[c.Name] = i
	}
	return m
}()

// categoryOf sorts a block name into a stageCategories entry.
func categoryOf(name string) string {
	name = strip(name)
	switch name {
	case "air", "cave_air", "void_air":
		return "air"
	case "stone":
		return "stone"
	case "water", "bubble_column":
		return "water"
	case "lava":
		return "lava"
	case "bedrock":
		return "bedrock"
	case "deepslate":
		return "deepslate"
	case "granite", "diorite", "andesite", "tuff", "gravel", "clay", "infested_stone", "infested_deepslate":
		return "ore blob"
	case "grass_block", "dirt", "coarse_dirt", "podzol", "mycelium", "rooted_dirt", "mud", "sand", "red_sand",
		"sandstone", "red_sandstone", "terracotta", "packed_mud", "dirt_path", "moss_block", "pale_moss_block":
		return "surface layer"
	case "snow", "snow_block", "powder_snow", "ice", "packed_ice", "blue_ice", "frosted_ice":
		return "ice and snow"
	case "dripstone_block", "pointed_dripstone", "calcite", "smooth_basalt", "amethyst_block", "budding_amethyst",
		"small_amethyst_bud", "medium_amethyst_bud", "large_amethyst_bud", "amethyst_cluster", "glow_lichen",
		"moss_carpet", "pale_moss_carpet", "cave_vines", "cave_vines_plant", "big_dripleaf", "big_dripleaf_stem",
		"small_dripleaf", "spore_blossom", "azalea", "flowering_azalea", "hanging_roots", "sculk", "sculk_vein",
		"sculk_catalyst", "sculk_shrieker", "sculk_sensor":
		return "underground feature"
	case "cobblestone", "mossy_cobblestone", "stone_bricks", "mossy_stone_bricks", "cracked_stone_bricks",
		"chiseled_stone_bricks", "oak_planks", "spruce_planks", "birch_planks", "dark_oak_planks", "oak_fence",
		"torch", "wall_torch", "chest", "spawner", "rail", "cobweb", "iron_bars", "glass_pane", "oak_door",
		"stone_brick_stairs", "cobblestone_stairs", "oak_stairs", "end_portal_frame", "end_portal",
		"cobblestone_wall", "prismarine", "prismarine_bricks", "dark_prismarine", "sea_lantern", "sponge", "wet_sponge", "gold_block",
		"smooth_sandstone", "cut_sandstone", "chiseled_sandstone", "sandstone_stairs", "sandstone_slab":
		return "structure block"
	}
	switch {
	case strings.HasSuffix(name, "_ore") || strings.HasPrefix(name, "raw_"):
		return "ore"
	case strings.HasSuffix(name, "_log") || strings.HasSuffix(name, "_wood") || strings.HasSuffix(name, "_leaves"),
		name == "mangrove_roots" || name == "muddy_mangrove_roots" || name == "bee_nest":
		return "log and leaves"
	case strings.HasSuffix(name, "_terracotta"):
		return "surface layer"
	}
	if isPlant(name) {
		return "plant"
	}
	return "other"
}

func isPlant(name string) bool {
	switch name {
	case "short_grass", "tall_grass", "fern", "large_fern", "dead_bush", "seagrass", "tall_seagrass", "kelp", "kelp_plant",
		"vine", "sugar_cane", "cactus", "bamboo", "bamboo_sapling", "lily_pad", "pumpkin", "melon", "sweet_berry_bush",
		"brown_mushroom", "red_mushroom", "dandelion", "poppy", "blue_orchid", "allium", "azure_bluet", "red_tulip",
		"orange_tulip", "white_tulip", "pink_tulip", "oxeye_daisy", "cornflower", "lily_of_the_valley", "sunflower",
		"lilac", "rose_bush", "peony", "torchflower", "pitcher_plant", "wildflowers", "leaf_litter", "firefly_bush",
		"bush", "cactus_flower", "short_dry_grass", "tall_dry_grass", "brown_mushroom_block", "red_mushroom_block",
		"mushroom_stem", "sea_pickle", "brain_coral", "tube_coral", "bubble_coral", "fire_coral", "horn_coral",
		"brain_coral_fan", "tube_coral_fan", "bubble_coral_fan", "fire_coral_fan", "horn_coral_fan", "mangrove_propagule",
		"closed_eyeblossom", "open_eyeblossom", "pink_petals", "oak_sapling", "birch_sapling", "spruce_sapling":
		return true
	}
	return strings.HasPrefix(name, "potted_")
}

// categoryCache is categoryOf memoized over every state ID.
var categoryCache = func() []uint8 {
	out := make([]uint8, 1<<16)
	for i := range out {
		out[i] = uint8(categoryIndex[categoryOf(blockName(uint16(i)))])
	}
	return out
}()

// stageStat counts one category over the range.
type stageStat struct {
	Category string `json:"category"`
	Stage    string `json:"stage"`
	// Vanilla and Ours are the blocks of the category on each side; Both is
	// those at the same position with the same state.
	Vanilla int `json:"vanilla"`
	Ours    int `json:"ours"`
	Both    int `json:"both"`
}

// Wrong is the blocks of the category that are in the wrong place or absent
// on one side: each is counted once, on the side that has it.
func (s stageStat) Wrong() int { return s.Vanilla + s.Ours - 2*s.Both }

// biomeStat is one biome over the range, in 4x4x4 cells.
type biomeStat struct {
	Biome   string `json:"biome"`
	Vanilla int    `json:"vanilla"`
	Ours    int    `json:"ours"`
	Both    int    `json:"both"`
}

// stageReport is the per-stage breakdown of one comparison.
type stageReport struct {
	Stages []stageStat `json:"stages"`
	Biomes []biomeStat `json:"biomes"`
	// Structures counts the vanilla structure starts in the range by type,
	// and OurStructures ours.
	Structures    map[string]int `json:"structures"`
	OurStructures map[string]int `json:"our_structures"`
	// SurfaceBlocks counts, over columns whose highest solid block is the
	// same Y on both sides, how often that block is the same.
	SurfaceBlocks ratio `json:"surface_blocks"`
	// Underground counts the air blocks between each column's own surface
	// and 8 blocks below it, to tell a caved hillside from open air: the
	// vanilla count of air below the surface is the volume noise caves and
	// carvers have to reproduce.
	UndergroundAir statCount `json:"underground_air"`
}

// statCount is a Vanilla/Ours/Both triple.
type statCount struct {
	Vanilla int `json:"vanilla"`
	Ours    int `json:"ours"`
	Both    int `json:"both"`
}

func stageReportOf(d dimension, vanilla, ours map[[2]int32]*chunkData) *stageReport {
	rep := &stageReport{Structures: map[string]int{}, OurStructures: map[string]int{}}
	cats := make([]stageStat, len(stageCategories))
	for i, c := range stageCategories {
		cats[i] = stageStat{Category: c.Name, Stage: c.Stage}
	}
	biomes := map[int32]*biomeStat{}
	biome := func(id int32) *biomeStat {
		b := biomes[id]
		if b == nil {
			b = &biomeStat{Biome: biomeName(id)}
			biomes[id] = b
		}
		return b
	}
	for key, v := range vanilla {
		o := ours[key]
		if o == nil {
			continue
		}
		for _, name := range v.Structures {
			rep.Structures[name]++
		}
		for _, name := range o.Structures {
			rep.OurStructures[name]++
		}
		for i, vs := range v.Blocks {
			os := o.Blocks[i]
			vc, oc := categoryCache[vs], categoryCache[os]
			cats[vc].Vanilla++
			cats[oc].Ours++
			if vs == os || blockName(vs) == blockName(os) {
				cats[vc].Both++
			}
		}
		for i, vb := range v.Biomes {
			biome(vb).Vanilla++
			biome(o.Biomes[i]).Ours++
			if vb == o.Biomes[i] {
				biome(vb).Both++
			}
		}
		for z := range 16 {
			for x := range 16 {
				vy, oy := surfaceY(v, x, z), surfaceY(o, x, z)
				if vy == oy && vy >= d.MinY {
					rep.SurfaceBlocks.add(blockName(v.Blocks[v.index(x, vy, z)]) == blockName(o.Blocks[o.index(x, oy, z)]))
				}
				rep.underground(d, v, o, x, z, vy, oy)
			}
		}
	}
	rep.Stages = cats
	for _, b := range biomes {
		rep.Biomes = append(rep.Biomes, *b)
	}
	sort.Slice(rep.Biomes, func(i, j int) bool {
		a, b := rep.Biomes[i], rep.Biomes[j]
		if a.Vanilla != b.Vanilla {
			return a.Vanilla > b.Vanilla
		}
		return a.Biome < b.Biome
	})
	return rep
}

// underground counts the air in the column below the column's own surface
// (so open sky never counts), down to the dimension's floor.
func (rep *stageReport) underground(d dimension, v, o *chunkData, x, z, vy, oy int) {
	for y := d.MinY; y < d.MinY+d.Height; y++ {
		vAir := y < vy && classCache[v.Blocks[v.index(x, y, z)]] == classAir
		oAir := y < oy && classCache[o.Blocks[o.index(x, y, z)]] == classAir
		if vAir {
			rep.UndergroundAir.Vanilla++
		}
		if oAir {
			rep.UndergroundAir.Ours++
		}
		if vAir && oAir {
			rep.UndergroundAir.Both++
		}
	}
}

// writeText prints the per-stage breakdown.
func (rep *stageReport) writeText(w io.Writer, top int) {
	fmt.Fprintf(w, "\nper stage (blocks; wrong = missing from ours + extra in ours):\n")
	fmt.Fprintf(w, "  %-20s %-40s %12s %12s %12s %12s\n", "category", "stage", "vanilla", "ours", "same", "wrong")
	total := 0
	for _, s := range rep.Stages {
		total += s.Wrong()
	}
	for _, s := range rep.Stages {
		if s.Vanilla == 0 && s.Ours == 0 {
			continue
		}
		fmt.Fprintf(w, "  %-20s %-40s %12d %12d %12d %12d\n", s.Category, s.Stage, s.Vanilla, s.Ours, s.Both, s.Wrong())
	}
	fmt.Fprintf(w, "  surface block, where the column height is equal: %s\n", rep.SurfaceBlocks)
	u := rep.UndergroundAir
	fmt.Fprintf(w, "  air below the column's surface: vanilla %d, ours %d, same %d\n", u.Vanilla, u.Ours, u.Both)

	fmt.Fprintf(w, "\nbiomes (cells; vanilla / ours / same):\n")
	for i, b := range rep.Biomes {
		if i == top {
			break
		}
		fmt.Fprintf(w, "  %-28s %9d %9d %9d\n", strip(b.Biome), b.Vanilla, b.Ours, b.Both)
	}

	fmt.Fprintf(w, "\nstructure starts (vanilla / ours):\n")
	names := make([]string, 0, len(rep.Structures))
	for n := range rep.Structures {
		names = append(names, n)
	}
	for n := range rep.OurStructures {
		if rep.Structures[n] == 0 {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(w, "  %-32s %5d %5d\n", strip(n), rep.Structures[n], rep.OurStructures[n])
	}
}

// writeText prints the block entity counts and the commonest differing keys.
func (rep blockEntityReport) writeText(w io.Writer, top int) {
	fmt.Fprintf(w, "\nblock entities (vanilla / ours / same id / identical): %d / %d / %d / %d\n", rep.Vanilla, rep.Ours, rep.SameID, rep.Identical)
	for i, p := range rep.Pairs {
		if i == top {
			break
		}
		fmt.Fprintf(w, "  %-32s %-32s %9d\n", strip(p.ID), p.Key, p.Count)
	}
}

// writeText prints the entity counts and the commonest differing keys.
func (rep entityReport) writeText(w io.Writer, top int) {
	if !rep.Recorded {
		fmt.Fprintln(w, "\nentities: not recorded (rerun with -record-entities)")
		return
	}
	fmt.Fprintf(w, "\nentities (vanilla / ours / paired / identical): %d / %d / %d / %d\n", rep.Vanilla, rep.Ours, rep.Paired, rep.Identical)
	if rep.FluidDrops > 0 {
		fmt.Fprintf(w, "fluid-flow item drops: %d\n", rep.FluidDrops)
	}
	for i, p := range rep.Pairs {
		if i == top {
			break
		}
		fmt.Fprintf(w, "  %-32s %-32s %9d\n", strip(p.ID), p.Key, p.Count)
	}
}
