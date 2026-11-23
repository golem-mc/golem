package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkCoolingHopperListener(b *testing.B) {
	run := func(b *testing.B, listen bool) {
		s, w := newBlockBenchServer(b, 0)
		fixture := &hopperListenerBenchPlugin{}
		if listen {
			s.loadPlugins([]plugin.Registration{{Name: "bench", Plugin: fixture}})
			s.enablePlugins()
		}
		if s.hopperListening() != listen {
			b.Fatal("unexpected hopper listener state")
		}
		const y = world.GroundLevel + 3
		state := registry.MustState("minecraft:hopper", map[string]string{"facing": "down", "enabled": "true"})
		w.SetBlock(0, y, 0, state)
		be := w.EnsureBlockEntity(0, y, 0, world.KindHopper)

		b.ReportAllocs()
		for b.Loop() {
			be.Cooldown = 8
			s.tickHopper(w, be, 101)
		}
		if be.Cooldown != 7 || be.TickedGameTime != 101 || fixture.calls != 0 {
			b.Fatalf("cooldown=%d, ticked game time=%d, listener calls=%d; want 7, 101, 0", be.Cooldown, be.TickedGameTime, fixture.calls)
		}
	}
	b.Run("plain", func(b *testing.B) { run(b, false) })
	b.Run("listener", func(b *testing.B) { run(b, true) })
}

type hopperListenerBenchPlugin struct{ calls int }

func (p *hopperListenerBenchPlugin) Enable(s plugin.Server) error {
	plugin.Listen(s, func(*plugin.InventoryMoveItemEvent) { p.calls++ })
	return nil
}

func (*hopperListenerBenchPlugin) Disable() {}
