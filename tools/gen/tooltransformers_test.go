package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestToolConversionsRead26_3Transformers(t *testing.T) {
	w := newWorkspace(t)
	blocks := map[string]any{}
	var axe []any
	var ground []string
	for _, kind := range []string{"strip", "scrape", "wax_off", "flatten"} {
		var rules []any
		for i := range 6 {
			from, to := fmt.Sprintf("minecraft:%s%d", kind, i), fmt.Sprintf("minecraft:%s_target%d", kind, i)
			for _, name := range []string{from, to} {
				blocks[name] = map[string]any{
					"properties": map[string]any{"axis": []string{"x", "y", "z"}},
					"states":     []any{map[string]any{"id": len(blocks), "default": true, "properties": map[string]string{"axis": "y"}}},
				}
			}
			rules = append(rules, map[string]any{
				"if_true": map[string]any{"type": "minecraft:matching_blocks", "blocks": from},
				"then":    map[string]any{"type": "minecraft:copy_properties", "source": map[string]any{"id": to, "properties": map[string]string{"axis": "y"}}},
			})
			if kind == "flatten" {
				ground = append(ground, from)
			}
		}
		if kind != "flatten" {
			entry := map[string]any{
				"sound": "minecraft:item.axe." + kind, "item_damage_per_use": 1,
				"block_state_provider": map[string]any{"type": "minecraft:rule_based", "rules": rules},
			}
			if kind != "strip" {
				entry["particle"] = kind
			}
			axe = append(axe, entry)
		}
	}
	w.report("blocks.json", j(blocks))
	axeFile, shovelFile := "data/minecraft/block_transformer/axe.json", "data/minecraft/block_transformer/shovel.json"
	const shovel = `[{"block_state_provider":{"type":"minecraft:rule_based","rules":[{"if_true":{"type":"minecraft:all_of","predicates":[{"type":"minecraft:matching_block_tag","tag":"minecraft:turns_into_dirt_path"},{"type":"minecraft:matching_block_tag","offset":[0,1,0],"tag":"minecraft:air"}]},"then":{"id":"minecraft:flatten_target0"}}]},"disallowed_faces":["down"],"item_damage_per_use":1,"sound":"minecraft:item.shovel.flatten"}]`
	files := map[string]string{
		axeFile: j(axe), shovelFile: shovel,
		"data/minecraft/tags/block/turns_into_dirt_path.json": j(map[string]any{"values": ground}),
		"data/minecraft/tags/block/copper_chests.json":        `{"values":[]}`,
		"data/minecraft/tags/block/doors.json":                `{"values":[]}`,
	}
	w.serverJar(files)
	w.gen("toolconversions")
	w.wantContains("registry/toolconversions_gen.go", `"minecraft:strip2": "minecraft:strip_target2",`, `"minecraft:scrape3": "minecraft:scrape_target3",`, `"minecraft:wax_off1": "minecraft:wax_off_target1",`, `"minecraft:flatten5": "minecraft:flatten_target0",`)
	for _, raw := range []string{
		strings.Replace(j(axe), `"item_damage_per_use":1`, `"item_damage_per_use":2`, 1),
		strings.Replace(j(axe), `"sound":`, `"extra":true,"sound":`, 1),
		strings.Replace(j(axe), `"minecraft:strip0"`, `"minecraft:unknown"`, 1),
		strings.Replace(j(axe), `"axis":"y"`, `"axis":"x"`, 1),
		strings.Replace(j(axe), `"minecraft:copy_properties"`, `"minecraft:weighted_state_provider"`, 1),
		strings.Replace(j(axe), `"item_damage_per_use":1`, `"transform_type":"unsupported","item_damage_per_use":1`, 1),
		strings.Replace(j(axe), `"item_damage_per_use":1`, `"update_from_neighbors":false,"item_damage_per_use":1`, 1),
		j([]any{axe[1], axe[0], axe[2]}),
		j([]any{axe[0], axe[0], axe[1], axe[2]}),
	} {
		files[axeFile] = raw
		w.serverJar(files)
		w.genFails("toolconversions")
	}
	files[axeFile] = j(axe)
	for _, raw := range []string{
		strings.Replace(shovel, `"offset":[0,1,0]`, `"offset":[0,2,0]`, 1),
		strings.Replace(shovel, `"disallowed_faces":["down"]`, `"disallowed_faces":[]`, 1),
		strings.Replace(shovel, `"id":"minecraft:flatten_target0"`, `"id":"minecraft:flatten_target0","properties":{"axis":"x"}`, 1),
	} {
		files[shovelFile] = raw
		w.serverJar(files)
		w.genFails("toolconversions")
	}
	// Both the property names and their allowed values must survive copying.
	files[shovelFile] = shovel
	blocks["minecraft:scrape_target0"].(map[string]any)["properties"] = map[string]any{"axis": []string{"x", "y"}}
	w.report("blocks.json", j(blocks))
	w.serverJar(files)
	if stderr := w.genFails("toolconversions"); !strings.Contains(stderr, "transformation property axis differs") {
		t.Errorf("incompatible property values accepted: %s", stderr)
	}
}
