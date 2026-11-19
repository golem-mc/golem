package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkAttackRangeWith(b *testing.B) {
	sword := protocol.ItemStack{ItemID: int32(registry.ItemDiamondSword), Count: 1}
	patched := sword
	var value protocol.Buffer
	for _, f := range []float32{0, 9, 0, 9, 0.3, 1} {
		value.PutF32(f)
	}
	patched.SetRaw(protocol.RawComponent{Type: protocol.DataComponentAttackRange, Data: value.Bytes()})
	for _, tc := range []struct {
		name string
		held protocol.ItemStack
	}{
		{"default", sword},
		{"override", patched},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				AttackRangeWith(tc.held, 3)
			}
		})
	}
}
