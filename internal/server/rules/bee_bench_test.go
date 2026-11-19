package rules

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

var beeGrownCropBench uint16

func BenchmarkBeeGrownCrop(b *testing.B) {
	wheat, _ := registry.FindState("minecraft:wheat", map[string]string{"age": "3"})
	vines, _ := registry.FindState("minecraft:cave_vines", map[string]string{"age": "5", "berries": "false"})
	states := [...]uint16{wheat, vines}
	b.ReportAllocs()
	for b.Loop() {
		for _, state := range states {
			next, ok := BeeGrownCrop(state)
			if !ok {
				b.Fatal("crop did not grow")
			}
			beeGrownCropBench = next
		}
	}
}
