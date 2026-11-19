package entity

import (
	"math/rand"
	"testing"

	"github.com/google/uuid"
)

// BenchmarkMobsTickCamels steps a herd of three hundred camels, a third of
// them sitting, with no players about, through a tick: the camel goals and
// pose rules each of them runs.
func BenchmarkMobsTickCamels(b *testing.B) {
	stone := defaultState(b, "minecraft:stone")
	w := newTestWorld()
	floor(w, -129, -129, 128, 128, 63, stone)
	rng := rand.New(rand.NewSource(3620))
	mobs := make(Mobs, 0, 300)
	for id := int32(1); len(mobs) < cap(mobs); id++ {
		x, z := (rng.Float64()-0.5)*256, (rng.Float64()-0.5)*256
		m := NewMob(TypeCamel, id, x, 64, z, int64(id))
		if id%3 == 0 {
			m.camel.lastPose = -1
		}
		mobs = append(mobs, m)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, nil, nil)
	}
}

// BenchmarkMobsTickRiddenCamels includes travel and player collision handling
// for sixty-four saddled camels, each carrying one player.
func BenchmarkMobsTickRiddenCamels(b *testing.B) {
	w := newTestWorld()
	floor(w, -2, -2, 66, 66, 63, defaultState(b, "minecraft:stone"))
	mobs := make(Mobs, 64)
	players := make([]Player, len(mobs))
	for i := range mobs {
		m := NewMob(TypeCamel, int32(i+1), float64(i%8)*8+0.5, 64, float64(i/8)*8+0.5, int64(i))
		m.GameTime, m.OnGround = 1000, true
		m.Horse.Saddle = saddleStack()
		m.Board(uuid.New())
		m.Horse.RiderID = int32(1000 + i)
		x, y, z := PlayerSeat(m, 0)
		players[i] = Player{EntityID: m.Horse.RiderID, X: x, Y: y, Z: z, Passenger: true, Pushable: true}
		mobs[i] = m
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, players, nil)
	}
}
