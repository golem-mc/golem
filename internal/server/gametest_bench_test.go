package server

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/datapack"
)

// BenchmarkGameTestTickWaiting measures the active runner between results.
func BenchmarkGameTestTickWaiting(b *testing.B) {
	s := &Server{}
	t := &gameTest{started: true, test: datapack.TestInstance{MaxTicks: math.MaxInt32}}
	s.gameTests.run = []*gameTest{t}
	b.ReportAllocs()
	for b.Loop() {
		t.tick = 0
		s.tickGameTests()
	}
}
