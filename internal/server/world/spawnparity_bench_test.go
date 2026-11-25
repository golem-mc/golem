package world_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

func BenchmarkNaturalSpawnPass(b *testing.B) {
	for _, water := range []bool{false, true} {
		for _, y := range []float64{30, 200} {
			b.Run(fmt.Sprintf("water=%t/y=%g", water, y), func(b *testing.B) {
				w := world.SpawnParityFixture(b, water)
				players := []entity.SpawnPlayer{{EntityID: 1, X: 80, Y: y, Z: 0}}
				requests := 0
				b.ResetTimer()
				for b.Loop() {
					rng := rand.New(rand.NewSource(1))
					var chunks []entity.ChunkPos
					for x := int32(-2); x <= 3; x++ {
						for z := int32(-1); z <= 1; z++ {
							chunks = append(chunks, entity.ChunkPos{X: x, Z: z})
						}
					}
					var counts [entity.SpawnCategoryCount]entity.SpawnCounts
					passes := []entity.SpawnPass{entity.NewSpawnPass(entity.CategoryMonster, &counts[entity.CategoryMonster], -1), entity.NewSpawnPass(entity.CategoryCreature, &counts[entity.CategoryCreature], -1), entity.NewSpawnPass(entity.CategoryWaterCreature, &counts[entity.CategoryWaterCreature], -1), entity.NewSpawnPass(entity.CategoryUndergroundWater, &counts[entity.CategoryUndergroundWater], -1), entity.NewSpawnPass(entity.CategoryAmbient, &counts[entity.CategoryAmbient], -1), entity.NewSpawnPass(entity.CategoryWaterAmbient, &counts[entity.CategoryWaterAmbient], -1), entity.NewSpawnPass(entity.CategoryAxolotls, &counts[entity.CategoryAxolotls], -1)}
					var potential entity.SpawnPotential
					potential.Add(0, 30, 0, 0.7)
					entity.SpawnNaturally(w, rng, chunks, players, nil, &potential, passes, 289, nil, func(r entity.SpawnRequest) {
						m := entity.NewMob(r.Type, 1, r.X, r.Y, r.Z, 1)
						m.FinalizeSpawn(entity.GoDice{Rand: rng}, w.Biome(int32(r.X), int32(r.Y), int32(r.Z)), r.Group, rules.LocalDifficulty{})
						requests++
					})
				}
				b.ReportMetric(float64(requests)/float64(b.N), "requests/op")
			})
		}
	}
}
