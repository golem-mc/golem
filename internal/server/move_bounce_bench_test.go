package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkOwnMovement measures an accepted own step through move and
// trackMovement: walking on the ground, and a bounce landing cycle on slime.
func BenchmarkOwnMovement(b *testing.B) {
	cases := []struct {
		name  string
		block string
		steps []player.Position
	}{
		{"walk", "minecraft:stone", []player.Position{
			{X: 0.5, Y: 64, Z: 0.5, OnGround: true}, {X: 0.7, Y: 64, Z: 0.5, OnGround: true}}},
		{"bounce", "minecraft:slime_block", []player.Position{
			{X: 0.5, Y: 65.5, Z: 0.5}, {X: 0.5, Y: 64.890, Z: 0.5}, {X: 0.5, Y: 64, Z: 0.5, OnGround: true},
			{X: 0.5, Y: 65.091, Z: 0.5}, {X: 0.5, Y: 65.4, Z: 0.5}}},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			s, clients := newIdlePlayersBenchServer(b, 1)
			c := clients[0]
			c.player.UpdateVitals(func(v *player.Vitals) { v.GameMode = protocol.GameModeSurvival })
			s.SetBlock(0, 62, 0, registry.MustState("minecraft:stone", nil))
			s.SetBlock(0, 63, 0, registry.MustState(tc.block, nil))
			c.player.SetPosition(tc.steps[0])
			b.ReportAllocs()
			b.ResetTimer()
			for i := range b.N {
				c.move(tc.steps[i%len(tc.steps)], true)
			}
		})
	}
}
