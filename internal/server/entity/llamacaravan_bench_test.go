package entity

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkMobsTickLlamas steps two thousand unleashed llamas, none in a
// caravan, through a tick: the caravan goal every one of them asks.
func BenchmarkMobsTickLlamas(b *testing.B) {
	stone := defaultState(b, "minecraft:stone")
	w := newTestWorld()
	floor(w, -129, -129, 128, 128, 63, stone)
	rng := rand.New(rand.NewSource(527))
	mobs := make(Mobs, 0, 2000)
	for id := int32(1); len(mobs) < cap(mobs); id++ {
		x, z := (rng.Float64()-0.5)*256, (rng.Float64()-0.5)*256
		mobs = append(mobs, NewMob(TypeLlama, id, x, 64, z, int64(id)))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, nil, nil)
	}
}

// BenchmarkMobsTickLlamasLeashed is BenchmarkMobsTickLlamas with every tenth
// llama on a lead held a block away, spread over the area so most pairs are
// far apart; more leashed llamas than caravanGridMinList keeps the grid
// fallback in the timed loop.
func BenchmarkMobsTickLlamasLeashed(b *testing.B) {
	stone := defaultState(b, "minecraft:stone")
	w := newTestWorld()
	floor(w, -129, -129, 128, 128, 63, stone)
	rng := rand.New(rand.NewSource(527))
	mobs := make(Mobs, 0, 2000)
	var holders []LeashHolder
	for id := int32(1); len(mobs) < cap(mobs); id++ {
		x, z := (rng.Float64()-0.5)*256, (rng.Float64()-0.5)*256
		m := NewMob(TypeLlama, id, x, 64, z, int64(id))
		if id%10 == 0 {
			m.LeashHolderID = 10000 + id
			holders = append(holders, LeashHolder{EntityID: m.LeashHolderID, X: x + 1, Y: 64, Z: z,
				Width: 0.375, Height: 0.5, Present: true, SameLevel: true, Knot: true})
		}
		if id == 10 {
			// One scale-16 lead widens the reach of every grid query.
			m.SetAttributes(m.BuildAttributes(nbt.List{}))
			m.attrs.SetBase(registry.AttributeScale, 16)
		}
		mobs = append(mobs, m)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, nil, holders)
	}
	b.StopTimer()
	leashed := 0
	for _, m := range mobs {
		if m.leashed() {
			leashed++
		}
	}
	if leashed <= caravanGridMinList {
		b.Fatalf("%d llamas still leashed after the run, want more than %d", leashed, caravanGridMinList)
	}
}
