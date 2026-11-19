package entity

import (
	"github.com/golem-mc/golem/nbt"
	"testing"
)

func BenchmarkMobCodec(b *testing.B) {
	now := func() int64 { return 1000 }
	for _, kind := range []MobType{TypePig, TypeHorse, TypeZombieVillager} {
		b.Run(kind.Name(), func(b *testing.B) {
			m := NewMob(kind, 1, 0.5, 64, 0.5, 7)
			tag := nbt.Compound{}
			m.SaveTo(tag, now)
			b.Run("save", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					out := make(nbt.Compound, 40)
					m.SaveTo(out, now)
				}
			})
			b.Run("load", func(b *testing.B) {
				b.ReportAllocs()
				for range b.N {
					m.LoadFrom(tag, nil, now)
				}
			})
		})
	}
}
