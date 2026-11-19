package main

import (
	"strings"
	"testing"
)

func TestMapDecorationsJoinReportIDsAndClassTints(t *testing.T) {
	w := newWorkspace(t)
	w.report("registries.json", `{"minecraft:map_decoration_type":{"entries":{"minecraft:monument":{"protocol_id":1},"minecraft:player":{"protocol_id":0}}}}`)
	w.java(map[string]string{"MapDecorations": "MAP_DECORATION minecraft:monument 3830373\nMAP_DECORATION minecraft:player -1"})
	w.gen("mapdecorations")
	w.wantContains("registry/mapdecorations_gen.go", `"minecraft:player":`, `{0, -1}`, `{1, 3830373}`)
	for _, dump := range []string{
		"MAP_DECORATION minecraft:player -1",
		"MAP_DECORATION minecraft:player -1\nMAP_DECORATION minecraft:player -1",
		"MAP_DECORATION minecraft:player -1\nMAP_DECORATION minecraft:unknown 42",
		"MAP_DECORATION minecraft:player -1\nMAP_DECORATION minecraft:monument -2",
	} {
		w.java(map[string]string{"MapDecorations": dump})
		if stderr := w.genFails("mapdecorations"); !strings.Contains(stderr, "map decoration") {
			t.Fatal(stderr)
		}
	}
	w.report("registries.json", `{"minecraft:map_decoration_type":{"entries":{"minecraft:player":{"protocol_id":1}}}}`)
	if stderr := w.genFails("mapdecorations"); !strings.Contains(stderr, "not contiguous") {
		t.Fatal(stderr)
	}
}
