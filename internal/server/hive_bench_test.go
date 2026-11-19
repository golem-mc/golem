package server

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkBeeEnterHive includes removing one bee from a herd of 256 bodies.
func BenchmarkBeeEnterHive(b *testing.B) {
	w, err := world.NewWorld(b.TempDir(), world.NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	pos := world.ChunkPos{}
	w.Acquire(pos)
	b.Cleanup(func() {
		w.Release(pos)
		_ = w.Close()
	})
	w.SetBlock(5, 64, 2, registry.BlockBeeNest.DefaultState())
	w.SetBlockEntityTag(5, 64, 2, "minecraft:beehive", nbt.Compound{})
	if n, ok := w.BeehiveOccupants(5, 64, 2); !ok || n != 0 {
		b.Fatal("missing empty nest")
	}
	h := &mobHerd{w: w, book: make(map[*entity.Mob]*mobEntity), byUUID: make(map[uuid.UUID]*entity.Mob)}
	for i := range 255 {
		h.add(entity.NewMob(entity.TypeCow, int32(i+1), 0.5, 64, 0.5, int64(i)))
	}
	m := entity.NewMob(entity.TypeBee, 256, 5.5, 64, 1.5, 1)
	ent := &mobEntity{mob: m, w: w, uuid: uuid.New()}
	h.add(m)
	bodies := slices.Clone(h.herd)
	tick := mobDimTick{h: h, w: w}
	fx := mobEffects{removals: make(map[*world.World][]int32)}
	event := entity.MobEvent{Mob: m, Kind: entity.MobEnteredHive, X: 5, Y: 64, Z: 2}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		h.herd = append(h.herd[:0], bodies...)
		h.remember(ent)
		clear(fx.hiveStays)
		fx.hiveStays = fx.hiveStays[:0]
		fx.scoreDrops = fx.scoreDrops[:0]
		fx.removals[w] = fx.removals[w][:0]
		tick.enteredHive(&fx, event)
	}
}
