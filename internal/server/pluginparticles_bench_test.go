package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// BenchmarkPluginPlayerParticles covers the shared option conversion and packet enqueue.
func BenchmarkPluginPlayerParticles(b *testing.B) {
	for _, tc := range []struct {
		name     string
		particle plugin.Particle
	}{
		{"flame", plugin.Particle{Type: protocol.ParticleFlame}},
		{"dust", plugin.Particle{Type: protocol.ParticleDust, Color: 0x123456, Size: 2}},
		{"item", plugin.Particle{Type: protocol.ParticleItem, Item: new(plugin.NewStack(registry.ItemDiamond, 2))}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			s := &Server{clients: make(map[uuid.UUID]*client)}
			c := &client{srv: s, out: &mailbox{}}
			c.profile.UUID = uuid.New()
			c.out.released.Store(true)
			s.clients[c.profile.UUID] = c
			s.deferWakes.Store(true)
			p := pluginPlayer{c: c}
			b.ReportAllocs()
			for b.Loop() {
				p.SpawnParticles(tc.particle, plugin.Vec3{X: 1, Y: 65, Z: 2}, 5, plugin.Vec3{X: .1, Y: .2, Z: .3}, .5)
				if len(c.out.queue) != 1 || c.out.queue[0].id != protocol.IDPlayLevelParticles {
					b.Fatal("particle packet missing")
				}
				clear(c.out.queue)
				c.out.queue = c.out.queue[:0]
				c.out.queued = 0
			}
		})
	}
}
