package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

type survivalBenchReader uint16

func (r survivalBenchReader) BlockAt(int32, int32, int32) uint16 { return uint16(r) }

var placementSurvivesResult bool

func BenchmarkPlacementSurvives(b *testing.B) {
	for _, tc := range []struct {
		name, block, below string
	}{
		{"soul_fire_stone", "soul_fire", "stone"},
		{"soul_fire_soil", "soul_fire", "soul_soil"},
		{"poppy_dirt", "poppy", "dirt"},
		{"stone_air", "stone", "air"},
	} {
		state := registry.MustState("minecraft:"+tc.block, nil)
		reader := BlockReader(survivalBenchReader(registry.MustState("minecraft:"+tc.below, nil)))
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				placementSurvivesResult = placementSurvives(reader, state, 0, 64, 0)
			}
		})
	}
}
