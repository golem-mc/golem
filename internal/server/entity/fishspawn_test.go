package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// TestWaterAmbientCategoryNumbers pins the category's cap, despawn distance
// and the fish's cluster sizes.
// vanilla: MobCategory.WATER_AMBIENT, AbstractFish.getMaxSpawnClusterSize
func TestWaterAmbientCategoryNumbers(t *testing.T) {
	if registry.MobCategoryWaterAmbient.Max() != 20 {
		t.Errorf("the cap is %d, want 20", registry.MobCategoryWaterAmbient.Max())
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
		var bodies SpawnBodies
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
