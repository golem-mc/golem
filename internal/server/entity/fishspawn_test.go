package entity

import (
	"math/rand"
	"testing"
)

// TestRiversHoldBackWaterAmbientRolls: in a river biome a water-ambient roll
// is thrown away unless its first float reaches 0.98, and the pick is then the
// list's salmon; elsewhere the list is rolled outright.
// vanilla: NaturalSpawner.getRandomSpawnMobAt, BiomeTags.REDUCED_WATER_AMBIENT_SPAWNS
func TestRiversHoldBackWaterAmbientRolls(t *testing.T) {
	for _, biome := range []string{"minecraft:river", "minecraft:frozen_river"} {
		w := newSpawnTestWorld()
		w.biomeName = biome
		passed, held := 0, 0
		for seed := range int64(3000) {
			through := rand.New(rand.NewSource(seed)).Float32() >= 0.98
			got := pickSpawnData(w, rand.New(rand.NewSource(seed)), 0, 62, 0, CategoryWaterAmbient)
			switch {
			case !through && got != nil:
				t.Fatalf("%s seed %d: picked %v past the 98%% hold", biome, seed, got.mob)
			case through && (got == nil || got.mob != TypeSalmon):
				t.Fatalf("%s seed %d: picked %v, want the salmon", biome, seed, got)
			case through:
				passed++
			default:
				held++
			}
		}
		if passed == 0 || held == 0 {
			t.Errorf("%s: %d rolls passed and %d were held, want both", biome, passed, held)
		}
	}
	w := newSpawnTestWorld()
	w.biomeName = "minecraft:ocean"
	for seed := range int64(50) {
		if got := pickSpawnData(w, rand.New(rand.NewSource(seed)), 0, 62, 0, CategoryWaterAmbient); got == nil || got.mob != TypeCod {
			t.Fatalf("ocean seed %d: picked %v, want the ocean's cod every time", seed, got)
		}
	}
}

// TestWaterAmbientCategoryNumbers pins the category's cap, despawn distance
// and the fish's cluster sizes.
// vanilla: MobCategory.WATER_AMBIENT, AbstractFish.getMaxSpawnClusterSize
func TestWaterAmbientCategoryNumbers(t *testing.T) {
	if waterAmbientCategoryMax != 20 {
		t.Errorf("the cap is %d, want 20", waterAmbientCategoryMax)
	}
	if got := despawnDistanceOf(TypeCod); got != 64 {
		t.Errorf("a cod's spawn distance is %v, want 64", got)
	}
	if got := despawnDistanceOf(TypeSquid); got != 128 {
		t.Errorf("a squid's spawn distance is %v, want 128", got)
	}
	if maxSpawnClusterSize(TypeCod) != 8 || maxSpawnClusterSize(TypeSalmon) != 5 || maxSpawnClusterSize(TypeZombie) != 4 {
		t.Errorf("cluster sizes cod %d salmon %d zombie %d, want 8, 5 and 4",
			maxSpawnClusterSize(TypeCod), maxSpawnClusterSize(TypeSalmon), maxSpawnClusterSize(TypeZombie))
	}
	if maxSpawnClusterSize(TypeWolf) != 8 || maxSpawnClusterSize(TypeHorse) != 6 ||
		maxSpawnClusterSize(TypeDonkey) != 6 || maxSpawnClusterSize(TypePillager) != 1 {
		t.Errorf("cluster sizes wolf %d horse %d donkey %d pillager %d, want 8, 6, 6 and 1",
			maxSpawnClusterSize(TypeWolf), maxSpawnClusterSize(TypeHorse),
			maxSpawnClusterSize(TypeDonkey), maxSpawnClusterSize(TypePillager))
	}
}

// oceanAround is a flat ocean of water from y 55 to 63 over the 81 by 81
// columns around the origin, a stone bed under it.
func oceanAround(t *testing.T) spawnTestWorld {
	t.Helper()
	w := newSpawnTestWorld()
	w.biomeName = "minecraft:ocean"
	water := defaultState(t, "minecraft:water")
	for x := int32(-40); x <= 40; x++ {
		for z := int32(-40); z <= 40; z++ {
			w.groundAt(t, x, 54, z)
			for y := int32(55); y <= 63; y++ {
				w.set(x, y, z, water)
			}
		}
	}
	return w
}

