package entity

import (
	"testing"

	"github.com/google/uuid"
)

func BenchmarkRiddenStepSounds(b *testing.B) {
	for _, kind := range []MobType{TypeHorse, TypePig, TypeCamel, TypeStrider, TypeNautilus, TypeHappyGhast} {
		b.Run(kind.Name(), func(b *testing.B) {
			w := newTestWorld()
			floor(w, -2, -2, 2, 2, 63, defaultState(b, "minecraft:stone"))
			m := NewMob(kind, 1, 0.25, 64, 0.5, 1)
			m.OnGround = true
			m.Board(uuid.New())
			b.ReportAllocs()
			for b.Loop() {
				from := m.X
				m.X = 1 - m.X
				m.RiddenStepSounds(w, from, m.Y, m.Z)
			}
		})
	}
}
