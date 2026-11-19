package world

import (
	"math/rand"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// bonusChestLootTable is the chest's loot table.
const bonusChestLootTable = "minecraft:chests/spawn_bonus_chest"

// PlaceBonusChest puts the bonus chest and its torches in the spawn's chunk and
// reports whether a column took it. The rng is the level random.
// vanilla: BonusChestFeature.place
func (w *World) PlaceBonusChest(rng *rand.Rand) bool {
	spawn := w.Spawn()
	pos := ChunkPos{X: spawn.X >> 4, Z: spawn.Z >> 4}
	c, _ := w.Acquire(pos)
	defer w.Release(pos)
	// The neighbours must be loaded: a torch on a chunk-edge column reads its
	// support block from the next chunk, and an unloaded one reads as air.
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			if dx != 0 || dz != 0 {
				n := ChunkPos{X: pos.X + dx, Z: pos.Z + dz}
				w.Acquire(n)
				defer w.Release(n)
			}
		}
	}

	// vanilla: Util.toShuffledList
	xs, zs := make([]int32, 16), make([]int32, 16)
	for i := range xs {
		xs[i], zs[i] = pos.X*16+int32(i), pos.Z*16+int32(i)
	}
	rng.Shuffle(len(xs), func(i, j int) { xs[i], xs[j] = xs[j], xs[i] })
	rng.Shuffle(len(zs), func(i, j int) { zs[i], zs[j] = zs[j], zs[i] })

	for _, x := range xs {
		for _, z := range zs {
			// vanilla: Heightmap.Types.MOTION_BLOCKING_NO_LEAVES
			_, _, top := w.heightmapColumn(c, x&15, z&15)
			y := top + w.dim.minY
			if !IsAirState(w.BlockAt(x, y, z)) && !w.CollisionEmpty(x, y, z) {
				continue
			}
			w.SetBlock(x, y, z, registry.BlockChest.DefaultState())
			w.SetBlockEntityTag(x, y, z, "minecraft:chest", nbt.Compound{
				"LootTable":     nbt.String(bonusChestLootTable),
				"LootTableSeed": nbt.Long(rng.Int63()),
			})
			torch := registry.BlockTorch.DefaultState()
			for _, d := range [4][2]int32{{0, -1}, {1, 0}, {0, 1}, {-1, 0}} {
				if tx, tz := x+d[0], z+d[1]; w.Survives(tx, y, tz, torch) {
					w.SetBlock(tx, y, tz, torch)
				}
			}
			return true
		}
	}
	return false
}
