package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Buffer builds a packet payload. Its Put methods never fail: the payload is
// assembled in memory and only handed to a Conn once it is complete.
type Buffer struct {
	data []byte
}

// Bytes returns the payload built so far. It is only valid until the next Put.
func (b *Buffer) Bytes() []byte { return b.data }

// Len returns the number of bytes written so far.
func (b *Buffer) Len() int { return len(b.data) }

// Reset empties the buffer while keeping its backing array for reuse.
func (b *Buffer) Reset() { b.data = b.data[:0] }

// Grow makes room for n more bytes, so the Puts that fill them do not
// reallocate.
func (b *Buffer) Grow(n int) { b.data = slices.Grow(b.data, n) }

func (b *Buffer) PutBool(v bool) {
	if v {
		b.PutU8(1)
	} else {
		b.PutU8(0)
	}
}

func (b *Buffer) PutU8(v uint8) { b.data = append(b.data, v) }

func (b *Buffer) PutI8(v int8) { b.PutU8(uint8(v)) }

func (b *Buffer) PutI16(v int16) { b.data = binary.BigEndian.AppendUint16(b.data, uint16(v)) }

func (b *Buffer) PutI32(v int32) { b.data = binary.BigEndian.AppendUint32(b.data, uint32(v)) }

func (b *Buffer) PutI64(v int64) { b.data = binary.BigEndian.AppendUint64(b.data, uint64(v)) }

func (b *Buffer) PutF32(v float32) { b.PutI32(int32(math.Float32bits(v))) }

func (b *Buffer) PutF64(v float64) { b.PutI64(int64(math.Float64bits(v))) }

// putVarUint writes a VarInt-encoded unsigned integer; PutVarInt and
// PutVarLong are the same algorithm at different widths and delegate here.
func (b *Buffer) putVarUint(u uint64) {
	for {
		if u&^0x7F == 0 {
			b.PutU8(uint8(u))
			return
		}
		b.PutU8(uint8(u&0x7F) | 0x80)
		u >>= 7
	}
}

func (b *Buffer) PutVarInt(v int32) { b.putVarUint(uint64(uint32(v))) }

func (b *Buffer) PutVarLong(v int64) { b.putVarUint(uint64(v)) }

// PutString writes a VarInt-length-prefixed UTF-8 string.
func (b *Buffer) PutString(s string) {
	b.PutVarInt(int32(len(s)))
	b.data = append(b.data, s...)
}

func (b *Buffer) PutUUID(u uuid.UUID) { b.data = append(b.data, u[:]...) }

// PutBytes writes a VarInt-length-prefixed byte array.
func (b *Buffer) PutBytes(v []byte) {
	b.PutVarInt(int32(len(v)))
	b.data = append(b.data, v...)
}

// PutRaw appends bytes without a length prefix.
func (b *Buffer) PutRaw(v []byte) { b.data = append(b.data, v...) }

