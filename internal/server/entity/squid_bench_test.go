package entity

import (
	"math/rand"
	"testing"
)

// BenchmarkMobsTickSquids steps a dimension's herd through a tick: a school
// of a hundred and fifty squids in a pool, none of them struck, beside three
// thousand cows on the stone around it. The squid's flee goal asks for its
// attacker every tick, and the attacker is looked for among the whole herd.
// The IDs start at one, as the server's do.
func BenchmarkMobsTickSquids(b *testing.B) {
	stone := defaultState(b, "minecraft:stone")
	water := defaultState(b, "minecraft:water")
	w := newTestWorld()
	floor(w, -257, -257, 256, 256, 63, stone)
	for y := int32(48); y <= 62; y++ {
		floor(w, -32, -32, 32, 32, y, water)
	}
	rng := rand.New(rand.NewSource(1544))
	mobs := make(Mobs, 0, 3150)
	id := int32(1)
	for range 150 {
		x, y, z := (rng.Float64()-0.5)*60, 50+rng.Float64()*10, (rng.Float64()-0.5)*60
		mobs = append(mobs, NewMob(TypeSquid, id, x, y, z, int64(id)))
		id++
	}
	for len(mobs) < cap(mobs) {
		x, z := (rng.Float64()-0.5)*512, (rng.Float64()-0.5)*512
		if x > -40 && x < 40 && z > -40 && z < 40 {
			continue // clear of the pool
		}
		mobs = append(mobs, NewMob(TypeCow, id, x, 64, z, int64(id)))
		id++
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, nil, nil)
	}
}

// BenchmarkSquidAttackerUnstruck is the flee goal's attacker lookup for a
// squid nobody struck, in a herd of three thousand: what every squid of a
// loaded dimension asks every tick.
func BenchmarkSquidAttackerUnstruck(b *testing.B) {
	others := make([]*Mob, 3000)
	for i := range others {
		others[i] = NewMob(TypeCow, int32(i+1), float64(i), 64, 0, int64(i))
	}
	squid := NewMob(TypeSquid, 3001, 0, 60, 0, 1)
	t := &tickView{others: others}
	b.ResetTimer()
	for range b.N {
		if squid.squidAttacker(t) != nil {
			b.Fatal("an unstruck squid named an attacker")
		}
	}
}
