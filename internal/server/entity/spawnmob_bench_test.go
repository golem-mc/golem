package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkSpawnMob(b *testing.B) {
	for _, tc := range []struct {
		name string
		kind MobType
	}{{"zombie", TypeZombie}, {"cow", TypeCow}, {"skeleton", TypeSkeleton}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				m := NewMob(tc.kind, int32(i), .5, 66, .5, int64(i))
				m.FinalizeSpawn(GoDice{Rand: m.Rand}, registry.BiomePlains, nil, rules.LocalDifficulty{})
			}
		})
	}
}
