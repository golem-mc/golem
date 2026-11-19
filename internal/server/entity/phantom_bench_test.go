package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// BenchmarkPhantomScan measures the target scan over visible, invisible and allied players.
func BenchmarkPhantomScan(b *testing.B) {
	for _, scene := range []string{"visible", "invisible", "allied"} {
		b.Run(scene, func(b *testing.B) {
			m := NewMob(TypePhantom, 1, 0.5, 84, 0.5, 1)
			m.Team = "phantoms"
			players := make([]Player, 16)
			for i := range players {
				players[i] = Player{EntityID: int32(i + 2), X: float64(i) - 7.5, Y: 64, Z: 0.5, Huntable: true,
					Invisible: scene == "invisible"}
				if scene == "allied" {
					players[i].Team = m.Team
				}
			}
			tick := &tickView{w: newTestWorld(), players: players}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				m.phantomScan(tick)
			}
		})
	}
}

// BenchmarkPhantomAttackDamage measures the bite's attribute lookup.
func BenchmarkPhantomAttackDamage(b *testing.B) {
	for _, mapped := range []bool{false, true} {
		name := "direct"
		if mapped {
			name = "attributes"
		}
		b.Run(name, func(b *testing.B) {
			m := NewMob(TypePhantom, 1, 0.5, 84, 0.5, 1)
			if mapped {
				m.SetAttributes(m.BuildAttributes(nbt.List{}))
			}
			b.ReportAllocs()
			for b.Loop() {
				m.attackDamage()
			}
		})
	}
}
