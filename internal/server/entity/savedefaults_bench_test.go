package entity

import "testing"

func BenchmarkEntitySaveDefaultsTick(b *testing.B) {
	w := newTestWorld()
	floor(w, -2, -2, 2, 2, 63, defaultState(b, "minecraft:stone"))
	b.Run("vex", func(b *testing.B) {
		m := NewMob(TypeVex, 1, 0.5, 64, 0.5, 7)
		m.NoAI, m.Persistence = true, true
		b.ReportAllocs()
		for b.Loop() {
			m.Tick(w, nil)
		}
	})
	b.Run("mannequin", func(b *testing.B) {
		m := NewMannequin(1, 0.5, 64, 0.5)
		b.ReportAllocs()
		for b.Loop() {
			m.Step(w)
		}
	})
}
