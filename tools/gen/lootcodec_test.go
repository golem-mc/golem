package main

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/golem-mc/golem/tools/internal/vanilla"
)

func fixtureLootCodec(files map[string]string) lootCodec {
	fsys := fstest.MapFS{}
	for name, raw := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(raw)}
	}
	return lootCodec{&vanilla.DataPack{FS: fsys}}
}

func TestLootCodecReadsTables(t *testing.T) {
	codec := fixtureLootCodec(map[string]string{
		"minecraft/predicate/tool/can_shear.json": `{"type":"minecraft:match_tool","predicate":{"items":"minecraft:shears"}}`,
		"example/item_modifier/count.json":        `[{"type":"minecraft:set_count","count":2},{"type":"minecraft:set_count","count":3,"add":true}]`,
	})
	current := `{"pools":[{"condition":{"type":"minecraft:survives_explosion"},"entries":[{"type":"minecraft:item","name":"minecraft:stone","condition":"minecraft:tool/can_shear","modifier":"example:count"}]}]}`
	r := &lootReader{dp: codec.dp, usedItems: map[string]bool{}}
	after := r.tableData(decode([]byte(current), "current"), "fixture")
	for _, want := range []string{"LootSurvivesExplosion{}", `Items: []string{"minecraft:shears"}`, "Value: 2.0", "Value: 3.0"} {
		if !strings.Contains(after, want) {
			t.Errorf("table lacks %s", want)
		}
	}
	if strings.Index(after, "Value: 2.0") > strings.Index(after, "Value: 3.0") {
		t.Error("modifier sequence reversed")
	}
}

func TestLootCodecRejectsUnrepresentableOrCyclicData(t *testing.T) {
	codec := fixtureLootCodec(map[string]string{
		"minecraft/predicate/a.json":          `{"type":"minecraft:inverted","term":"minecraft:b"}`,
		"minecraft/predicate/b.json":          `{"type":"minecraft:all_of","terms":["minecraft:a"]}`,
		"minecraft/tags/predicate/cycle.json": `{"values":["#minecraft:cycle"]}`,
		"minecraft/item_modifier/a.json":      `["minecraft:b"]`,
		"minecraft/item_modifier/b.json":      `{"type":"minecraft:sequence","functions":["minecraft:a"]}`,
	})
	for _, tt := range []struct {
		name, raw, want string
		read            func(any)
	}{
		{"predicate cycle", `"minecraft:a"`, "cyclic", func(v any) { codec.predicate(v, nil) }},
		{"tag cycle", `{"type":"minecraft:all_of","terms":"#minecraft:cycle"}`, "cyclic", func(v any) { codec.predicate(v, nil) }},
		{"modifier cycle", `"minecraft:a"`, "cyclic", func(v any) { codec.modifier(v, nil) }},
		{"missing predicate", `"minecraft:missing"`, "missing.json", func(v any) { codec.predicate(v, nil) }},
		{"invalid path", `"minecraft:../escape"`, "invalid", func(v any) { codec.predicate(v, nil) }},
		{"tag as holder", `"#minecraft:tag"`, "cannot be a tag", func(v any) { codec.predicate(v, nil) }},
		{"conditional sequence", `{"type":"minecraft:sequence","condition":{"type":"minecraft:killed_by_player"},"functions":[]}`, "conditional", func(v any) { codec.modifier(v, nil) }},
		{"filtered sequence", `{"type":"minecraft:filtered","on_fail":{"type":"minecraft:sequence","functions":[{"type":"minecraft:discard"},{"type":"minecraft:discard"}]}}`, "single item modifier", func(v any) { codec.modifier(v, nil) }},
		{"mixed condition fields", `{"condition":"minecraft:killed_by_player","type":"minecraft:survives_explosion"}`, "both", func(v any) { codec.predicate(v, nil) }},
		{"mixed modifier fields", `{"function":"minecraft:set_count","type":"minecraft:set_count","count":1}`, "both", func(v any) { codec.modifier(v, nil) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := tryRead(func() { tt.read(decodeValue([]byte(tt.raw), tt.name)) })
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v, want %s", err, tt.want)
			}
		})
	}
}
