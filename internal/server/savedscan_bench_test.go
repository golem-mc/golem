package server

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/world"
)

func BenchmarkSavedEntityScans(b *testing.B) {
	for _, operation := range []string{"uuids", "tags", "unload"} {
		b.Run(operation, func(b *testing.B) {
			s, _ := newCommandBenchServer(b, 100)
			for range 100 {
				s.RunRconLine(fmt.Sprintf("summon snowball 4 %d 4 {NoGravity:1b}", world.GroundLevel))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				switch operation {
				case "uuids":
					if len(s.liveEntityUUIDs()) != 200 {
						b.Fatal("lost entity identity")
					}
				case "tags":
					if len(s.liveEntityTags()) != 1 {
						b.Fatal("lost entity world")
					}
				case "unload":
					if len(s.sweepDepartingEntities()) != 0 {
						b.Fatal("held entity departed")
					}
				}
			}
		})
	}
}
