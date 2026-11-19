package server

import (
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"testing"
)

// BenchmarkIdleBlockEntities is one block entity tick over 16384 block
// entities of one kind that have nothing to do.
func BenchmarkIdleBlockEntities(b *testing.B) {
	const side = 128
	y := int32(world.GroundLevel + 3)
	for _, kind := range []struct {
		name, block string
		kind        world.BlockEntityKind
	}{
		{"chest", "minecraft:barrel", world.KindBarrel},
		{"furnace", "minecraft:furnace", world.KindFurnace},
		{"brewing_stand", "minecraft:brewing_stand", world.KindBrewingStand},
		{"sign", "minecraft:oak_sign", world.KindSign},
		{"campfire", "minecraft:campfire", world.KindCampfire},
		{"jukebox", "minecraft:jukebox", world.KindJukebox},
		{"crafter", "minecraft:crafter", world.KindCrafter},
		{"beacon", "minecraft:beacon", world.KindBeacon},
		{"spawner", "minecraft:spawner", world.KindSpawner},
		{"daylight", "minecraft:daylight_detector", world.KindDaylightDetector},
		{"conduit", "minecraft:conduit", world.KindConduit},
	} {
		b.Run(kind.name, func(b *testing.B) {
			s, w := newBlockBenchServer(b, side/16)
			state := registry.MustState(kind.block, nil)
			for x := range int32(side) {
				for z := range int32(side) {
					w.SetBlock(x, y, z, state)
					w.EnsureBlockEntity(x, y, z, kind.kind)
				}
			}
			w.FlushLighting()
			for range 3 {
				s.tickBlockEntities()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tickBlockEntities()
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/(side*side), "ns/entity")
		})
	}
	b.Run("none_1024chunks", func(b *testing.B) {
		s, _ := newBlockBenchServer(b, 30)
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			s.tickBlockEntities()
		}
	})
}