// Write makes Buffer an io.Writer, so that it can be used as a sink for
// compressed data.
func (b *Buffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

// PutPosition writes a block position packed into a single long: x and z as
// 26-bit and y as a 12-bit signed integer.
func (b *Buffer) PutPosition(x, y, z int32) {
	b.PutI64(int64(x&0x3FFFFFF)<<38 | int64(z&0x3FFFFFF)<<12 | int64(y&0xFFF))
}

// ReadPosition unpacks a block position from a single long: x and z as 26-bit
// and y as a 12-bit signed integer. The three are sign-extended from their
// widths, so a y of -1 arrives as -1 rather than 0xFFF.
func (r *Reader) ReadPosition() (x, y, z int32) {
	v := r.ReadI64()
	// x sits at the top of the long, so shifting it down sign-extends in one
	// go; y is at the bottom, which a double shift moves through the sign bit.
	// z is in the middle, so it has to be masked out and sign-extended by hand.
	x = int32(v >> 38)
	z = int32(v>>12&0x3FFFFFF) << 6 >> 6
	y = int32(v << 52 >> 52)
	return x, y, z
}

// PutAngle writes a rotation as one byte of 1/256 turn. Vanilla floors and
// lets the byte wrap, so any angle is valid; it is reduced first so a huge
// client value cannot overflow the conversion.
func (b *Buffer) PutAngle(degrees float32) {
	turn := math.Mod(float64(degrees), 360) * 256 / 360
	b.PutU8(uint8(int32(math.Floor(turn))))
}

// LpVec3 packs three doubles into six bytes (Spawn Entity, Set Entity
// Velocity): each divided by the largest component rounded up, sent as 15
// bits in -1..1, with that scale in the two spare bits or, if too big, as a
// VarInt after them.
const (
	// lpQuantized is the range a component is scaled into once packed.
	lpQuantized = 32766.0
	// lpScaleBits holds the magnitude; lpContinuation says it needed more.
	lpScaleBits    = 0x03
	lpContinuation = 0x04
)

// PutLpVec3 writes a vector in the packed form described on LpVec3.
func (b *Buffer) PutLpVec3(x, y, z float64) {
	magnitude := math.Max(math.Abs(x), math.Max(math.Abs(y), math.Abs(z)))
	// Anything below the quantization step is indistinguishable from
	// standing still: a single zero byte.
	if math.IsNaN(magnitude) || magnitude < 1/lpQuantized {
		b.PutU8(0)
		return
	}

	scale := int64(math.Ceil(magnitude))
	packed := uint64(scale & lpScaleBits)
	if scale&lpScaleBits != scale {
		packed |= lpContinuation
	}
	packed |= lpPack(x/float64(scale)) << 3
	packed |= lpPack(y/float64(scale)) << 18
	packed |= lpPack(z/float64(scale)) << 33

	// The first two bytes go out little-endian and the rest big-endian,
	// which is how the client reads them back.
	b.PutU8(uint8(packed))
	b.PutU8(uint8(packed >> 8))
	b.PutI32(int32(packed >> 16))
	if packed&lpContinuation != 0 {
		b.PutVarInt(int32(scale >> 2))
	}
}

// lpPack scales a component of -1 to 1 into the 15 bits it is sent as.
func lpPack(v float64) uint64 {
	return uint64(math.Round((v*0.5 + 0.5) * lpQuantized))
}

// PutMetadata starts a metadata entry; a run of them ends with EndMetadata,
// and the client keeps any field it is not sent.
func (b *Buffer) PutMetadata(index uint8, fieldType int32) {
	b.PutU8(index)
	b.PutVarInt(fieldType)
}

// EndMetadata closes a metadata array; without it the client keeps reading
// the packet as though more fields followed.
func (b *Buffer) EndMetadata() { b.PutU8(MetadataEnd) }

// PutBitSet writes a BitSet as a length-prefixed byte array: the longs
// little-endian with trailing zero bytes dropped (bit i is bit i%8 of byte
// i/8).
//
// vanilla: ByteBufCodecs.BIT_SET
func (b *Buffer) PutBitSet(longs []uint64) {
	raw := make([]byte, 0, 8*len(longs))
	for _, l := range longs {
		for i := range 8 {
			raw = append(raw, byte(l>>(8*i)))
		}
	}
	for len(raw) > 0 && raw[len(raw)-1] == 0 {
		raw = raw[:len(raw)-1]
	}
	b.PutVarInt(int32(len(raw)))
	b.PutRaw(raw)
}

// ErrShortPacket is reported when a packet ends in the middle of a field.
var ErrShortPacket = errors.New("packet ended unexpectedly")

// Reader reads fields from a packet payload. Errors are sticky: after a failed
// read every following read yields the zero value and Err reports the failure,
// so packets can be parsed without checking each field individually.
type Reader struct {
	data []byte
	pos  int
	err  error
}

// NewReader returns a Reader over a packet payload.
func NewReader(data []byte) *Reader { return &Reader{data: data} }

// Err returns the first error encountered, if any.
func (r *Reader) Err() error { return r.err }

// Remaining returns the number of unread bytes.
func (r *Reader) Remaining() int { return len(r.data) - r.pos }

// fail records err unless an earlier error was already recorded.
func (r *Reader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// take consumes n bytes, reporting ErrShortPacket if fewer are left.
func (r *Reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || r.Remaining() < n {
		r.fail(ErrShortPacket)
		return nil
	}
	v := r.data[r.pos : r.pos+n]
	r.pos += n
	return v
}

func (r *Reader) ReadBool() bool { return r.ReadU8() != 0 }

func (r *Reader) ReadU8() uint8 {
	v := r.take(1)
	if v == nil {
		return 0
	}
	return v[0]
}

func (r *Reader) ReadI8() int8 { return int8(r.ReadU8()) }

func (r *Reader) ReadU16() uint16 {
	v := r.take(2)
	if v == nil {
		return 0
	}
	return binary.BigEndian.Uint16(v)
}

func (r *Reader) ReadI16() int16 { return int16(r.ReadU16()) }

func (r *Reader) ReadI32() int32 {
	v := r.take(4)
	if v == nil {
		return 0
	}
	return int32(binary.BigEndian.Uint32(v))
}

func (r *Reader) ReadI64() int64 {
	v := r.take(8)
	if v == nil {
		return 0
	}
	return int64(binary.BigEndian.Uint64(v))
}

func (r *Reader) ReadF32() float32 { return math.Float32frombits(uint32(r.ReadI32())) }

func (r *Reader) ReadF64() float64 { return math.Float64frombits(uint64(r.ReadI64())) }

func (r *Reader) ReadVarInt() int32 {
	var result uint32
	for i := range 5 {
		b := r.ReadU8()
		if r.err != nil {
			return 0
		}
		result |= uint32(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			return int32(result)
		}
	}
	r.fail(ErrVarIntTooLong)
	return 0
}

// ReadVarLong reads a 64-bit VarLong, the wider sibling of ReadVarInt, used
// by the few fields that need more than 32 bits, such as Update Time's clock.
func (r *Reader) ReadVarLong() int64 {
	var result uint64
	for i := range 10 {
		b := r.ReadU8()
		if r.err != nil {
			return 0
		}
		result |= uint64(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			return int64(result)
		}
	}
	r.fail(ErrVarIntTooLong)
	return 0
}

// ReadString reads a VarInt-prefixed UTF-8 string of at most maxCodeUnits
// UTF-16 units: the bytes are checked against 3n (three bytes a unit at
// most), then the decoded unit count against n.
//
// vanilla: Utf8String.read
func (r *Reader) ReadString(maxCodeUnits int32) string {
	length := r.ReadVarInt()
	if r.err != nil {
		return ""
	}
	if maxBytes := 3 * maxCodeUnits; length < 0 || length > maxBytes {
		r.fail(fmt.Errorf("string of %d bytes out of range (max %d)", length, maxBytes))
		return ""
	}
	raw := string(r.take(int(length)))
	s := utf8Replaced(raw)
	// Decoding never yields more units than bytes, so only a string over
	// the limit in bytes needs counting.
	if len(raw) > int(maxCodeUnits) {
		if units := utf16Len(s); units > int(maxCodeUnits) {
			r.fail(fmt.Errorf("string of %d code units out of range (max %d)", units, maxCodeUnits))
			return ""
		}
	}
	return s
}

// utf8Replaced decodes s with one U+FFFD per malformed sequence.
//
// vanilla: ByteBufUtil.decodeString → new String(bytes, UTF_8)
func utf8Replaced(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r != utf8.RuneError || size != 1 {
			b.WriteString(s[i : i+size])
			i += size
			continue
		}
		b.WriteRune(utf8.RuneError)
		i += malformedLen(s[i:])
	}
	return b.String()
}

// utf16Len returns how many UTF-16 code units s takes: one per rune, and a
// second for each astral rune, whose lead byte lies in F0..F4. It reads
// bytes, not runes, so it needs s valid UTF-8, as utf8Replaced's output is.
func utf16Len(s string) int {
	units := 0
	for i := 0; i < len(s); i++ {
		units += int(utf16UnitOf[s[i]])
	}
	return units
}

// utf16UnitOf maps a UTF-8 byte to the units its rune contributes: one for
// ASCII and one- to three-byte leads, two for an astral lead, none for a
// continuation.
var utf16UnitOf = func() (t [256]int8) {
	for i := range 0x80 {
		t[i] = 1
	}
	for i := 0xC2; i < 0xF0; i++ {
		t[i] = 1
	}
	for i := 0xF0; i < 0xF5; i++ {
		t[i] = 2
	}
	return t
}()

// malformedLen returns how much of s Java's decoder consumes for the
// malformed sequence at its start: the lead and fitting continuations, or the
// whole truncated tail.
//
// vanilla: ByteBufUtil.decodeString → new String(bytes, UTF_8)
func malformedLen(s string) int {
	b1 := s[0]
	switch {
	case b1 < 0xC2 || b1 >= 0xF8:
		// A continuation byte (80..BF), or a lead that only ever makes
		// overlong or illegal sequences (C0, C1, F5..FF).
		return 1
	case b1 < 0xE0: // 110xxxxx: C2..DF, which any continuation completes
		return 1
	case b1 < 0xF0: // 1110xxxx: E0..EF
		if len(s) > 1 && (b1 == 0xE0 && s[1]&0xE0 == 0x80 || s[1]&0xC0 != 0x80) {
			return 1 // E0 continues A0..BF; 80..9F is overlong
		}
		if len(s) > 2 && s[2]&0xC0 != 0x80 {
			return 2
		}
		if len(s) > 2 {
			return 3 // an encoded surrogate is replaced whole
		}
		return len(s)
	default: // 11110xxx: F0..F7
		if b1 > 0xF4 || len(s) > 1 &&
			(b1 == 0xF0 && s[1] < 0x90 || b1 == 0xF4 && s[1] >= 0x90 || s[1]&0xC0 != 0x80) {
			return 1 // F5..F7, and F0/F4 leads outside their range, are the lead's fault
		}
		if len(s) > 2 && s[2]&0xC0 != 0x80 {
			return 2
		}
		if len(s) > 3 && s[3]&0xC0 != 0x80 {
			return 3
		}
		return len(s)
	}
}

func (r *Reader) ReadUUID() uuid.UUID {
	var u uuid.UUID
	copy(u[:], r.take(len(u)))
	return u
}

// ReadAngle reads a rotation stored as 1/256 of a full turn and returns it in
// degrees.
func (r *Reader) ReadAngle() float32 {
	return float32(r.ReadU8()) * 360 / 256
}

// ReadLpVec3 reads a vector in the packed form described on LpVec3.
func (r *Reader) ReadLpVec3() (x, y, z float64) {
	first := r.ReadU8()
	if r.err != nil || first == 0 {
		return 0, 0, 0
	}
	second := r.ReadU8()
	rest := uint32(r.ReadI32())
	packed := uint64(rest)<<16 | uint64(second)<<8 | uint64(first)

	scale := uint64(first & lpScaleBits)
	if first&lpContinuation != 0 {
		scale |= uint64(uint32(r.ReadVarInt())) << 2
	}
	return lpUnpack(packed>>3) * float64(scale),
		lpUnpack(packed>>18) * float64(scale),
		lpUnpack(packed>>33) * float64(scale)
}

// lpUnpack turns the 15 bits a component was sent as back into -1 to 1.
func lpUnpack(v uint64) float64 {
	return math.Min(float64(v&0x7FFF), lpQuantized)*2/lpQuantized - 1
}

// ReadBytes reads a VarInt-length-prefixed byte array of at most max bytes.
func (r *Reader) ReadBytes(max int32) []byte {
	length := r.ReadVarInt()
	if r.err != nil {
		return nil
	}
	if length < 0 || length > max {
		r.fail(fmt.Errorf("byte array length %d out of range (max %d)", length, max))
		return nil
	}
	return r.take(int(length))
}

// ReadBitSet reads a BitSet written by PutBitSet and returns its low 64 bits;
// it fails the reader on a set with a higher bit.
func (r *Reader) ReadBitSet() uint64 {
	n := r.ReadVarInt()
	if n < 0 || n > 8 {
		r.fail(errors.New("BitSet wider than 64 bits"))
		return 0
	}
	var v uint64
	for i, c := range r.take(int(n)) {
		v |= uint64(c) << (8 * i)
	}
	return v
}

// Rest returns the unread remainder of the payload.
func (r *Reader) Rest() []byte {
	if r.err != nil {
		return nil
	}
	return r.take(r.Remaining())
}

// Skip consumes n bytes, reporting ErrShortPacket if fewer are left; for a
// fixed-width field that is not needed (a chat signature).
func (r *Reader) Skip(n int) { r.take(n) }

// SkipProfileProperties skips Login Success and Player Info Update's profile
// properties (name, value, optional signature), returning their count.
func (r *Reader) SkipProfileProperties() int32 {
	count := r.ReadVarInt()
	for range count {
		r.ReadString(64)
		r.ReadString(32767)
		if r.ReadBool() {
			r.ReadString(1024)
		}
	}
	return count
}
