package registry

import (
	"strconv"
	"testing"
)

// BenchmarkBedOccupiedTransition compares the state work around a bed's write.
func BenchmarkBedOccupiedTransition(b *testing.B) {
	b.Run("parsed", func(b *testing.B) {
		s := BlockRedBed.DefaultState()
		b.ReportAllocs()
		for i := range b.N {
			name, raw, _ := BlockStateName(s)
			props := ParseStateProps(raw)
			props["occupied"] = strconv.FormatBool(i&1 != 0)
			s, _ = FindState(name, props)
		}
		if BlockOf(s) != BlockRedBed {
			b.Fatal("lost bed")
		}
	})
	b.Run("typed", func(b *testing.B) {
		s := BlockRedBed.DefaultState()
		b.ReportAllocs()
		for i := range b.N {
			s, _ = WithBool(s, PropOccupied, i&1 != 0)
		}
		if BlockOf(s) != BlockRedBed {
			b.Fatal("lost bed")
		}
	})
}
