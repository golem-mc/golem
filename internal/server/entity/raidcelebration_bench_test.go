package entity

import "testing"

// BenchmarkIllusionerRaidCelebration rolls the cheer and jump decisions of a lost raid.
func BenchmarkIllusionerRaidCelebration(b *testing.B) {
	m := NewMob(TypeIllusioner, 1, 0.5, 64, 0.5, 1)
	g := &raidCelebrationGoal{m: m}
	view := &tickView{}
	b.ReportAllocs()
	for b.Loop() {
		view.events = view.events[:0]
		g.Tick(view)
	}
}
