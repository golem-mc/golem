package world

import "testing"

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
