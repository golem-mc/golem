package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkTickSleeping exercises both straw bed halves for each sleeper.
func BenchmarkTickSleeping(b *testing.B) {
	s, clients := newIdlePlayersBenchServer(b, 8)
	s.world.SetClockTime(world.OverworldClock, 13000)
	s.world.SetIntRule(world.RulePlayersSleeping, 101) // Keep the sleepers in bed.
	head := registry.MustState("minecraft:straw_bed", map[string]string{"part": "head", "facing": "south", "occupied": "true"})
	foot, _ := registry.WithProp(head, registry.PropPart, "foot")
	for i, c := range clients {
		x := int32(i)
		if !s.world.SetBlock(x, 64, 1, head) || !s.world.SetBlock(x, 64, 0, foot) {
			b.Fatal("could not place the straw bed")
		}
		c.player.SetSleep(player.Sleep{Lying: true, X: x, Y: 64, Z: 1})
	}
	s.recountSleepers(s.dimensionOfWorld(s.world))
	b.ReportAllocs()
	for b.Loop() {
		s.tickSleeping()
	}
	for _, c := range clients {
		if sl := c.player.Sleep(); !sl.Lying || sl.Ticks == 0 {
			b.Fatal("the sleeper did not tick in bed")
		}
	}
}
