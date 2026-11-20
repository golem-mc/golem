package world

import (
	"encoding/json"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func loadProcs(t *testing.T, list string) []structureProcessor {
	t.Helper()
	procs, _, err := loadStructureProcessors(json.RawMessage(`{"processors":[`+list+`]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	return procs
}

// run is runProcessors over a flat world of world state, with the height map
// at height.
func run(t *testing.T, procs []structureProcessor, state uint16, tag nbt.Compound, y, tmplY int32, world uint16, height int32) (uint16, nbt.Compound, int32, bool) {
	t.Helper()
	return runProcessors(procs, state, tag, 3, y, 5, tmplY, [3]int32{0, 60, 0},
		func(_, _, _ int32) uint16 { return world }, func(_ heightmapKind, _, _ int32) int32 { return height })
}

// vanilla: BlockIgnoreProcessor, NopProcessor, JigsawReplacementProcessor
func TestProcessorBlockIgnoreAndNop(t *testing.T) {
	t.Parallel()
	air, stone := blockState(t, "minecraft:air"), blockState(t, "minecraft:stone")
	procs := loadProcs(t, `{"processor_type":"minecraft:nop"},{"processor_type":"minecraft:jigsaw_replacement"},`+
		`{"processor_type":"minecraft:block_ignore","blocks":[{"Name":"minecraft:structure_void"},"minecraft:air"]}`)
	if _, _, _, ok := run(t, procs, air, nil, 70, 0, 0, 0); ok {
		t.Fatal("air was kept")
	}
	if got, _, _, ok := run(t, procs, stone, nil, 70, 0, 0, 0); !ok || got != stone {
		t.Fatal("stone changed")
	}
}

// vanilla: GravityProcessor
func TestProcessorGravityMovesBlock(t *testing.T) {
	t.Parallel()
	stone := blockState(t, "minecraft:stone")
	procs := loadProcs(t, `{"processor_type":"minecraft:gravity","heightmap":"OCEAN_FLOOR_WG","offset":-1}`)
	_, _, y, ok := run(t, procs, stone, nil, 200, 4, 0, 64)
	if !ok || y != 64-1+4 {
		t.Fatalf("y = %d, %v", y, ok)
	}
	if procs = loadProcs(t, `{"processor_type":"minecraft:gravity"}`); procs[0].(gravityProcessor).kind != hmWorldSurfaceWG {
		t.Fatal("default heightmap is not WORLD_SURFACE_WG")
	}
}

// vanilla: ProtectedBlockProcessor
func TestProcessorProtectedBlocksAnyTag(t *testing.T) {
	t.Parallel()
	stone, dirt, bedrock := blockState(t, "minecraft:stone"), blockState(t, "minecraft:dirt"), blockState(t, "minecraft:bedrock")
	for _, value := range []string{`"#minecraft:dirt"`, `"minecraft:dirt"`, `["minecraft:dirt","minecraft:sand"]`} {
		procs := loadProcs(t, `{"processor_type":"minecraft:protected_blocks","value":`+value+`}`)
		if _, _, _, ok := run(t, procs, stone, nil, 70, 0, dirt, 0); ok {
			t.Fatalf("%s: placed over dirt", value)
		}
		if _, _, _, ok := run(t, procs, stone, nil, 70, 0, bedrock, 0); !ok {
			t.Fatalf("%s: dropped over bedrock", value)
		}
	}
	_, prot, err := loadStructureProcessors(json.RawMessage(`{"processors":[{"processor_type":"minecraft:protected_blocks","value":"#minecraft:features_cannot_replace"}]}`), nil)
	if err != nil || !prot {
		t.Fatalf("features_cannot_replace fast path: %v %v", prot, err)
	}
}

// vanilla: LavaSubmergedBlockProcessor, BlackstoneReplaceProcessor
func TestProcessorLavaSubmergedAndBlackstone(t *testing.T) {
	t.Parallel()
	lava, stone, torch := blockState(t, "minecraft:lava"), blockState(t, "minecraft:stone"), blockState(t, "minecraft:torch")
	procs := loadProcs(t, `{"processor_type":"minecraft:lava_submerged_block"}`)
	if got, _, _, _ := run(t, procs, torch, nil, 70, 0, lava, 0); got != lava {
		t.Fatal("torch in lava stayed")
	}
	if got, _, _, _ := run(t, procs, stone, nil, 70, 0, lava, 0); got != stone {
		t.Fatal("full block in lava changed")
	}
	if got, _, _, _ := run(t, procs, torch, nil, 70, 0, 0, 0); got != torch {
		t.Fatal("torch in air changed")
	}
	procs = loadProcs(t, `{"processor_type":"minecraft:blackstone_replace"}`)
	if got, _, _, _ := run(t, procs, stone, nil, 70, 0, 0, 0); got != blockState(t, "minecraft:polished_blackstone") {
		t.Fatalf("stone became %d", got)
	}
}

// vanilla: BlockAgeProcessor, BlockRotProcessor
func TestProcessorBlockAgeAndRotAreSeededByPosition(t *testing.T) {
	t.Parallel()
	bricks := blockState(t, "minecraft:stone_bricks")
	age := loadProcs(t, `{"processor_type":"minecraft:block_age","mossiness":1.0}`)
	for _, procs := range [][]structureProcessor{age} {
		a, _, _, _ := run(t, procs, bricks, nil, 70, 0, 0, 0)
		b, _, _, _ := run(t, procs, bricks, nil, 70, 0, 0, 0)
		if a != b {
			t.Fatal("block_age is not deterministic")
		}
	}
	for _, rottable := range []string{`"#minecraft:dirt"`, `"minecraft:dirt"`, `["minecraft:dirt"]`} {
		rot := loadProcs(t, `{"processor_type":"minecraft:block_rot","integrity":0.0,"rottable_blocks":`+rottable+`}`)
		if _, _, _, ok := run(t, rot, bricks, nil, 70, 0, 0, 0); !ok {
			t.Fatalf("%s: rotted a block outside the set", rottable)
		}
		if _, _, _, ok := run(t, rot, blockState(t, "minecraft:dirt"), nil, 70, 0, 0, 0); ok {
			t.Fatalf("%s: integrity 0 kept a rottable block", rottable)
		}
	}
}

// vanilla: RandomBlockStateMatchTest, LinearPosTest, Clear, AppendStatic, Passthrough
func TestRuleProcessorMoreTestsAndModifiers(t *testing.T) {
	t.Parallel()
	const out = `"output_state":{"Name":"minecraft:air"}`
	stone, air := blockState(t, "minecraft:stone"), blockState(t, "minecraft:air")
	r := rulesFor(t, `{"input_predicate":{"predicate_type":"minecraft:random_blockstate_match","block_state":{"Name":"minecraft:stone"},"probability":1.0},`+
		`"location_predicate":{"predicate_type":"minecraft:random_blockstate_match","block_state":{"Name":"minecraft:dirt"},"probability":1.0},`+out+`}`)
	if got := r.process(stone, blockState(t, "minecraft:dirt"), 1, 2, 3, [3]int32{}); got != air {
		t.Fatal("random_blockstate_match did not replace")
	}
	if got := r.process(stone, stone, 1, 2, 3, [3]int32{}); got != stone {
		t.Fatal("location predicate ignored")
	}
	lin := rulesFor(t, `{"input_predicate":{"predicate_type":"minecraft:always_true"},"location_predicate":{"predicate_type":"minecraft:always_true"},`+out+
		`,"position_predicate":{"predicate_type":"minecraft:linear_pos","min_chance":0.0,"max_chance":1.0,"min_dist":0,"max_dist":10}}`)
	if got := lin.process(stone, 0, 20, 100, 20, [3]int32{}); got != air {
		t.Fatal("linear_pos beyond max_dist (manhattan) kept the block")
	}
	if got := lin.process(stone, 0, 0, 0, 0, [3]int32{}); got != stone {
		t.Fatal("linear_pos at the reference replaced the block")
	}
	mod := func(m string) nbt.Compound {
		rule := rulesFor(t, `{"input_predicate":{"predicate_type":"minecraft:always_true"},"location_predicate":{"predicate_type":"minecraft:always_true"},`+out+`,"block_entity_modifier":`+m+`}`)
		_, tag := rule.processTag(stone, 0, 0, 0, 0, [3]int32{}, nbt.Compound{"a": nbt.Int(1), "n": nbt.Compound{"x": nbt.Int(1)}})
		return tag
	}
	if tag := mod(`{"type":"minecraft:clear"}`); len(tag) != 0 || tag == nil {
		t.Fatalf("clear: %v", tag)
	}
	if tag := mod(`{"type":"minecraft:passthrough"}`); len(tag) != 2 {
		t.Fatalf("passthrough: %v", tag)
	}
	tag := mod(`{"type":"minecraft:append_static","data":"{b:2,n:{y:2}}"}`)
	if len(tag) != 3 || len(tag["n"].(nbt.Compound)) != 2 {
		t.Fatalf("append_static: %v", tag)
	}
}
