package rules

import "testing"

func BenchmarkSectionsToVillage(b *testing.B) {
	for _, bc := range []struct {
		name     string
		occupied int
	}{{"no_village", 0}, {"far_village", 40}} {
		b.Run(bc.name, func(b *testing.B) {
			v := NewVillageIndex(nil)
			v.occupied = map[[3]int]struct{}{}
			for i := range bc.occupied {
				v.occupied[[3]int{100 + i%8, 4 + i%2, 100 + i/8}] = struct{}{}
			}
			b.ReportAllocs()
			for b.Loop() {
				if v.SectionsToVillage(8, 64, 8, 6) != 7 {
					b.Fatal("found a village")
				}
			}
		})
	}
}
