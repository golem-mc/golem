package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// BenchmarkTickSurvivalShoulderParrots runs a hundred players' ticks with two
// perched parrots each, alone or beside a herd the imitation roll must scan.
func BenchmarkTickSurvivalShoulderParrots(b *testing.B) {
	for _, crowd := range []int{0, 2000} {
		name := "Alone"
		if crowd > 0 {
			name = "Herd"
		}
		b.Run(name, func(b *testing.B) {
			s, clients := newIdlePlayersBenchServer(b, idleBenchPlayers)
			for _, c := range clients {
				c.player.SetShoulders(player.Shoulders{
					Left:  nbt.Compound{"id": nbt.String("minecraft:parrot")},
					Right: nbt.Compound{"id": nbt.String("minecraft:parrot")},
				})
			}
			pos := clients[0].player.Position()
			for i := range crowd {
				s.spawnMobInDim(s.allDims()[0], entity.TypeCow, pos.X+8+float64(i%50), world.GroundLevel,
					pos.Z-25+float64(i/50), int64(i), nil)
			}
			for range 200 {
				s.tickSurvival()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				s.tickSurvival()
			}
		})
	}
}
