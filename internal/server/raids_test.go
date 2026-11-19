package server

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func TestRaidSpawnRequiresAllNearbyChunks(t *testing.T) {
	for _, origin := range []int32{6, -7} {
		for _, offset := range []world.ChunkPos{{X: 0, Z: 1}, {X: 1, Z: 0}, {X: 1, Z: 1}, {X: 1, Z: 2}, {X: 2, Z: 1}} {
			t.Run(fmt.Sprintf("%d/%d/%d", origin, offset.X, offset.Z), func(t *testing.T) {
				w, err := world.NewWorld(t.TempDir(), world.NewFlatGenerator())
				if err != nil {
					t.Fatal(err)
				}
				defer w.Close()
				start := (origin - 10) >> 4
				missing := world.ChunkPos{X: start + offset.X, Z: start + offset.Z}
				for cx := start; cx <= start+2; cx++ {
					for cz := start; cz <= start+2; cz++ {
						pos := world.ChunkPos{X: cx, Z: cz}
						if pos == missing {
							continue
						}
						w.Acquire(pos)
						defer w.Release(pos)
					}
				}
				w.SetBlock(origin, 99, origin, registry.BlockStone.DefaultState())
				level := raidLevel{dim: &dimension{w: w}}
				if level.CanSpawnAt(int(origin), 100, int(origin)) {
					t.Fatal("spawn accepted with a missing interior or edge chunk")
				}
				w.Acquire(missing)
				defer w.Release(missing)
				w.SetBlock(origin, 99, origin, registry.BlockStone.DefaultState())
				if !level.CanSpawnAt(int(origin), 100, int(origin)) {
					t.Fatal("spawn refused with every required chunk loaded and stone below air")
				}
				w.SetBlock(origin, 99, origin, registry.MustState("minecraft:snow", map[string]string{"layers": "1"}))
				if !level.CanSpawnAt(int(origin), 100, int(origin)) {
					t.Fatal("spawn refused above one snow layer")
				}
				w.SetBlock(origin, 99, origin, registry.BlockGlass.DefaultState())
				if level.CanSpawnAt(int(origin), 100, int(origin)) {
					t.Fatal("spawn accepted above glass")
				}
			})
		}
	}
}
