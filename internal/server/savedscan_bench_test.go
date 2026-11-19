package server

import "testing"

func BenchmarkSavedEntityScans(b *testing.B) {
	for _, operation := range []string{"uuids", "tags", "unload"} {
		b.Run(operation, func(b *testing.B) {
			s, _ := newCommandBenchServer(b, 100)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				switch operation {
				case "uuids":
					if len(s.liveEntityUUIDs()) != 100 {
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
