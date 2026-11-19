package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
)

// BenchmarkEquipmentSound measures feedback selection and encoding after equipping.
func BenchmarkEquipmentSound(b *testing.B) {
	for _, kind := range []entity.MobType{entity.TypeZombie, entity.TypeNautilus} {
		b.Run(kind.Name(), func(b *testing.B) {
			s := &Server{}
			m := entity.NewMob(kind, 1, 0, 0, 0, 1)
			item, _ := registry.ItemSaddle.Equippable()
			b.ReportAllocs()
			for b.Loop() {
				s.equipFeedback(nil, item, equipFx{x: 1.5, y: 64, z: -2.5,
					category: protocol.SoundCategoryNeutral, sound: m.EquipSoundOverride("saddle")})
			}
		})
	}
}