// TestAFishPackStaysUnderItsClusterSize throws the water-ambient pass at one
// position over many seeds: a pack of cod is three to six, three packs may
// follow one another, and the position stops at the fish's cluster size of
// eight, which the draws do reach.
// vanilla: AbstractFish.getMaxSpawnClusterSize, NaturalSpawner.spawnCategoryForPosition
func TestAFishPackStaysUnderItsClusterSize(t *testing.T) {
	w := oceanAround(t)
	players := []SpawnPlayer{{EntityID: 1, X: 40.5, Y: 62, Z: 0.5}}
	most := 0
	for seed := int64(1); seed <= 3000; seed++ {
		var reqs []SpawnRequest
		var bodies []SpawnBody
		spawnCategoryForPosition(w, readSpawnPoint(w), rand.New(rand.NewSource(seed)), ChunkPos{}, 62, 0, 0,
			players, &bodies, &SpawnPotential{}, &SpawnCounts{PerPlayer: make(map[int32]int)}, CategoryWaterAmbient,
			func(r SpawnRequest) { reqs = append(reqs, r) })
		for _, r := range reqs {
			if r.Type != TypeCod {
				t.Fatalf("seed %d: the ocean stood up a %s", seed, r.Type)
			}
		}
		most = max(most, len(reqs))
	}
	if most != 8 {
		t.Errorf("the largest pack at one position was %d, want exactly the cluster size of 8", most)
	}
}

// TestFishAreNotSpawnedBeyondSixtyFourBlocks: a player a hundred blocks off
// sees squid appear (the category's despawn distance is 128) and cod not (64).
// vanilla: NaturalSpawner.isValidSpawnPostitionForType, MobCategory.getDespawnDistance
func TestFishAreNotSpawnedBeyondSixtyFourBlocks(t *testing.T) {
	w := oceanAround(t)
	players := []SpawnPlayer{{EntityID: 1, X: 100.5, Y: 62, Z: 0.5}}
	var cod, squid int
	for seed := int64(1); seed <= 3000; seed++ {
		for _, c := range []SpawnCategory{CategoryWaterAmbient, CategoryWaterCreature} {
			var bodies []SpawnBody
			spawnCategoryForPosition(w, readSpawnPoint(w), rand.New(rand.NewSource(seed)), ChunkPos{}, 62, 0, 0,
				players, &bodies, &SpawnPotential{}, &SpawnCounts{PerPlayer: make(map[int32]int)}, c,
				func(r SpawnRequest) {
					switch r.Type {
					case TypeCod:
						cod++
					case TypeSquid:
						squid++
					}
				})
		}
	}
	if cod != 0 {
		t.Errorf("%d cod spawned for a player 100 blocks away, want none past 64", cod)
	}
	if squid == 0 {
		t.Error("no squid spawned for a player 100 blocks away, want the 128 of their category")
	}
}

// TestABucketedFishIsNotCountedAgainstTheCap: a fish a bucket let out is
// persistent and stands outside the category's count.
// vanilla: NaturalSpawner.createState, AbstractFish.requiresCustomPersistence
func TestABucketedFishIsNotCountedAgainstTheCap(t *testing.T) {
	wild := NewMob(TypeCod, 1, 0.5, 62, 0.5, 3)
	bucketed := NewMob(TypeCod, 2, 0.5, 62, 0.5, 3)
	bucketed.FromBucket = true
	if !wild.CountsForSpawnCap() || bucketed.CountsForSpawnCap() {
		t.Errorf("a wild cod counts %v and a bucketed one %v, want true and false", wild.CountsForSpawnCap(), bucketed.CountsForSpawnCap())
	}
}

// TestAColdOceanSpawnsSalmonAndCod: the cold ocean's water-ambient list holds
// cod and salmon at equal weight, so over many seeds both appear, in packs
// within each one's own size.
// vanilla: OverworldBiomes.coldOcean, AbstractFish.getMaxSpawnClusterSize
func TestAColdOceanSpawnsSalmonAndCod(t *testing.T) {
	w := oceanAround(t)
	w.biomeName = "minecraft:cold_ocean"
	players := []SpawnPlayer{{EntityID: 1, X: 40.5, Y: 62, Z: 0.5}}
	var cod, salmon int
	for seed := int64(1); seed <= 3000; seed++ {
		var bodies []SpawnBody
		spawnCategoryForPosition(w, readSpawnPoint(w), rand.New(rand.NewSource(seed)), ChunkPos{}, 62, 0, 0,
			players, &bodies, &SpawnPotential{}, &SpawnCounts{PerPlayer: make(map[int32]int)}, CategoryWaterAmbient,
			func(r SpawnRequest) {
				switch r.Type {
				case TypeCod:
					cod++
				case TypeSalmon:
					salmon++
				}
			})
	}
	if cod == 0 || salmon == 0 {
		t.Errorf("cold ocean spawned %d cod and %d salmon, want both", cod, salmon)
	}
}

// TestANamedFishDoesNotDespawn: a wild fish far from every player is
// discarded, a named one or a bucketed one stands.
// vanilla: AbstractFish.removeWhenFarAway
func TestANamedFishDoesNotDespawn(t *testing.T) {
	w := newTestWorld()
	far := []Player{survivor(7, 200.5)}
	wild := NewMob(TypeSalmon, 1, 0.5, 64, 0.5, 3)
	if !wild.despawnStep(w, far) {
		t.Fatal("a wild salmon beyond the despawn distance stands, want it discarded")
	}
	named := NewMob(TypeSalmon, 2, 0.5, 64, 0.5, 3)
	named.Named = true
	if named.despawnStep(w, far) {
		t.Error("a named salmon was despawned")
	}
}
