package world

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// BenchmarkLoadChunkEntities includes the region read, decompression and NBT
// decoding of saved passenger trees, without terrain generation or mob wake.
func BenchmarkLoadChunkEntities(b *testing.B) {
	for _, count := range []int{1, 16} {
		b.Run(fmt.Sprintf("%dtrees", count), func(b *testing.B) {
			dir := b.TempDir()
			w, err := NewWorld(dir, NewFlatGenerator())
			if err != nil {
				b.Fatal(err)
			}
			// Each tree has a saddled horse, an equipped skeleton rider and
			// a parrot riding the skeleton, including nested item components.
			fixture, err := nbt.ParseSNBT(`{
				id:"minecraft:horse", UUID:[I;1,2,3,4],
				Pos:[5.5d,64d,5.5d], Motion:[0d,0d,0d], Rotation:[0f,0f],
				Health:30f, Air:300s, Fire:-1s, OnGround:1b,
				equipment:{saddle:{id:"minecraft:saddle",count:1}},
				Passengers:[{
					id:"minecraft:skeleton", UUID:[I;5,6,7,8],
					Pos:[5.5d,65.5d,5.5d], Motion:[0d,0d,0d], Rotation:[0f,0f],
					Health:20f, Air:300s, Fire:-1s, OnGround:0b,
					equipment:{
						mainhand:{id:"minecraft:bow",count:1,components:{
							"minecraft:damage":12,"minecraft:enchantments":{"minecraft:power":2},
							"minecraft:custom_data":{nested:{v:1,f:2}}
						}},
						head:{id:"minecraft:iron_helmet",count:1,components:{"minecraft:damage":3}}
					},
					Passengers:[{
						id:"minecraft:parrot",UUID:[I;9,10,11,12],Variant:2,
						Pos:[5.5d,67d,5.5d], Motion:[0d,0d,0d], Rotation:[0f,0f],
						Health:6f, Air:300s, Fire:-1s, OnGround:0b
					}]
				}]
			}`)
			if err != nil {
				b.Fatal(err)
			}
			entities := make([]nbt.Tag, count)
			for i := range entities {
				tree := nbt.Copy(fixture).(nbt.Compound)
				tree["UUID"] = nbt.IntArray{1, 2, 3, int32(i*3 + 4)}
				rider := tree["Passengers"].(nbt.List).Items[0].(nbt.Compound)
				rider["UUID"] = nbt.IntArray{1, 2, 3, int32(i*3 + 5)}
				rider["Passengers"].(nbt.List).Items[0].(nbt.Compound)["UUID"] = nbt.IntArray{1, 2, 3, int32(i*3 + 6)}
				entities[i] = tree
			}
			pos := ChunkPos{}
			if err := w.SaveChunkEntities(pos, entities); err != nil {
				b.Fatal(err)
			}
			if err := w.Close(); err != nil {
				b.Fatal(err)
			}
			w, err = NewWorld(dir, NewFlatGenerator())
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = w.Close() })
			loaded, present, err := w.LoadChunkEntities(pos)
			if err != nil || !present || !nbt.Equal(nbt.List{Elem: nbt.TypeCompound, Items: loaded}, nbt.List{Elem: nbt.TypeCompound, Items: entities}) {
				b.Fatalf("saved passenger trees did not round-trip: %v", err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				loaded, present, err := w.LoadChunkEntities(pos)
				if err != nil || !present || len(loaded) != count {
					b.Fatalf("entity region did not load: %v", err)
				}
			}
		})
	}
}
