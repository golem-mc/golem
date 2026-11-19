package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

var infestedBench uint16

func BenchmarkInfestedHost(b *testing.B) {
	stone, _ := registry.FindState("minecraft:stone", nil)
	deepslate, _ := registry.FindState("minecraft:deepslate", map[string]string{"axis": "y"})
	dirt, _ := registry.FindState("minecraft:dirt", nil)
	states := [...]uint16{stone, deepslate, dirt}
	b.ReportAllocs()
	for b.Loop() {
		for _, state := range states {
			if IsInfestableHost(state) {
				next, _ := InfestedStateByHost(state)
				infestedBench = next
			}
		}
	}
}
