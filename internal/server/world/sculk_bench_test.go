package world

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/registry"
)

func BenchmarkVibrationNearSensors(b *testing.B) {
	for _, count := range []int32{0, 1, 40} {
		b.Run(fmt.Sprintf("sensors=%d", count), func(b *testing.B) {
			w := newTestWorld(b, NewFlatGenerator())
			for x := int32(-1); x <= 4; x++ {
				for z := int32(-1); z <= 4; z++ {
					w.Acquire(ChunkPos{X: x, Z: z})
				}
			}
			const y = GroundLevel + 1
			for i := range count {
				// Scatter the sensors across nearby sections outside their hearing range.
				x, z := 40+i%15, 40+(7*i+i/15)%15
				w.SetBlock(x, y, z, registry.BlockSculkSensor.DefaultState())
			}
			p := NewPhysics(w)
			v := Vibration{X: 30.5, Y: y, Z: 30.5, Event: registry.GameEventStep, Frequency: 1}
			b.ReportAllocs()
			for b.Loop() {
				p.Vibrate(v)
			}
		})
	}
}

func BenchmarkSculkCatalystsNear(b *testing.B) {
	w := newTestWorld(b, NewFlatGenerator())
	w.Acquire(ChunkPos{X: 2, Z: 2})
	const y = GroundLevel + 1
	for i := range int32(40) {
		w.SetBlock(40+i%8, y, 40+i/8, registry.BlockSculkCatalyst.DefaultState())
	}
	p := NewPhysics(w)
	b.ReportAllocs()
	for b.Loop() {
		p.SculkCatalystDeath(30.5, y, 30.5, nil)
	}
}
