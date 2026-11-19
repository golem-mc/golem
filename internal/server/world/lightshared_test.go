package world

import (
	"bytes"
	"testing"
)

func TestSharedSkyLightSections(t *testing.T) {
	t.Parallel()
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	a, b := newChunk(0, 0), newChunk(1, 0)
	for _, c := range []*Chunk{a, b} {
		NewFlatGenerator().Generate(c)
		w.lightAlone(c)
	}
	for si := lightSectionOf(GroundLevel + 16); si < lightSections; si++ {
		for _, c := range []*Chunk{a, b} {
			if c.skyLight[si] != &fullSection || c.lightBorrowed[skyLayer]&(1<<si) == 0 {
				t.Fatalf("section %d does not borrow the shared full array", si)
			}
		}
	}
	want := b.encodeLight()
	a.lightSet(skyLayer, 1, 200, 2, 7)
	if got := a.lightGet(&a.skyLight, 1, 200, 2); got != 7 {
		t.Fatalf("private write = %d", got)
	}
	if a.lightBorrowed[skyLayer]&(1<<lightSectionOf(200)) != 0 {
		t.Fatal("written section remains borrowed")
	}
	if !bytes.Equal(want, b.encodeLight()) || fullSection[0] != 0xff {
		t.Fatal("write changed another chunk's light")
	}
	if !bytes.Equal(b.encodeChunkLight(), lightFormReference(b, 0, lightSections, true, true, false)) {
		t.Fatal("shared chunk light differs from reference")
	}
}

func TestDecodedUniformLightSections(t *testing.T) {
	t.Parallel()
	for _, layer := range []int{skyLayer, blockLayer} {
		c := newChunk(0, 0)
		var arrays [2][lightSections]*[lightArrayLen]byte
		arrays[layer][1], arrays[layer][2] = &darkSection, &fullSection
		masks := lightMasks{}
		if layer == skyLayer {
			masks.sky = 6
		} else {
			masks.block = 6
		}
		encoded := masks.encode(arrays[0][:], arrays[1][:])
		if !c.decodeLight(encoded) {
			t.Fatal("decode failed")
		}
		if c.lightLayer(layer)[1] != &darkSection || c.lightLayer(layer)[2] != &fullSection || c.lightBorrowed[layer]&6 != 6 {
			t.Fatal("decoded uniform sections are not borrowed")
		}
		if layer == skyLayer && c.skyLightBelow(0, worldMinY-1, 0, lightSections-1) != 0 {
			t.Fatal("stored dark sky section was treated as absent")
		}
		for si, want := range map[int]uint8{1: 0, 2: 15} {
			y := worldMinY + int32(si-1)*16
			c.lightSet(layer, 1, y, 0, 8)
			if c.lightLayer(layer)[si] == arrays[layer][si] || c.lightBorrowed[layer]&(1<<si) != 0 {
				t.Fatal("write did not copy borrowed section")
			}
			if got := c.lightGet(c.lightLayer(layer), 0, y, 0); got != want {
				t.Fatalf("adjacent nibble = %d, want %d", got, want)
			}
		}
	}
}

func TestSharedLightOwnershipAfterJoinAndRelight(t *testing.T) {
	t.Parallel()
	w, err := NewWorld(t.TempDir(), NewFlatGenerator())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	w.Acquire(ChunkPos{0, 0})
	w.Acquire(ChunkPos{1, 0})
	check := func() {
		t.Helper()
		for _, l := range w.chunks {
			for layer := range 2 {
				for si, a := range l.chunk.lightLayer(layer) {
					if (a == &fullSection || a == &darkSection) && l.chunk.lightBorrowed[layer]&(1<<si) == 0 {
						t.Fatalf("shared layer %d section %d lost borrowed mark", layer, si)
					}
				}
			}
		}
	}
	check()
	w.SetBlock(8, 200, 8, stoneState)
	w.Relight(8, 8)
	check()
	w.SetBlock(8, 200, 8, airState)
	// Force the whole-column fallback to replace the roof's private section
	// with shared sky, rather than incrementally updating its nibbles.
	w.chunks[ChunkPos{0, 0}].chunk.lightCells = nil
	w.Relight(8, 8)
	check()
}
