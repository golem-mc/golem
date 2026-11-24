package datapack

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestDecodeSlotSource(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackSlotSources(nil) })
	registry.ApplyPackSlotSources(map[string]registry.SlotSource{"p:a": registry.SlotEmptySource{}})
	for _, tc := range []struct {
		name, json string
		ok         bool
	}{
		{"range", `{"type":"minecraft:slot_range","slots":"hotbar.*"}`, true},
		{"range source", `{"type":"minecraft:slot_range","source":"attacker","slots":"hotbar.*"}`, true},
		{"unknown target", `{"type":"minecraft:slot_range","source":"nobody","slots":"hotbar.*"}`, false},
		{"filtered", `{"type":"minecraft:filtered","slot_source":"p:a","item_filter":{"items":"minecraft:diamond"}}`, true},
		{"limit", `{"type":"minecraft:limit_slots","slot_source":{"type":"minecraft:empty"},"limit":2}`, true},
		{"zero limit", `{"type":"minecraft:limit_slots","slot_source":{"type":"minecraft:empty"},"limit":0}`, false},
		{"contents", `{"type":"minecraft:contents","slot_source":"p:a","component":"minecraft:bundle_contents"}`, true},
		{"contents of another component", `{"type":"minecraft:contents","slot_source":"p:a","component":"minecraft:damage"}`, false},
		{"group", `{"type":"minecraft:group","terms":["p:a",{"type":"minecraft:empty"}]}`, true},
		{"bare list", `["p:a"]`, true},
		{"unknown id", `"p:missing"`, false},
		{"unknown type", `{"type":"minecraft:nope"}`, false},
	} {
		if _, err := DecodeSlotSource(nil, []byte(tc.json)); (err == nil) != tc.ok {
			t.Errorf("%s: err = %v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

// TestDecodeResolvesStagedLayers checks that a pack load's loot resolves
// provider and slot source names against its staged layers, which the
// layers in force do not see before Publish.
func TestDecodeResolvesStagedLayers(t *testing.T) {
	layers := registry.StagePackLayers()
	layers.SetContextProviders(map[string][]byte{"p:n": []byte(`3`)}, nil)
	layers.SetSlotSources(map[string]registry.SlotSource{"p:s": registry.SlotEmptySource{}})
	table := []byte(`{"pools":[{"rolls":"p:n","entries":[{"type":"minecraft:slots","slot_source":"p:s"}]}]}`)
	if _, err := DecodeLootTable(layers, table); err != nil {
		t.Errorf("staged layers: %v", err)
	}
	if _, err := DecodeLootTable(nil, table); err == nil {
		t.Error("the layers in force resolved the staged names")
	}
}
