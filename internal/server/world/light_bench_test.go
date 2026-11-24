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

// BenchmarkLightQueueLava measures the queue a lone lava light grows and the
// array it retains.
func BenchmarkLightQueueLava(b *testing.B) {
	c := newChunk(0, 0)
	c.SetBlock(8, 64, 8, registry.BlockStates["minecraft:lava"])
	ctx := lightCtx{alone: c}
	var q *lightQueue
	b.ReportAllocs()
	for b.Loop() {
		c.blockLight = [lightSections]*[lightArrayLen]byte{}
		q = &lightQueue{}
		c.lightSet(blockLayer, 8, 64, 8, 15)
		q.push(8, 64, 8, 15, blockLayer|allDirs)
		ctx.runIncreases(q)
		if got := c.lightGet(&c.blockLight, 8, 78, 8); got != 1 {
			b.Fatalf("light at (8,78,8) = %d, want 1", got)
		}
	}
	b.ReportMetric(float64(cap(q.entries)), "cap")
}

// BenchmarkLightDecrease measures clearing a lone level-15 block light and
// draining the queues, with the light arrays restored each iteration.
func BenchmarkLightDecrease(b *testing.B) {
	c := newChunk(0, 0)
	ctx := lightCtx{alone: c}
	dq, iq := &lightQueue{}, &lightQueue{}
	c.lightSet(blockLayer, 8, 64, 8, 15)
	iq.push(8, 64, 8, 15, blockLayer|allDirs)
	ctx.runIncreases(iq)
	var saved [lightSections][lightArrayLen]byte
	for si, s := range c.blockLight {
		if s != nil {
			saved[si] = *s
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		for si, s := range c.blockLight {
			if s != nil {
				*s = saved[si]
			}
		}
		dq.entries, dq.head = dq.entries[:0], 0
		iq.entries, iq.head = iq.entries[:0], 0
		c.lightSet(blockLayer, 8, 64, 8, 0)
		dq.push(8, 64, 8, 15, blockLayer|allDirs)
		ctx.runDecreases(dq, iq)
		ctx.runIncreases(iq)
		if got := c.lightGet(&c.blockLight, 8, 78, 8); got != 0 {
			b.Fatalf("light at (8,78,8) = %d, want 0", got)
		}
	}
}
