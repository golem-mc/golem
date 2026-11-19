package world

import (
	"fmt"
	"github.com/golem-mc/golem/registry"
	"testing"
)

var spawnCollisionResult int

func BenchmarkSpawnCollision(b *testing.B) {
	// Initialize collision tables before timing, including the first lazy lookup.
	CollisionTop(registry.BlockStone.DefaultState())
	for _, layout := range []string{"4", "5", "6", "7", "8", "direct", "uniform-air", "uniform-stone", "repeated-word"} {
		for _, cold := range []bool{true, false} {
			mode := "warm"
			if cold {
				mode = "cold"
			}
			b.Run(fmt.Sprintf("%s/%s", mode, layout), func(b *testing.B) {
				w := newTestWorld(b, NewFlatGenerator())
				c := newChunk(0, 0)
				s := SpawnChunk{World: w, origin: &loadedChunk{chunk: c}}
				sec := &c.sections[(80-worldMinY)/16]
				switch layout {
				case "uniform-air":
					sec.setAll(airState)
				case "uniform-stone":
					sec.setAll(stoneState)
				default:
					n := map[string]int{"4": 16, "5": 32, "6": 64, "7": 128, "8": 256, "direct": 300, "repeated-word": 16}[layout]
					states := []uint16{airState, stoneState}
					for state := uint16(0); len(states) < n; state++ {
						if state != airState && state != stoneState {
							states = append(states, state)
						}
					}
					for i := range blocksPerSection {
						j := i
						if layout == "repeated-word" {
							j = i / 32
						}
						sec.set(i, states[j%n], false)
					}
				}
				var queries [128][3]int32
				for j := range queries {
					i := (j*997 + 31) & 4095
					queries[j] = [3]int32{int32(i & 15), 80 + int32(i>>8), int32(i >> 4 & 15)}
				}
				for _, q := range queries {
					s.InitialSpawnBlocked(q[0], q[1], q[2])
				}
				b.ReportAllocs()
				b.ResetTimer()
				result := 0
				for b.Loop() {
					if cold {
						sec.spawn.Store(nil)
					}
					for _, q := range queries {
						if s.InitialSpawnBlocked(q[0], q[1], q[2]) {
							result++
						}
					}
				}
				spawnCollisionResult = result
			})
		}
	}
}
