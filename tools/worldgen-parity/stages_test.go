package main

import "testing"

func TestCategoryOf(t *testing.T) {
	for name, want := range map[string]string{
		"minecraft:air":                  "air",
		"minecraft:cave_air":             "air",
		"minecraft:stone":                "stone",
		"minecraft:deepslate_iron_ore":   "ore",
		"minecraft:raw_copper_block":     "ore",
		"minecraft:tuff":                 "ore blob",
		"minecraft:grass_block":          "surface layer",
		"minecraft:red_terracotta":       "surface layer",
		"minecraft:birch_leaves":         "log and leaves",
		"minecraft:short_grass":          "plant",
		"minecraft:pointed_dripstone":    "underground feature",
		"minecraft:oak_planks":           "structure block",
		"minecraft:cobblestone_wall":     "structure block",
		"minecraft:prismarine":           "structure block",
		"minecraft:prismarine_bricks":    "structure block",
		"minecraft:dark_prismarine":      "structure block",
		"minecraft:sea_lantern":          "structure block",
		"minecraft:wet_sponge":           "structure block",
		"minecraft:gold_block":           "structure block",
		"minecraft:bedrock":              "bedrock",
		"minecraft:snow":                 "ice and snow",
		"minecraft:potted_poppy":         "plant",
		"minecraft:mangrove_roots":       "log and leaves",
		"minecraft:infested_deepslate":   "ore blob",
		"minecraft:water":                "water",
		"minecraft:lava":                 "lava",
		"minecraft:deepslate":            "deepslate",
		"minecraft:white_terracotta":     "surface layer",
		"minecraft:oak_wood":             "log and leaves",
		"minecraft:warped_stem":          "other",
		"minecraft:diamond_ore":          "ore",
		"minecraft:big_dripleaf":         "underground feature",
		"minecraft:brown_mushroom_block": "plant",
	} {
		if got := categoryOf(name); got != want {
			t.Errorf("categoryOf(%s) = %q, want %q", name, got, want)
		}
	}
}

func TestStageReportCountsSamePositionAndStructures(t *testing.T) {
	v, o := filled(t, 0, 0), filled(t, 0, 0)
	deepslate := state(t, "minecraft:deepslate", nil)
	oak := state(t, "minecraft:oak_log", map[string]string{"axis": "y"})
	for y := -64; y < -60; y++ {
		v.Blocks[v.index(1, y, 1)] = deepslate
		o.Blocks[o.index(1, y, 1)] = deepslate
	}
	v.Blocks[v.index(2, 1, 2)] = oak
	o.Blocks[o.index(3, 1, 3)] = oak
	v.Structures = []string{"minecraft:mineshaft"}

	rep := stageReportOf(overworld, map[[2]int32]*chunkData{{0, 0}: v}, map[[2]int32]*chunkData{{0, 0}: o})
	var logs, slate stageStat
	for _, s := range rep.Stages {
		switch s.Category {
		case "log and leaves":
			logs = s
		case "deepslate":
			slate = s
		}
	}
	if logs.Vanilla != 1 || logs.Ours != 1 || logs.Both != 0 || logs.Wrong() != 2 {
		t.Errorf("logs = %+v, want one on each side at different positions", logs)
	}
	if slate.Both != 4 || slate.Wrong() != 0 {
		t.Errorf("deepslate = %+v, want 4 matching", slate)
	}
	if rep.Structures["minecraft:mineshaft"] != 1 || len(rep.OurStructures) != 0 {
		t.Errorf("structures = %v / %v", rep.Structures, rep.OurStructures)
	}
}
