package entity

import "testing"

// BenchmarkDragonHoldingTick measures flight, contact selection and wall reads.
func BenchmarkDragonHoldingTick(b *testing.B) {
	w := newTestWorld()
	m := NewMob(TypeEnderDragon, 1, 0, 128, 0, 1)
	m.SetDragonPhase(DragonHolding, 0)
	players := []Player{{EntityID: 20, X: 8, Y: 128}}
	others := []*Mob{NewMob(TypeCow, 30, 8, 128, 0, 1)}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		m.tick(w, players, others)
	}
}
