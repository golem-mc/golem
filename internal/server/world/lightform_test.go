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
