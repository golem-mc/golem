package server

import (
	"fmt"
	"testing"
)

// BenchmarkUnlistNewestFixture removes the just-appended fixture from a list
// of n unrelated ones.
func BenchmarkUnlistNewestFixture(b *testing.B) {
	for _, n := range []int{1, 2000} {
		b.Run(fmt.Sprintf("fixtures=%d", n), func(b *testing.B) {
			s := &Server{}
			for range n {
				s.fixtures = append(s.fixtures, &fixtureEntity{})
			}
			ent := &fixtureEntity{}
			for b.Loop() {
				s.fixtures = append(s.fixtures, ent)
				s.unlistFixtureLocked(ent)
			}
		})
	}
}
