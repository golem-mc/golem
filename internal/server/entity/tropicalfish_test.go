package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
)

func TestTropicalFishSpawnGroups(t *testing.T) {
	common, solitary := 0, 0
	for seed := range int64(200) {
		group := NewNaturalSpawnGroup(GoDice{rand.New(rand.NewSource(seed))})
		leader := NewMob(TypeTropicalFish, 1, 0, 60, 0, seed)
		leader.FinalizeSpawn(GoDice{leader.Rand}, testBiome("minecraft:warm_ocean"), group, rules.LocalDifficulty{})
		if group.tropicalSolitary {
			solitary++
			continue
		}
		common++
		for i := range 7 {
			follower := NewMob(TypeTropicalFish, int32(i+2), 0, 60, 0, seed+int64(i)+1)
			follower.FinalizeSpawn(GoDice{follower.Rand}, testBiome("minecraft:warm_ocean"), group, rules.LocalDifficulty{})
			if follower.TropicalVariant != leader.TropicalVariant || follower.fish.leader != leader {
				t.Fatalf("school member differs from leader: %d, %d", follower.TropicalVariant, leader.TropicalVariant)
			}
		}
		if leader.SchoolSize() != 8 {
			t.Fatalf("school size = %d", leader.SchoolSize())
		}
	}
	if common < 160 || solitary < 5 {
		t.Fatalf("common=%d solitary=%d", common, solitary)
	}
}

// TestASolitaryTropicalFishEndsOnlyItsPack: a fish with a random pattern
// stops its own pack, and the position's later packs still spawn.
// vanilla: NaturalSpawner.spawnCategoryForPosition, TropicalFish.isMaxGroupSizeReached
func TestASolitaryTropicalFishEndsOnlyItsPack(t *testing.T) {
	w := oceanAround(t)
	w.biomeName = "minecraft:warm_ocean"
	players := []SpawnPlayer{{EntityID: 1, X: 40.5, Y: 62, Z: 0.5}}
	solitaryFirst := 0
	for seed := int64(1); seed <= 2000; seed++ {
		var reqs []SpawnRequest
		var bodies []SpawnBody
		id := int32(1)
		spawnCategoryForPosition(w, readSpawnPoint(w), rand.New(rand.NewSource(seed)), ChunkPos{}, 62, 0, 0,
			players, &bodies, &SpawnPotential{}, &SpawnCounts{PerPlayer: make(map[int32]int)}, CategoryWaterAmbient,
			func(r SpawnRequest) {
				m := NewMob(r.Type, id, r.X, r.Y, r.Z, seed*16+int64(id))
				id++
				m.FinalizeSpawn(GoDice{m.Rand}, testBiome(w.biomeName), r.Group, rules.LocalDifficulty{})
				reqs = append(reqs, r)
			})
		if len(reqs) == 0 || reqs[0].Type != TypeTropicalFish || !reqs[0].Group.tropicalSolitary {
			continue
		}
		solitaryFirst++
		for _, r := range reqs[1:] {
			if r.Group == reqs[0].Group {
				t.Fatalf("seed %d: the solitary fish's pack spawned another mob", seed)
			}
		}
		if len(reqs) > 1 {
			return
		}
	}
	t.Fatalf("none of %d positions opening with a solitary tropical fish spawned a later pack", solitaryFirst)
}

func TestTropicalFishVariantSaveAndLoad(t *testing.T) {
	m := NewMob(TypeTropicalFish, 1, 0, 60, 0, 1)
	m.TropicalVariant, m.FromBucket = 117506305, true
	tag := nbt.Compound{}
	m.SaveTo(tag, func() int64 { return 0 })
	out := NewMob(TypeTropicalFish, 2, 0, 60, 0, 2)
	out.LoadFrom(tag, nil, func() int64 { return 0 })
	if out.TropicalVariant != m.TropicalVariant || !out.FromBucket {
		t.Fatalf("saved fish variant=%d, fromBucket=%v", out.TropicalVariant, out.FromBucket)
	}
	for _, tc := range []struct {
		value nbt.Tag
		want  int32
	}{
		{nbt.Double(117506305.75), 117506305}, // Codec.INT truncates.
		{nbt.Int(-1), 0},
		{nbt.Int(117506305), 117506305},
		{nbt.Int(0x100f0501), 0x000f0501},
	} {
		out.LoadFrom(nbt.Compound{"Variant": tc.value}, nil, func() int64 { return 0 })
		if out.TropicalVariant != tc.want {
			t.Errorf("Variant %v = %d, want %d", tc.value, out.TropicalVariant, tc.want)
		}
	}
}

func TestTropicalFishNaturalSpawnRules(t *testing.T) {
	w := newSpawnTestWorld()
	x, z := atChunk(0, 0)
	water := defaultState(t, "minecraft:water")
	rng := rand.New(rand.NewSource(1))
	for _, tc := range []struct {
		biome        string
		y            int32
		below, above uint16
		want         bool
	}{
		{"minecraft:warm_ocean", 50, water, water, true},
		{"minecraft:warm_ocean", 49, water, water, false},
		{"minecraft:warm_ocean", 63, water, water, true},
		{"minecraft:warm_ocean", 64, water, water, false},
		{"minecraft:lush_caves", 10, water, water, true},
		{"minecraft:lush_caves", 100, water, water, true},
		{"minecraft:lush_caves", 10, 0, water, false},
		{"minecraft:lush_caves", 10, water, defaultState(t, "minecraft:kelp"), false},
	} {
		w.biomeName = tc.biome
		w.set(x, tc.y, z, water)
		w.set(x, tc.y-1, z, tc.below)
		w.set(x, tc.y+1, z, tc.above)
		if got := validSpawnPositionForType(w, rng, TypeTropicalFish, x, tc.y, z, 1600, CategoryWaterAmbient); got != tc.want {
			t.Errorf("%s y=%d below=%d above=%d: %v, want %v", tc.biome, tc.y, tc.below, tc.above, got, tc.want)
		}
	}
	if !WaterAmbientKind(TypeTropicalFish) || !KnownMobKind(TypeTropicalFish) || maxSpawnClusterSize(TypeTropicalFish) != 8 {
		t.Fatal("tropical fish category or cluster size")
	}
}
