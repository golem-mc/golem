package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

var mobEventPacketSink []byte

// BenchmarkMobEventSound encodes a mob action through the tick-effect collector.
func BenchmarkMobEventSound(b *testing.B) {
	m := entity.NewMob(entity.TypeWarden, 1, 1.5, 64, 2.5, 17)
	voice := entity.MobEvent{Mob: m, Kind: entity.MobSounded, SoundID: registry.SoundEntityWardenDig, Volume: 1, Pitch: 1}
	unmuted := voice
	unmuted.Unmuted, unmuted.Centred = true, true
	for _, c := range []struct {
		name string
		ev   entity.MobEvent
	}{{"voice", voice}, {"unmuted_centred", unmuted}} {
		b.Run(c.name, func(b *testing.B) {
			var fx mobEffects
			tick := mobDimTick{}
			b.ReportAllocs()
			for b.Loop() {
				tick.sounded(&fx, c.ev)
				mobEventPacketSink = fx.sends[0].data
				fx.sends = fx.sends[:0]
			}
		})
	}
}
