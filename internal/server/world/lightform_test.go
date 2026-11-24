package world

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// lightFormReference is the light encoding built array list by array list,
// against which the mask writer's bytes are checked: omitFull drops uniformly
// lit sky sections, span reads sections [offset, offset+sections) and tells
// a missing array by nil alone.
func lightFormReference(c *Chunk, offset, sections int, sky, omitFull, span bool) []byte {
	var skyMask, blockMask, emptySkyMask, emptyBlockMask uint64
	var skyArrays, blockArrays [][]byte
	for i := range sections {
		s, bl := c.skyLight[offset+i], c.blockLight[offset+i]
		switch {
		case span && (!sky || s == nil), !span && lightArrayEmpty(s):
			emptySkyMask |= 1 << i
		case omitFull && lightArrayFull(s):
		default:
			skyMask |= 1 << i
			skyArrays = append(skyArrays, s[:])
		}
		if span && bl == nil || !span && lightArrayEmpty(bl) {
			emptyBlockMask |= 1 << i
		} else {
			blockMask |= 1 << i
			blockArrays = append(blockArrays, bl[:])
		}
	}
	var b protocol.Buffer
	for _, mask := range []uint64{skyMask, blockMask, emptySkyMask, emptyBlockMask} {
		b.PutBitSet([]uint64{mask})
	}
	for _, arrays := range [][][]byte{skyArrays, blockArrays} {
		b.PutVarInt(int32(len(arrays)))
		for _, arr := range arrays {
			b.PutBytes(arr)
		}
	}
	return b.Bytes()
}

// TestLightFormsMatchReference checks every light encoding against the
// reference over sections that are missing, dark, full and mixed.
func TestLightFormsMatchReference(t *testing.T) {
	t.Parallel()
	c := newChunk(0, 0)
	for i := range lightSections {
		var sky, block *[lightArrayLen]byte
		switch i % 4 {
		case 1:
			sky, block = new([lightArrayLen]byte), new([lightArrayLen]byte)
		case 2:
			full := fullSection
			sky = &full
		case 3:
			sky, block = new([lightArrayLen]byte), new([lightArrayLen]byte)
			for j := range lightArrayLen {
				sky[j], block[j] = byte(i*j), byte(i+j)
			}
		}
		c.skyLight[i], c.blockLight[i] = sky, block
	}
	for _, omitFull := range []bool{false, true} {
		if got, want := c.encodeLightForm(omitFull, allLight), lightFormReference(c, 0, lightSections, true, omitFull, false); !bytes.Equal(got, want) {
			t.Errorf("encodeLightForm(%v) differs from the reference", omitFull)
		}
	}
	for _, sky := range []bool{false, true} {
		if got, want := c.encodeLightSpan(3, 18, sky, allLight), lightFormReference(c, 3, 18, sky, false, true); !bytes.Equal(got, want) {
			t.Errorf("encodeLightSpan(sky %v) differs from the reference", sky)
		}
	}
}

func TestDecodeLightRoundTrip(t *testing.T) {
	t.Parallel()
	c := newChunk(0, 0)
	c.skyLight[10], c.blockLight[3] = new([lightArrayLen]byte), new([lightArrayLen]byte)
	for j := range lightArrayLen {
		c.skyLight[10][j], c.blockLight[3][j] = byte(j*7+1), byte(j*3+2)
	}
	enc := c.encodeLight()
	d := newChunk(0, 0)
	if !d.decodeLight(enc) {
		t.Fatal("decodeLight rejected encodeLight's output")
	}
	for i := range lightSections {
		if (c.skyLight[i] == nil) != (d.skyLight[i] == nil) || (c.blockLight[i] == nil) != (d.blockLight[i] == nil) {
			t.Fatalf("section %d presence differs", i)
		}
		if c.skyLight[i] != nil && *c.skyLight[i] != *d.skyLight[i] {
			t.Errorf("sky section %d differs", i)
		}
		if c.blockLight[i] != nil && *c.blockLight[i] != *d.blockLight[i] {
			t.Errorf("block section %d differs", i)
		}
	}
	if !bytes.Equal(enc, d.encodeLight()) {
		t.Error("re-encoded light differs")
	}
}

// lightWithArrays is a chunk whose first 16 sky and four block sections are
// uniformly full and one of each is mixed.
func lightWithArrays() *Chunk {
	c := newChunk(0, 0)
	for i := range 16 {
		c.skyLight[i] = &fullSection
	}
	for i := range 4 {
		c.blockLight[i] = &fullSection
	}
	c.skyLight[20], c.blockLight[9] = new([lightArrayLen]byte), new([lightArrayLen]byte)
	for j := range lightArrayLen {
		c.skyLight[20][j], c.blockLight[9][j] = byte(j+1), byte(j*5+3)
	}
	return c
}

func TestDecodeLightRejectsBadArrays(t *testing.T) {
	t.Parallel()
	enc := lightWithArrays().encodeLight()
	if !newChunk(0, 0).decodeLight(enc) {
		t.Fatal("valid light rejected")
	}
	// Header: four trimmed masks, then the sky count and the first array's length.
	r := protocol.NewReader(enc)
	for range 4 {
		r.ReadBitSet()
	}
	hdr := len(enc) - r.Remaining()
	lenAt := hdr + 1
	for _, cut := range []int{1, lightArrayLen, len(enc) - hdr} {
		if newChunk(0, 0).decodeLight(enc[:len(enc)-cut]) {
			t.Errorf("truncated by %d accepted", cut)
		}
	}
	// Wrong declared lengths for the first sky array and the first block array.
	bad := bytes.Clone(enc)
	bad[lenAt], bad[lenAt+1] = 0xff, 0x0f // 2047
	if newChunk(0, 0).decodeLight(bad) {
		t.Error("wrong-length sky array accepted")
	}
	skyEnd := hdr + 1 + 17*(2+lightArrayLen)
	bad = bytes.Clone(enc)
	bad[skyEnd+1], bad[skyEnd+2] = 0x81, 0x10 // 2049
	if newChunk(0, 0).decodeLight(bad) {
		t.Error("wrong-length block array accepted")
	}
}

func TestDecodeLightOwnsMixedArrays(t *testing.T) {
	t.Parallel()
	c := lightWithArrays()
	enc := c.encodeLight()
	in := bytes.Clone(enc)
	d := newChunk(0, 0)
	if !d.decodeLight(in) {
		t.Fatal("decodeLight rejected encodeLight's output")
	}
	clear(in)
	if *d.skyLight[20] != *c.skyLight[20] || *d.blockLight[9] != *c.blockLight[9] {
		t.Error("a decoded mixed array aliases the input")
	}
	if d.skyLight[0] != &fullSection {
		t.Error("full section not shared")
	}
}

func BenchmarkDecodeLight(b *testing.B) {
	c := lightWithArrays()
	c.skyLight[20], c.blockLight[9] = nil, nil // mixed arrays allocate by design
	enc := c.encodeLight()
	d := newChunk(0, 0)
	b.ReportAllocs()
	for b.Loop() {
		if !d.decodeLight(enc) {
			b.Fatal("decodeLight failed")
		}
	}
}
