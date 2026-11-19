package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkRefreshPose(b *testing.B) {
	for _, mode := range []string{"standing", "passenger", "spectator"} {
		b.Run(mode, func(b *testing.B) {
			s, w := newCommandBenchServer(b, 0)
			p := &player.Player{EntityID: s.nextEntity()}
			p.SetPosition(player.Position{X: 5.5, Y: float64(world.GroundLevel), Z: 5.5})
			c := &client{srv: s, player: p}
			c.dim.Store(s.allDims()[0])
			if mode != "standing" {
				// Swimming fits beneath the ceiling, but standing and crouching do not.
				p.SetPosition(player.Position{X: 5.5, Y: float64(world.GroundLevel) + 0.84375, Z: 5.5})
				w.SetBlock(5, world.GroundLevel+2, 5, registry.MustState("minecraft:stone", nil))
			}
			switch mode {
			case "passenger":
				c.mount.Store(&mobEntity{})
			case "spectator":
				p.UpdateVitals(func(v *player.Vitals) { v.GameMode = protocol.GameModeSpectator })
			}
			look := player.Appearance{}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				c.refreshPose(&look)
			}
		})
	}
}
