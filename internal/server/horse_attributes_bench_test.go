package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
)

func BenchmarkHorseAttributesPairing(b *testing.B) {
	for _, kind := range []entity.MobType{entity.TypeHorse, entity.TypeZombieHorse} {
		name, _ := entity.MobName(kind)
		b.Run(name, func(b *testing.B) {
			ent := &mobEntity{mob: entity.NewMob(kind, 1, 0.5, 64, 0.5, 1)}
			b.ReportAllocs()
			for b.Loop() {
				spawnPacketSink = mobAttributesPayload(ent)
			}
		})
	}
}
