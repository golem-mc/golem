package world

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/bits"

	"github.com/pierrec/lz4/v4"
)

// Compression type 4 is not the LZ4 frame format but lz4-java's LZ4Block
// framing: 64 KiB blocks, each in an envelope of magic, token byte,
// little-endian int32 compressed and uncompressed lengths, the block's XXH32,
// then the raw LZ4 block (stored verbatim when LZ4 cannot shrink it). An
// envelope with both lengths zero ends the stream.
// vanilla: RegionFileVersion.VERSION_LZ4 over net.jpountz.lz4.LZ4BlockOutputStream
const (
	lz4BlockMagic   = "LZ4Block"
	lz4MethodStored = byte(0x10) // block stored verbatim
	lz4MethodLZ4    = byte(0x20) // block in the raw LZ4 block format

	// lz4Level65536 is the 64 KiB block's size class, 32 - bits.Len(64*1024-1)
	// - 10: the token's low nibble, which a reader turns back into the bound 1
	// << (10 + level).
	lz4Level65536 = byte(6)
	lz4BlockSize  = 64 * 1024

	// lz4ChecksumSeed is XXH32's default seed, used for every block.
	// vanilla: LZ4BlockOutputStream.DEFAULT_SEED
	lz4ChecksumSeed = 0x9747B28C

	// The envelope checksum is XXH32 without its top four bits: lz4-java
	// computes it through StreamingXXHash32.asChecksum, whose getValue masks
	// with 0xFFFFFFF, for both LZ4Block streams.
	lz4ChecksumMask = 0x0FFFFFFF

	// lz4EnvelopeLen is the magic, the token and the three lengths.
	lz4EnvelopeLen = len(lz4BlockMagic) + 1 + 4 + 4 + 4
)

// lz4CompressStream frames data as vanilla reads it: one envelope per 64 KiB
// block, then the empty terminator.
func lz4CompressStream(data []byte) []byte {
	out := make([]byte, 0, len(data)+2*lz4EnvelopeLen)
	for len(data) > 0 {
		block := data[:min(lz4BlockSize, len(data))]
		data = data[len(block):]

		method, body := lz4MethodStored, block
		var c lz4.Compressor
		bound := make([]byte, lz4.CompressBlockBound(len(block)))
		if n, err := c.CompressBlock(block, bound); err == nil && n > 0 && n < len(block) {
			method, body = lz4MethodLZ4, bound[:n]
		}
		out = appendEnvelope(out, method|lz4Level65536, body, block)
	}
	// The terminal envelope: both lengths and the checksum zero.
	out = append(out, lz4BlockMagic...)
	out = append(out, lz4MethodStored|lz4Level65536)
	var zeros [12]byte
	return append(out, zeros[:]...)
}

// appendEnvelope adds one envelope: magic, token, both lengths, the raw block's
// checksum, then body.
func appendEnvelope(dst []byte, token byte, body, raw []byte) []byte {
	var head [lz4EnvelopeLen]byte
	copy(head[:len(lz4BlockMagic)], lz4BlockMagic)
	head[len(lz4BlockMagic)] = token
	binary.LittleEndian.PutUint32(head[len(lz4BlockMagic)+1:], uint32(len(body)))
	binary.LittleEndian.PutUint32(head[len(lz4BlockMagic)+5:], uint32(len(raw)))
	binary.LittleEndian.PutUint32(head[len(lz4BlockMagic)+9:], lz4Checksum(raw))
	dst = append(dst, head[:]...)
	return append(dst, body...)
}

// lz4Checksum is the truncated XXH32 an envelope carries.
func lz4Checksum(raw []byte) uint32 {
	return xxhash32(raw, lz4ChecksumSeed) & lz4ChecksumMask
}

// lz4DecompressStream reads LZ4Block framing, refusing broken structure or
// checksums rather than decoding what it can, so a damaged chunk is unreadable
// instead of half-read.
func lz4DecompressStream(data []byte) ([]byte, error) {
	var out bytes.Buffer
	for {
		if len(data) < lz4EnvelopeLen {
			return nil, io.ErrUnexpectedEOF
		}
		if !bytes.Equal(data[:len(lz4BlockMagic)], []byte(lz4BlockMagic)) {
			return nil, errors.New("lz4: block magic missing")
		}
		token := data[len(lz4BlockMagic)]
		method := token & 0xF0
		compressedLen := int(binary.LittleEndian.Uint32(data[len(lz4BlockMagic)+1:]))
		originalLen := int(binary.LittleEndian.Uint32(data[len(lz4BlockMagic)+5:]))
		check := binary.LittleEndian.Uint32(data[len(lz4BlockMagic)+9:])
		data = data[lz4EnvelopeLen:]
		if method != lz4MethodStored && method != lz4MethodLZ4 {
			return nil, fmt.Errorf("lz4: unknown block method %#x", method)
		}
		if originalLen == 0 {
			if compressedLen != 0 || check != 0 {
				return nil, errors.New("lz4: corrupt terminal block")
			}
			return out.Bytes(), nil
		}
		// The token's low nibble is the size class, which bounds both lengths
		// before anything is allocated.
		if compressedLen == 0 || originalLen > 1<<(10+(token&0xF)) ||
			(method == lz4MethodStored && originalLen != compressedLen) {
			return nil, errors.New("lz4: block lengths out of bounds")
		}
		if len(data) < compressedLen {
			return nil, io.ErrUnexpectedEOF
		}
		body := data[:compressedLen]
		data = data[compressedLen:]

		var raw []byte
		if method == lz4MethodStored {
			raw = body
		} else {
			raw = make([]byte, originalLen)
			if n, err := lz4.UncompressBlock(body, raw); err != nil || n != originalLen {
				return nil, errors.New("lz4: block would not decode")
			}
		}
		if lz4Checksum(raw) != check {
			return nil, errors.New("lz4: block checksum mismatch")
		}
		out.Write(raw)
	}
}

// xxhash32 is XXH32, each envelope's checksum of its uncompressed bytes.
// vanilla: net.jpountz.xxhash in the same library
func xxhash32(data []byte, seed uint32) uint32 {
	const (
		p1 = 2654435761
		p2 = 2246822519
		p3 = 3266489917
		p4 = 668265263
		p5 = 374761393
	)
	round := func(acc, v uint32) uint32 {
		acc += v * p2
		acc = bits.RotateLeft32(acc, 13)
		return acc * p1
	}
	n := len(data)
	var h uint32
	if n >= 16 {
		v1, v2, v3, v4 := seed+p1+p2, seed+p2, seed, seed-p1
		for len(data) >= 16 {
			v1 = round(v1, binary.LittleEndian.Uint32(data[0:]))
			v2 = round(v2, binary.LittleEndian.Uint32(data[4:]))
			v3 = round(v3, binary.LittleEndian.Uint32(data[8:]))
			v4 = round(v4, binary.LittleEndian.Uint32(data[12:]))
			data = data[16:]
		}
		h = bits.RotateLeft32(v1, 1) + bits.RotateLeft32(v2, 7) +
			bits.RotateLeft32(v3, 12) + bits.RotateLeft32(v4, 18)
	} else {
		h = seed + p5
	}
	h += uint32(n)
	for len(data) >= 4 {
		h += binary.LittleEndian.Uint32(data) * p3
		h = bits.RotateLeft32(h, 17) * p4
		data = data[4:]
	}
	for _, b := range data {
		h += uint32(b) * p5
		h = bits.RotateLeft32(h, 11) * p1
	}
	h ^= h >> 15
	h *= p2
	h ^= h >> 13
	h *= p3
	h ^= h >> 16
	return h
}
