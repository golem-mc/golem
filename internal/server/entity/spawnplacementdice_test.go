package entity

import (
	"crypto/sha256"
	"fmt"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

type placementDiceWorld struct {
	spawnTestWorld
	ground uint16
}

func (w placementDiceWorld) BlockAt(x, y, z int32) uint16 {
	if y == 63 {
		return w.ground
	}
	if y < 63 {
		return registry.BlockStone.DefaultState()
	}
	return registry.BlockAir.DefaultState()
}
func (w placementDiceWorld) BlocksIn(x0, y0, z0, x1, y1, z1 int32, buf []uint16) []uint16 {
	return blocksVia(w.BlockAt, x0, y0, z0, x1, y1, z1, buf)
}

// Baselines recorded before optimization cover unrestricted placement, the
// monster-list ocelot, and an entry whose kind is not simulated.
func TestNaturalSpawnPlacementDice(t *testing.T) {
	for _, tc := range []struct {
		name, biome string
		category    SpawnCategory
		ground      registry.Block
		unsupported bool
		want        string
	}{
		{"fox-snow", "minecraft:taiga", CategoryCreature, registry.BlockSnow, false, "303:aef9077da02d6b47c1d2a89e119a45413217294d5c744d34d12562abde6ab1b9"},
		{"panda", "minecraft:bamboo_jungle", CategoryCreature, registry.BlockGrassBlock, false, "1168:dc1e1d69229d75ee0d5327f50f878868190abfafc35139569e325fef782075d7"},
		{"ocelot", "minecraft:bamboo_jungle", CategoryMonster, registry.BlockGrassBlock, false, "1167:a68aefc532a602e2ba3745cb33b18c48ab2d89428bd60f3b6c4824b422b49e05"},
		{"unsupported", "minecraft:plains", CategoryMonster, registry.BlockGrassBlock, true, "1124:74a2c9465932658698e6e6842dd4122ecf1c7231faf09f3b848e804a3e10e787"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.unsupported {
				k := kinds[TypeZombie]
				kinds[TypeZombie] = nil
				defer func() { kinds[TypeZombie] = k }()
			}
			w := placementDiceWorld{spawnTestWorld: newSpawnTestWorld(), ground: tc.ground.DefaultState()}
			w.biomeName = tc.biome
			w.sky = 15
			w.darken = 0
			if tc.category == CategoryMonster {
				w.sky = 0
			}
			hash := sha256.New()
			spawned := 0
			for seed := int64(1); seed <= 300; seed++ {
				rng := rand.New(rand.NewSource(seed))
				var bodies []SpawnBody
				spawnCategoryForPosition(w, readSpawnPoint(w), rng, ChunkPos{}, 64, 0, 0,
					[]SpawnPlayer{{EntityID: 1, X: 40, Y: 64}}, &bodies, &SpawnPotential{}, &SpawnCounts{}, tc.category, func(r SpawnRequest) {
						fmt.Fprintf(hash, "%s %g %g %g %g\n", r.Type.Name(), r.X, r.Y, r.Z, r.Yaw)
						spawned++
					})
				fmt.Fprintf(hash, "next %d\n", rng.Int63())
			}
			got := fmt.Sprintf("%d:%x", spawned, hash.Sum(nil))
			if got != tc.want {
				t.Fatalf("placement dice = %s, want %s", got, tc.want)
			}
		})
	}
}
