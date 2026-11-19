package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

const cookingSpeed = `{"type":"minecraft:conditional","condition":"minecraft:block/fast_cooking","on_false":1.0,"on_true":2.0}`
const cookingTime = `{"type":"minecraft:div","left":4001,"right":{"type":"minecraft:conditional","condition":"minecraft:block/fast_cooking","on_false":1,"on_true":2}}`
const compostLayers = `{"type":"minecraft:number_dispatcher","cases":[{"condition":{"type":"minecraft:match_block","blocks":"minecraft:composter","state":{"level":"0"}},"value":1}],"default":{"type":"minecraft:weighted_list","distribution":[{"data":1,"weight":30},{"data":0,"weight":70}]}}`
const fastCooking = `{"type":"minecraft:match_block","blocks":["minecraft:smoker","minecraft:blast_furnace"]}`

func TestComponentFuelsAndCompostRead26_3Providers(t *testing.T) {
	w := newWorkspace(t)
	items := map[string]any{}
	for i := range 100 {
		name := fmt.Sprintf("item%d", i)
		items["minecraft:"+name] = map[string]int{"protocol_id": i}
		w.report("minecraft/components/item/"+name+".json", `{"components":{"minecraft:cooking_fuel":{"burn_time":"minecraft:cooking/time","speed_multiplier":"minecraft:cooking/speed"},"minecraft:compostable":{"layers":"minecraft:compostable/low"}}}`)
	}
	w.report("registries.json", j(map[string]any{
		"minecraft:item": map[string]any{"entries": items},
		"minecraft:data_component_type": map[string]any{"entries": map[string]any{
			"minecraft:cooking_fuel": map[string]int{"protocol_id": 0}, "minecraft:compostable": map[string]int{"protocol_id": 1},
		}},
	}))
	// Only pot contents still come from Java in 26.3.
	var pots strings.Builder
	for i := range 20 {
		fmt.Fprintf(&pots, "POT minecraft:f%d minecraft:p%d\nUNPOT minecraft:p%d minecraft:f%d\n", i, i, i, i)
	}
	w.java(map[string]string{"BlockUseTables": pots.String()})
	w.serverJar(map[string]string{
		"data/minecraft/context_int_provider/cooking/time.json":    cookingTime,
		"data/minecraft/context_float_provider/cooking/speed.json": cookingSpeed,
		"data/minecraft/predicate/block/fast_cooking.json":         fastCooking,
		"data/minecraft/context_int_provider/compostable/low.json": compostLayers,
	})
	w.gen("fuel", "blockuse")
	w.wantContains("registry/fuel_gen.go", `ItemItem7: 4001,`)
	w.wantContains("registry/blockuse_gen.go", `ItemItem7: LootProviderRef{Name: "minecraft:compostable/low"},`, `"minecraft:f7": "minecraft:p7",`)
	w.report("minecraft/components/item/item7.json", `{"components":{"minecraft:cooking_fuel":{"burn_time":4001,"speed_multiplier":1}}}`)
	if stderr := w.genFails("fuel"); !strings.Contains(stderr, "unsupported burn times or speeds") {
		t.Errorf("unsupported constant furnace speed accepted: %s", stderr)
	}
	w.report("minecraft/components/item/item8.json", `{"components":{"minecraft:compostable":{"layers":2}}}`)
	w.gen("blockuse")
	w.wantContains("registry/blockuse_gen.go", `ItemItem8: LootConstant{Value: 2},`)
	w.report("minecraft/components/item/item8.json", `{"components":{"minecraft:compostable":{"layers":{"type":"minecraft:uniform","min":1,"max":2}}}}`)
	if stderr := w.genFails("blockuse"); !strings.Contains(stderr, "unsupported compost layers") {
		t.Errorf("accepted an inline provider: %s", stderr)
	}
}

func TestCookingProvidersRejectUnrepresentableValues(t *testing.T) {
	dp := &vanilla.DataPack{FS: fstest.MapFS{
		"minecraft/context_int_provider/loop.json":    {Data: []byte(`"minecraft:loop"`)},
		"minecraft/predicate/block/fast_cooking.json": {Data: []byte(fastCooking)},
	}}
	r := lootCodec{dp: dp}
	for _, raw := range []string{
		`"minecraft:loop"`, `"#minecraft:tag"`, `"minecraft:missing"`, `1.5`, `2147483648`,
		`{"type":"minecraft:div","left":10,"right":0}`,
		`{"type":"minecraft:div","left":10,"right":2,"extra":true}`,
		`{"type":"minecraft:uniform","min_inclusive":1,"max_inclusive":4}`,
		strings.Replace(cookingTime, `"minecraft:block/fast_cooking"`, `{"type":"minecraft:match_block","blocks":["minecraft:furnace"]}`, 1),
		strings.Replace(cookingTime, `"on_true":2`, `"on_true":{"type":"minecraft:unsupported"}`, 1),
	} {
		if err := tryRead(func() { cookingNumber(r, "int", decodeValue([]byte(raw), "fixture"), false, nil) }); err == nil {
			t.Errorf("accepted cooking provider %s", raw)
		}
	}
}

func TestBlockDefinitionsRead26_3ClassesAndValidateCoverage(t *testing.T) {
	var blocks vanilla.Blocks
	if err := json.Unmarshal([]byte(`{"minecraft:powder":{"states":[]},"minecraft:concrete":{"states":[]},"minecraft:door":{"states":[]}}`), &blocks); err != nil {
		t.Fatal(err)
	}
	const dump = "[STDOUT]: DEFINITION minecraft:powder ConcretePowderBlock minecraft:concrete\nDEFINITION minecraft:concrete Block -\nDEFINITION minecraft:door TrapDoorBlock -\n"
	definitions, err := parseBlockDefinitions(blocks, dump)
	if err != nil {
		t.Fatal(err)
	}
	if d := definitions["minecraft:powder"]; d.Type != "minecraft:concrete_powder" || d.Concrete == nil || *d.Concrete != "minecraft:concrete" {
		t.Errorf("concrete definition = %+v", d)
	}
	if definitions["minecraft:door"].Type != "minecraft:trapdoor" {
		t.Error("legacy trapdoor codec label lost")
	}
	for _, invalid := range []string{
		strings.Replace(dump, "DEFINITION minecraft:door TrapDoorBlock -\n", "", 1),
		dump + "DEFINITION minecraft:door TrapDoorBlock -\n",
		strings.Replace(dump, "minecraft:door", "minecraft:missing", 1),
		strings.Replace(dump, "ConcretePowderBlock minecraft:concrete", "Block minecraft:concrete", 1),
		strings.Replace(dump, "ConcretePowderBlock minecraft:concrete", "ConcretePowderBlock minecraft:missing", 1),
	} {
		if _, err := parseBlockDefinitions(blocks, invalid); err == nil {
			t.Errorf("accepted mismatched class dump: %s", invalid)
		}
	}
	for class, want := range map[string]string{"StrawBedBlock": "bed", "PathBlock": "dirt_path", "LeavesBlock": "untinted_particle_leaves", "RedStoneWireBlock": "redstone_wire", "WeatheringCopperTrapDoorBlock": "weathering_copper_trapdoor", "PiglinWallSkullBlock": "piglinwallskull"} {
		if got := blockClassType(class); got != "minecraft:"+want {
			t.Errorf("%s = %s, want %s", class, got, want)
		}
	}
}
