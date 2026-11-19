package server

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkConsumableSound measures encoding each bite of an already selected meal.
func BenchmarkConsumableSound(b *testing.B) {
	for _, item := range []registry.Item{registry.ItemBread, registry.ItemHoneyBottle} {
		b.Run(item.Name(), func(b *testing.B) {
			s := &Server{soundRng: rand.New(rand.NewSource(1))}
			use, ok := registry.ConsumableOf(int32(item))
			if !ok {
				b.Fatal("missing consumable")
			}
			b.ReportAllocs()
			for b.Loop() {
				s.eatBiteSoundAt(use, 1.5, 64, -2.5)
			}
		})
	}
}
