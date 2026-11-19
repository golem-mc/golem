package server

import (
	"github.com/golem-mc/golem/internal/server/entity"
	"testing"
)

// BenchmarkMobDamageSound selects and encodes a mob's hurt voice.
func BenchmarkMobDamageSound(b *testing.B) {
	for _, kind := range []entity.MobType{entity.TypeCow, entity.TypeWolf, entity.TypeGuardian, entity.TypeMooshroom} {
		b.Run(kind.Name(), func(b *testing.B) {
			m := entity.NewMob(kind, 1, 1.25, 64, -2.5, 17)
			b.ReportAllocs()
			for b.Loop() {
				mobVoiceSend(nil, m, mobSound(m, true), m.SoundVolume(), m.VoicePitch())
			}
		})
	}
}
