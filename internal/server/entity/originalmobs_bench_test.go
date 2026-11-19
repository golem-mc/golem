package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

// BenchmarkOriginalMobs grows the first-generation herds of an 8x8-chunk
// plains pasture at seed 0: the dice set-up, gate, picks, walk and finalize
// one freshly generated chunk costs.
func BenchmarkOriginalMobs(b *testing.B) {
	w := newSpawnTestWorld()
	w.sky = 15
	grass := defaultState(b, "minecraft:grass_block")
	for x := range int32(128) {
		for z := range int32(128) {
			w.set(x, 29, z, grass)
		}
	}
	w.surface = 30
	var id int32
	nextID := func() int32 { id++; return id }
	difficulty := func(float64, float64) rules.LocalDifficulty { return rules.LocalDifficulty{} }
	b.ReportAllocs()
	for b.Loop() {
		for cx := range int32(8) {
			for cz := range int32(8) {
				OriginalMobs(w, 0, ChunkPos{X: cx, Z: cz}, nextID, difficulty)
			}
		}
	}
}
