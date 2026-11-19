package entity

import (
	"math/rand"
	"testing"
)

func TestAxolotlNaturalSpawnPlacementAndCategory(t *testing.T) {
	w := newSpawnTestWorld()
	w.biomeName = "minecraft:lush_caves"
	x, z := atChunk(0, 0)
	const y = 10
	water := defaultState(t, "minecraft:water")
	clay := defaultState(t, "minecraft:clay")
	stone := defaultState(t, "minecraft:stone")
	w.set(x, y-1, z, clay)
	w.set(x, y, z, water)
	rng := rand.New(rand.NewSource(1))
	if got := pickSpawnData(w, rng, x, y, z, CategoryAxolotls); got == nil || got.mob != TypeAxolotl || got.minCount != 4 || got.maxCount != 6 {
		t.Fatalf("lush cave spawn list: %+v", got)
	}
	if !AxolotlKind(TypeAxolotl) || CreatureKind(TypeAxolotl) || WaterCreatureKind(TypeAxolotl) || UndergroundWaterKind(TypeAxolotl) || WaterAmbientKind(TypeAxolotl) {
		t.Fatal("axolotl does not have its own category")
	}
	for _, tc := range []struct {
		name             string
		below, at, above uint16
		want             bool
	}{
		{"clay under water", clay, water, 0, true},
		{"clay under kelp", clay, defaultState(t, "minecraft:kelp"), 0, true},
		{"deep water without clay", water, water, 0, false},
		{"stone bottom", stone, water, 0, false},
		{"dry", clay, 0, 0, false},
		{"conductor above", clay, water, stone, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w.set(x, y-1, z, tc.below)
			w.set(x, y, z, tc.at)
			w.set(x, y+1, z, tc.above)
			if got := validSpawnPositionForType(w, rng, TypeAxolotl, x, y, z, 10000, CategoryAxolotls); got != tc.want {
				t.Fatalf("placement=%v, want %v", got, tc.want)
			}
		})
	}
	w.set(x, y, z, water)
	if !validPositionForMob(w, TypeAxolotl, x, y, z, 10000, nil) {
		t.Fatal("water obstructed an axolotl")
	}
	w.biomeName = "minecraft:plains"
	if pickSpawnData(w, rng, x, y, z, CategoryAxolotls) != nil {
		t.Fatal("axolotl spawned outside lush caves")
	}
	// A full cap stops the pass before it reads chunks or attempts placement.
	SpawnNaturally(w, rng, []ChunkPos{{}}, []SpawnPlayer{{EntityID: 1}}, nil, nil,
		[]SpawnPass{AxolotlPass(&SpawnCounts{Global: 5})}, spawnMagicNumber, func(SpawnRequest) { t.Fatal("spawned past the category cap") })
}
