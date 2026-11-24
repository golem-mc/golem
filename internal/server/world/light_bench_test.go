package world

import (
	"testing"

	"github.com/golem-mc/golem/registry"
)

// BenchmarkSkyLightBelow measures missing-section searches and a stored-cell read.
func BenchmarkSkyLightBelow(b *testing.B) {
	var c Chunk
	c.lightSet(skyLayer, 3, 48, 5, 2)
	c.lightSet(skyLayer, 3, 60, 5, 11)
	for _, tc := range []struct {
		name string
		y    int32
	}{
		{"MissingSection", -1},
		{"BelowStorage", -100},
		{"StoredSection", 60},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				c.skyLightBelow(3, tc.y, 5, lightSectionOf(256))
			}
		})
	}
}

// BenchmarkFlatLightAlone measures lightAlone on a chunk of the black-box
// tests' flat world: stone to y=63, open sky above.
func BenchmarkFlatLightAlone(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	defer w.Close()
	c := newChunk(0, 0)
	NewFlatGenerator().Generate(c)
	b.ReportAllocs()
	for b.Loop() {
		w.lightAlone(c)
	}
}

func BenchmarkScanEmitters(b *testing.B) {
	for _, tc := range []struct {
		name      string
		glowstone bool
	}{
		{"Empty", false},
		{"Glowstone", true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			c := newChunk(0, 0)
			NewFlatGenerator().Generate(c)
			if tc.glowstone {
				c.SetBlock(8, 66, 8, registry.BlockStates["minecraft:glowstone"])
				c.lightSet(blockLayer, 8, 66, 8, 15)
			}
			ctx := lightCtx{alone: c}
			q := lightQueue{entries: make([]lightEntry, 0, 1)}
			b.ReportAllocs()
			for b.Loop() {
				if tc.glowstone {
					c.lightSet(blockLayer, 8, 66, 8, 0)
				}
				ctx.scanEmitters(0, 0, 15, 15, &q)
				q.entries = q.entries[:0]
			}
		})
	}
}

// BenchmarkFlatJoinArea measures what a test's join costs the world: a 9x9
// of flat chunks acquired, which lights and joins them, then released and
// saved.
func BenchmarkFlatJoinArea(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	defer w.Close()
	b.ReportAllocs()
	off := int32(0)
	for b.Loop() {
		for x := off - 4; x <= off+4; x++ {
			for z := int32(-4); z <= 4; z++ {
				w.Acquire(ChunkPos{X: x, Z: z})
			}
		}
		for x := off - 4; x <= off+4; x++ {
			for z := int32(-4); z <= 4; z++ {
				w.Release(ChunkPos{X: x, Z: z})
			}
		}
		w.FlushSaves()
		// Far enough over that the next area shares no chunk with this one.
		off += 20
	}
}

// BenchmarkStaleChunkLightForm measures the first Chunk Data light form
// after a relight on a retained flat chunk: one buffer is built, not both.
func BenchmarkStaleChunkLightForm(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	defer w.Close()
	c := newChunk(0, 0)
	NewFlatGenerator().Generate(c)
	w.lightAlone(c)
	c.lightFormsRetained = true
	b.ReportAllocs()
	for b.Loop() {
		c.invalidateLightForms()
		if len(c.chunkLightForm()) == 0 {
			b.Fatal("no light form")
		}
	}
}
