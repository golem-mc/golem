package entity

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

type spawnBoundaryWorld struct {
	spawnTestWorld
	water bool
}

func (w spawnBoundaryWorld) Biome(x, y, z int32) registry.Biome {
	if w.water {
		if x&16 == 0 {
			return registry.BiomeRiver
		}
		return registry.BiomeOcean
	}
	if x&16 == 0 {
		return registry.BiomePlains
	}
	return registry.BiomeSwamp
}
func (w spawnBoundaryWorld) BlockAt(x, y, z int32) uint16 {
	floor := int32(30)
	if w.water {
		floor = 55
	}
	if y < floor {
		return registry.BlockGrassBlock.DefaultState()
	}
	if w.water && y < 59 {
		return registry.BlockWater.DefaultState()
	}
	return registry.BlockAir.DefaultState()
}
func (w spawnBoundaryWorld) BlocksIn(x0, y0, z0, x1, y1, z1 int32, buf []uint16) []uint16 {
	return blocksVia(w.BlockAt, x0, y0, z0, x1, y1, z1, buf)
}
func (w spawnBoundaryWorld) StructureContains(x, y, z int32, id string, piece bool) bool {
	return id == "minecraft:swamp_hut" && piece && x >= 8 && x <= 24 && y >= 30 && y <= 32
}

// TestNaturalSpawnBoundaryDice pins both the emitted requests and the next RNG
// draw across biome and structure boundaries, including reduced river spawns.
func TestNaturalSpawnBoundaryDice(t *testing.T) {
	for _, water := range []bool{false, true} {
		t.Run(fmt.Sprint(water), func(t *testing.T) {
			w := spawnBoundaryWorld{spawnTestWorld: newSpawnTestWorld(), water: water}
			w.surface = 33
			if water {
				w.surface = 58
			}
			w.sky = 0
			hash := sha256.New()
			requests := 0
			for seed := int64(1); seed <= 300; seed++ {
				rng := rand.New(rand.NewSource(seed))
				var chunks []ChunkPos
				for x := int32(-2); x <= 3; x++ {
					for z := int32(-1); z <= 1; z++ {
						chunks = append(chunks, ChunkPos{X: x, Z: z})
					}
				}
				passes := []SpawnPass{MonsterPass(&SpawnCounts{}), CreaturePass(&SpawnCounts{}), AmbientPass(&SpawnCounts{}), WaterCreaturePass(&SpawnCounts{}), WaterAmbientPass(&SpawnCounts{})}
				SpawnNaturally(w, rng, chunks, []SpawnPlayer{{EntityID: 1, X: 80, Y: 30, Z: 0}}, nil, &SpawnPotential{}, passes, 289, func(r SpawnRequest) {
					fmt.Fprintf(hash, "%s %g %g %g %g\n", r.Type.Name(), r.X, r.Y, r.Z, r.Yaw)
					requests++
				})
				fmt.Fprintf(hash, "next %d\n", rng.Int63())
			}
			got := fmt.Sprintf("%d:%x", requests, hash.Sum(nil))
			want := "207:568a273802858d871c1f531934ed0de4bb9fc4ebff4a1b8c5f8ff6aeb84ab73b"
			if water {
				want = "429:87ab80a85218e97b0609a0e977268c1a0502f35072d3f804633f5fbe5b7c56ab"
			}
			if got != want {
				t.Fatalf("seeded boundary outcome = %s, want %s", got, want)
			}
		})
	}
}
