package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkRecipeBookEntries measures encoding every crafting and cooking recipe.
func BenchmarkRecipeBookEntries(b *testing.B) {
	var names []string
	for _, d := range registry.CraftingDisplaysNow() {
		if len(names) == 0 || names[len(names)-1] != d.Name {
			names = append(names, d.Name)
		}
	}
	for _, d := range registry.FurnaceDisplaysNow() {
		names = append(names, d.Name)
	}
	b.ReportAllocs()
	for b.Loop() {
		var packet protocol.Buffer
		for _, name := range names {
			putNamedRecipeBookEntry(&packet, name, true, true)
		}
	}
}
