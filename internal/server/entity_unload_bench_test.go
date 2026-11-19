package server

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func BenchmarkEntityChunkUnload(b *testing.B) {
	w, err := world.NewWorld(b.TempDir(), world.NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	if err := w.SetSaveFormat("deflate", true); err != nil {
		b.Fatal(err)
	}
	h := &mobHerd{w: w, listed: make(map[world.ChunkPos]bool)}
	tags := []nbt.Tag{nbt.Compound{"id": nbt.String("minecraft:pig"), "Health": nbt.Float(10)}}
	b.ReportAllocs()
	var tickTime time.Duration
	b.ResetTimer()
	for range b.N {
		start := time.Now()
		for x := range int32(100) {
			h.boxMobs(world.ChunkPos{X: x}, nil, tags, nil)
		}
		tickTime += time.Since(start)
		// Include draining in calibration so asynchronous submission cannot make
		// a one-second benchmark queue minutes of unmeasured fsync work.
		w.FlushSaves()
	}
	b.ReportMetric(float64(tickTime.Nanoseconds())/float64(b.N), "tick-ns/op")
}

func BenchmarkEntityChunkUnloadPassengers(b *testing.B) {
	for _, name := range []string{"Snapshot", "QueuedUnload"} {
		b.Run(name, func(b *testing.B) {
			w, err := world.NewWorld(b.TempDir(), world.NewFlatGenerator())
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = w.Close() })
			if err := w.SetSaveFormat("deflate", true); err != nil {
				b.Fatal(err)
			}
			stand := nbt.Compound{
				"id":   nbt.String("minecraft:armor_stand"),
				"UUID": nbt.UUIDToIntArray(uuid.MustParse("00000000-0000-0000-0000-000000000003")),
				"Pose": nbt.Compound{"Head": nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(10), nbt.Float(20), nbt.Float(30)}}},
			}
			cart := &minecartEntity{
				uuid:              uuid.MustParse("00000000-0000-0000-0000-000000000001"),
				kind:              registry.EntityTypeMinecart,
				cart:              entity.NewMinecart(1, 5.5, 64, 5.5),
				carriedPassengers: []nbt.Tag{stand},
			}
			villager := &mobEntity{
				uuid: uuid.MustParse("00000000-0000-0000-0000-000000000002"),
				mob:  entity.NewMob(entity.TypeVillager, 2, 5.5, 64, 5.5, 1),
			}
			h := &mobHerd{
				w:      w,
				listed: make(map[world.ChunkPos]bool),
				book:   map[*entity.Mob]*mobEntity{villager.mob: villager},
			}
			mobs := []*entity.Mob{villager.mob}
			tags := []nbt.Tag{minecartTag(cart)}
			// Unloading detaches the mob before saving; steppedOff retains its seat.
			steppedOff := map[*mobEntity]uuid.UUID{villager: cart.uuid}
			b.ReportAllocs()
			var tickTime time.Duration
			b.ResetTimer()
			for range b.N {
				start := time.Now()
				for x := range int32(100) {
					pos := world.ChunkPos{X: x}
					if name == "QueuedUnload" {
						h.boxMobs(pos, mobs, tags, steppedOff)
					} else {
						_ = h.chunkEntities(pos, mobs, tags, steppedOff)
					}
				}
				tickTime += time.Since(start)
				if name == "QueuedUnload" {
					// Include disk draining in calibration, as in BenchmarkEntityChunkUnload.
					w.FlushSaves()
				}
			}
			b.ReportMetric(float64(tickTime.Nanoseconds())/float64(b.N), "tick-ns/op")
		})
	}
}
