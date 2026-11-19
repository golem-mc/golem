package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
)

// BenchmarkFrogTongues steps sixteen frogs with nearby small cubes.
func BenchmarkFrogTongues(b *testing.B) {
	w := newTestWorld()
	floor(w, -40, -40, 40, 40, 63, defaultState(b, "minecraft:stone"))
	var mobs Mobs
	for i := range 16 {
		x, z := float64(i%4)*8, float64(i/4)*8
		frog := NewMob(TypeFrog, int32(2*i+1), x, 64, z, int64(i+1))
		frog.Persistence = true
		frog.RestrictTo(int32(x), 64, int32(z), 3)
		prey := NewMob(TypeMagmaCube, int32(2*i+2), x+1, 64, z, int64(i+1))
		prey.SetSize(1, true)
		prey.NoAI, prey.Persistence = true, true
		mobs = append(mobs, frog, prey)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		mobs.Tick(w, nil, nil)
	}
}

// BenchmarkFrogBreeding ticks courting frogs beside water and follows laying activity.
func BenchmarkFrogBreeding(b *testing.B) {
	w := newTestWorld()
	floor(w, -8, -8, 8, 8, 63, defaultState(b, "minecraft:stone"))
	w.set(2, 63, 0, defaultState(b, "minecraft:water"))
	frogs := []*Mob{NewMob(TypeFrog, 1, 0.5, 64, 0.5, 1), NewMob(TypeFrog, 2, 1.5, 64, 0.5, 2)}
	var events []MobEvent
	b.ReportAllocs()
	b.ResetTimer()
	for step := range b.N {
		for _, m := range frogs {
			if step%120 == 0 {
				m.BabyAge, m.InLove = 0, 600
				m.X, m.Y, m.Z = 0.5+float64(m.ID-1), 64, 0.5
			}
			events = m.tickInto(events[:0], &tickView{}, w, nil, frogs)
		}
	}
}

// BenchmarkFrogsSwimming steps sixteen frogs in a 9-deep pool sunk in a
// stone plain, put back on its floor with full air every 200 ticks so the
// swim and the land search keep running and none drowns.
func BenchmarkFrogsSwimming(b *testing.B) {
	w := newTestWorld()
	stone, water := defaultState(b, "minecraft:stone"), defaultState(b, "minecraft:water")
	floor(w, -9, -9, 9, 9, 0, stone)
	for y := int32(1); y <= 9; y++ {
		floor(w, -9, -9, 9, 9, y, stone)
		if y == 9 {
			floor(w, -40, -40, 40, 40, y, stone)
		}
		floor(w, -8, -8, 8, 8, y, water)
	}
	mobs := make(Mobs, 0, 16)
	for i := range 16 {
		frog := NewMob(TypeFrog, int32(i+1), float64(i%4)*2-3, 1, float64(i/4)*2-3, int64(i+1))
		frog.Persistence = true
		mobs = append(mobs, frog)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for step := range b.N {
		if step%200 == 0 {
			for i, m := range mobs {
				m.X, m.Y, m.Z = float64(i%4)*2-3, 1, float64(i/4)*2-3
				m.VX, m.VY, m.VZ = 0, 0, 0
				m.AirSupply = rules.MaxAirSupply
			}
		}
		mobs.Tick(w, nil, nil)
	}
}
