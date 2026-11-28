package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

func TestSlotSourceReadsEntity(t *testing.T) {
	this := registry.SlotRangeSource{Source: "this", Slots: "weapon.*"}
	container := registry.SlotRangeSource{Source: "container", Slots: "container.*"}
	blockEntity := registry.SlotRangeSource{Source: "block_entity", Slots: "container.*"}

	registry.ApplyPackSlotSources(map[string]registry.SlotSource{
		"t:reads":  registry.SlotLimitSource{From: this, Limit: 1},
		"t:plain":  registry.SlotLimitSource{From: container, Limit: 1},
		"t:chain":  registry.SlotSourceReference{Name: "t:reads"},
		"t:cycle":  registry.SlotSourceReference{Name: "t:cycle"},
		"t:inside": registry.SlotGroupSource{Terms: []registry.SlotSource{container, registry.SlotSourceReference{Name: "t:reads"}}},
	})
	t.Cleanup(func() { registry.ApplyPackSlotSources(nil) })

	wrap := map[string]func(registry.SlotSource) registry.SlotSource{
		"filtered": func(s registry.SlotSource) registry.SlotSource {
			return registry.SlotFilteredSource{From: s}
		},
		"limit": func(s registry.SlotSource) registry.SlotSource {
			return registry.SlotLimitSource{From: s, Limit: 2}
		},
		"contents": func(s registry.SlotSource) registry.SlotSource {
			return registry.SlotContentsSource{From: s, Component: "container"}
		},
		"group": func(s registry.SlotSource) registry.SlotSource {
			return registry.SlotGroupSource{Terms: []registry.SlotSource{container, s}}
		},
		"nested": func(s registry.SlotSource) registry.SlotSource {
			return registry.SlotLimitSource{From: registry.SlotFilteredSource{From: s}, Limit: 2}
		},
	}
	for name, w := range wrap {
		if !SlotSourceReadsEntity(w(this)) {
			t.Errorf("%s around this: want true", name)
		}
		if SlotSourceReadsEntity(w(container)) {
			t.Errorf("%s around container: want false", name)
		}
		if SlotSourceReadsEntity(w(blockEntity)) {
			t.Errorf("%s around block_entity: want false", name)
		}
	}

	for _, tc := range []struct {
		name   string
		source registry.SlotSource
		want   bool
	}{
		{"this", this, true},
		{"container", container, false},
		{"empty", registry.SlotEmptySource{}, false},
		{"empty group", registry.SlotGroupSource{}, false},
		{"reference reading this", registry.SlotSourceReference{Name: "t:reads"}, true},
		{"reference to container", registry.SlotSourceReference{Name: "t:plain"}, false},
		{"reference chain", registry.SlotSourceReference{Name: "t:chain"}, true},
		{"reference in group", registry.SlotSourceReference{Name: "t:inside"}, true},
		{"unknown reference", registry.SlotSourceReference{Name: "t:missing"}, false},
		{"reference cycle", registry.SlotSourceReference{Name: "t:cycle"}, false},
		{"filtered reference", registry.SlotFilteredSource{From: registry.SlotSourceReference{Name: "t:reads"}}, true},
	} {
		if got := SlotSourceReadsEntity(tc.source); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}
