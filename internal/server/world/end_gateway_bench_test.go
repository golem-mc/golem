package world

import "testing"

// BenchmarkEndGenerateGatewayReturn uses a seed-0 highlands chunk containing a
// return gateway at (-426, 70, 1079), including its carried block entity.
func BenchmarkEndGenerateGatewayReturn(b *testing.B) {
	g := NewEndGenerator(0)
	b.ReportAllocs()
	for b.Loop() {
		c := newChunk(-27, 67)
		g.Generate(c)
	}
}
