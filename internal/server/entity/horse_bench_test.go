package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

// BenchmarkMobsTickHorses steps a herd of three hundred tame horses,
// donkeys and mules, none in love and no players about, through a tick: the
// breed and tempt goals every one of them asks each tick.
func BenchmarkMobsTickHorses(b *testing.B) {
	stone := defaultState(b, "minecraft:stone")
	w := newTestWorld()
	floor(w, -129, -129, 128, 128, 63, stone)
	rng := rand.New(rand.NewSource(1544))
	kinds := []MobType{TypeHorse, TypeDonkey, TypeMule}
	mobs := make(Mobs, 0, 300)
	for id := int32(1); len(mobs) < cap(mobs); id++ {
		x, z := (rng.Float64()-0.5)*256, (rng.Float64()-0.5)*256
		m := NewMob(kinds[int(id)%len(kinds)], id, x, 64, z, int64(id))
		m.Horse.Tame = true
		mobs = append(mobs, m)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, nil, nil)
	}
}

// BenchmarkRiddenHorseMoveCollision measures the collision query for a driver's report.
func BenchmarkRiddenHorseMoveCollision(b *testing.B) {
	for _, tc := range []struct {
		name string
		x    float64
	}{
		{"open", 5.2},
		{"flush", 8 - float64(float32(1.3964844))/2},
		{"inside", 7.5},
	} {
		b.Run(tc.name, func(b *testing.B) {
			w := world.NewDimensionWorld(b.TempDir(), true, world.NewFlatGenerator())
			defer w.Close()
			w.Acquire(world.ChunkPos{})
			w.SetBlock(8, 64, 5, defaultState(b, "minecraft:stone"))
			m := NewMob(TypeHorse, 1, 5.5, 64, 5.5, 1)
			b.ReportAllocs()
			for b.Loop() {
				m.RejectsMove(w, 5.5, 64, 5.5, tc.x, 64, 5.5)
			}
		})
	}
}
