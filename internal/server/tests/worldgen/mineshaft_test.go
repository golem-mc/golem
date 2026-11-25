package worldgen

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The seed-0 corridor crosses both chunks. The first chunk to decorate it
// places its spawner, using that chunk's random stream.
// vanilla: MineShaftCorridor.postProcess
func TestMineshaftSpiderSpawnerDecorationOrder(t *testing.T) {
	south := world.ChunkPos{X: 8, Z: -11}
	north := world.ChunkPos{X: 8, Z: -12}
	for _, tc := range []struct {
		name  string
		order []world.ChunkPos
		want  [3]int32
	}{
		{"south first", []world.ChunkPos{south, north}, [3]int32{129, -29, -173}},
		{"north first", []world.ChunkPos{north, south}, [3]int32{129, -29, -179}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gen := world.NewOverworldGenerator(0)
			w, err := world.NewWorld(t.TempDir(), gen)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := w.Close(); err != nil {
					t.Error(err)
				}
			})

			gen.DecorateInOrder(tc.order)
			var got [][3]int32
			for _, pos := range []world.ChunkPos{north, south} {
				c, _ := w.Acquire(pos)
				defer w.Release(pos)
				for _, tag := range c.BlockEntitiesNBT() {
					id, _ := nbt.Get[nbt.String](tag, "id")
					spawn, _ := nbt.Get[nbt.Compound](tag, "SpawnData")
					entity, _ := nbt.Get[nbt.Compound](spawn, "entity")
					mob, _ := nbt.Get[nbt.String](entity, "id")
					if id != "minecraft:mob_spawner" || mob != "minecraft:cave_spider" {
						continue
					}
					x, _ := nbt.IntField(tag, "x")
					y, _ := nbt.IntField(tag, "y")
					z, _ := nbt.IntField(tag, "z")
					got = append(got, [3]int32{x, y, z})
					if registry.BlockOf(c.BlockAt(x&15, y, z&15)) != registry.BlockSpawner {
						t.Errorf("cave-spider block entity at (%d, %d, %d) has no spawner block", x, y, z)
					}
				}
			}
			if !slices.Equal(got, [][3]int32{tc.want}) {
				t.Errorf("cave-spider spawners at %v, want exactly one at %v", got, tc.want)
			}
		})
	}
}
