package entity

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// BenchmarkMobBuildAttributes measures the throwaway pairing map a mob
// without an installed map gets for each spawn packet.
func BenchmarkMobBuildAttributes(b *testing.B) {
	for _, tc := range []struct {
		name  string
		kind  MobType
		type_ int32
	}{{"rabbit", TypeRabbit, 0}, {"killer bunny", TypeRabbit, 99}, {"pig", TypePig, 0}, {"zombie", TypeZombie, 0}} {
		b.Run(tc.name, func(b *testing.B) {
			m := NewMob(tc.kind, 1, .5, 66, .5, 1)
			m.RabbitType = tc.type_
			b.ReportAllocs()
			for b.Loop() {
				m.BuildAttributes(nbt.List{})
			}
		})
	}
}
