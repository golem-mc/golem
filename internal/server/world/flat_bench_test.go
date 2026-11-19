package world

import (
	"io"
	"log/slog"
	"testing"
)

// BenchmarkFlatGenerate generates a 7x7 spread of chunks of the default flat
// world, with a fresh generator each round, through the constructor a server
// calls.
func BenchmarkFlatGenerate(b *testing.B) {
	// "{}" has no layers, so each constructor warns it falls back to the
	// default preset; benchmarks run after the tests, so nothing else logs.
	defer slog.SetDefault(slog.Default())
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	dir := b.TempDir()
	b.ReportAllocs()
	for b.Loop() {
		g := NewGenerator(dir, "minecraft:flat", "0", "{}")
		for x := int32(-3); x <= 3; x++ {
			for z := int32(-3); z <= 3; z++ {
				g.Generate(newChunk(x, z))
			}
		}
	}
}
