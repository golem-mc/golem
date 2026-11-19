package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"testing"
)

// BenchmarkMobAmbientSound selects and encodes a mob's idle voice.
func BenchmarkMobAmbientSound(b *testing.B) {
	for _, kind := range []entity.MobType{entity.TypeCow, entity.TypeWolf, entity.TypeCat, entity.TypeGuardian, entity.TypeNautilus, entity.TypeSlime} {
		b.Run(kind.Name(), func(b *testing.B) {
			m := entity.NewMob(kind, 1, 1.25, 64, -2.5, 17)
			b.ReportAllocs()
			for b.Loop() {
				event, volume, pitch := m.AmbientVoice()
				mobVoiceIDSend(nil, m, event, volume, pitch)
			}
		})
	}
}
