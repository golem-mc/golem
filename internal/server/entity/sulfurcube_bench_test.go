package entity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkSulfurCubeSplits(b *testing.B) {
	for _, tc := range []struct {
		name   string
		primed bool
	}{
		{"unprimed", false},
		{"primed", true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			m := NewMob(TypeSulfurCube, 1, 0.5, 64, 0.5, 1)
			m.AbsorbSulfurBlock(protocol.ItemStack{ItemID: int32(registry.ItemTnt), Count: 1})
			if tc.primed && !m.PrimeSulfur(false) {
				b.Fatal("cube did not prime")
			}
			b.ReportAllocs()
			for b.Loop() {
				m.Splits()
			}
		})
	}
}
