// Package nbt reads and writes Named Binary Tag data, Minecraft's big-endian
// tree of named, typed tags. The network form, without the root's name, is
// EncodeNetwork and DecodeNetwork.
package nbt

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"
	"unicode/utf16"
)

// Type identifies what a tag holds. The numbers are the ones written to disk.
type Type uint8

const (
	TypeEnd Type = iota
	TypeByte
	TypeShort
	TypeInt
	TypeLong
	TypeFloat
	TypeDouble
	TypeByteArray
	TypeString
	TypeList
	TypeCompound
	TypeIntArray
	TypeLongArray
)

var typeNames = [...]string{
	"end", "byte", "short", "int", "long", "float", "double",
	"byte array", "string", "list", "compound", "int array", "long array",
}

func (t Type) String() string {
	if int(t) >= len(typeNames) {
		return fmt.Sprintf("tag %d", uint8(t))
	}
	return typeNames[t]
}

// Tag is one value in an NBT tree; only the concrete types below satisfy it.
type Tag interface {
	// Type is the tag number this value is written as.
	Type() Type
}

// The scalar tags, typed as the format stores them so a value round-trips
// byte for byte.
type (
	Byte   int8
	Short  int16
	Int    int32
	Long   int64
	Float  float32
	Double float64
	String string
)

// The array tags. ByteArray is bytes rather than signed bytes: everything
// that uses one (light data, UUIDs) treats it as a blob.
type (
	ByteArray []byte
	IntArray  []int32
	LongArray []int64
)

// Compound is a set of uniquely named tags. Encoding sorts the names, so a
// tree always encodes to the same bytes.
type Compound map[string]Tag

// List is a sequence of tags of one type, Elem; an empty list has Elem
// TypeEnd, as the game writes one.
type List struct {
	Elem  Type
	Items []Tag
}

func (Byte) Type() Type      { return TypeByte }
func (Short) Type() Type     { return TypeShort }
func (Int) Type() Type       { return TypeInt }
func (Long) Type() Type      { return TypeLong }
func (Float) Type() Type     { return TypeFloat }
func (Double) Type() Type    { return TypeDouble }
func (String) Type() Type    { return TypeString }
func (ByteArray) Type() Type { return TypeByteArray }
func (IntArray) Type() Type  { return TypeIntArray }
func (LongArray) Type() Type { return TypeLongArray }
func (Compound) Type() Type  { return TypeCompound }
func (List) Type() Type      { return TypeList }

// Get returns the tag under name if it is of type T; a missing name or one
// of another type (as another game version may write it) reads as absent.
// An OrderedCompound may be read as a Compound view; add or remove keys only on the original compound.
func Get[T Tag](c Compound, name string) (T, bool) {
	value, ok := c[name].(T)
	if !ok {
		// Map-based readers may borrow a view without stripping nested history.
		if compound, isCompound := c[name].(CompoundTag); isCompound {
			value, ok = any(compound.Entries()).(T)
		}
	}
	return value, ok
}

// Copy returns a deep copy of a tag, sharing no mutable state with it.
// vanilla: Tag.copy
func Copy(tag Tag) Tag {
	return copyTag(tag, false, false)
}

// OrderedCopy is Copy with map compounds promoted to insertion-aware compounds
// in their current HashOrderedKeys order, for subsequent command mutations.
func OrderedCopy(tag Tag) Tag {
	return copyTag(tag, false, true)
}

// Snapshot returns a deep copy retaining compound table capacity and ordering
// for reads that must outlive the source's lock.
func Snapshot(tag Tag) Tag {
	return copyTag(tag, true, false)
}

func copyTag(tag Tag, snapshot, orderMaps bool) Tag {
	switch v := tag.(type) {
	case *OrderedCompound:
		out := &OrderedCompound{}
		names := v.keys
		if !snapshot {
			names = HashOrderedKeys(v)
		}
		for _, name := range names {
			out.Set(name, copyTag(v.values[name], snapshot, orderMaps))
		}
		if snapshot {
			out.capacity = v.capacity
		}
		return out
	case Compound:
		if orderMaps {
			out := &OrderedCompound{}
			for _, name := range HashOrderedKeys(v) {
				out.Set(name, copyTag(v[name], snapshot, orderMaps))
			}
			return out
		}
		out := make(Compound, len(v))
		for name, child := range v {
			out[name] = copyTag(child, snapshot, orderMaps)
		}
		return out
	case List:
		out := List{Elem: v.Elem, Items: make([]Tag, len(v.Items))}
		for i, item := range v.Items {
			out.Items[i] = copyTag(item, snapshot, orderMaps)
		}
		return out
	case ByteArray:
		return slices.Clone(v)
	case IntArray:
		return slices.Clone(v)
	case LongArray:
		return slices.Clone(v)
	}
	return tag
}

// IntValue narrows a numeric tag to an int: integers keep their low 32
// bits, floats truncate toward zero and clamp, NaN is 0. ok is false for a
// non-number, which the game reads as absent. This is a codec's read
// (Codec.INT through NbtOps, Number.intValue); a compound's getIntOr floors
// instead, see IntField.
func IntValue(tag Tag) (int32, bool) {
	switch v := tag.(type) {
	case Byte:
		return int32(v), true
	case Short:
		return int32(v), true
	case Int:
		return int32(v), true
	case Long:
		return int32(v), true
	case Float:
		return clampInt(float64(v)), true
	case Double:
		return clampInt(float64(v)), true
	}
	return 0, false
}

// NumberValue is a numeric tag's value; ok is false for any other tag.
func NumberValue(tag Tag) (float64, bool) {
	switch v := tag.(type) {
	case Byte:
		return float64(v), true
	case Short:
		return float64(v), true
	case Int:
		return float64(v), true
	case Long:
		return float64(v), true
	case Float:
		return float64(v), true
	case Double:
		return float64(v), true
	}
	return 0, false
}

// LongValue narrows a numeric tag to a long: a float truncates toward zero
// but a double floors (FloatTag.longValue and DoubleTag.longValue differ),
// both clamped, NaN is 0. ok is false for a non-number.
func LongValue(tag Tag) (int64, bool) {
	switch v := tag.(type) {
	case Byte:
		return int64(v), true
	case Short:
		return int64(v), true
	case Int:
		return int64(v), true
	case Long:
		return int64(v), true
	case Float:
		return clampLong(float64(v)), true
	case Double:
		return clampLong(math.Floor(float64(v))), true
	}
	return 0, false
}

// ShortValue narrows a numeric tag to a short: integers keep their low 16
// bits, floats and doubles floor first (Mth.floor, so -1.5 is -2). ok is
// false for a non-number.
func ShortValue(tag Tag) (int16, bool) {
	switch v := tag.(type) {
	case Byte:
		return int16(v), true
	case Short:
		return int16(v), true
	case Int:
		return int16(v), true
	case Long:
		return int16(v), true
	case Float:
		return int16(clampInt(math.Floor(float64(v)))), true
	case Double:
		return int16(clampInt(math.Floor(float64(v)))), true
	}
	return 0, false
}

// ByteValue narrows a numeric tag to a byte: integers keep their low 8
// bits, floats and doubles floor first, NaN is 0. ok is false for a
// non-number.
func ByteValue(tag Tag) (int8, bool) {
	switch v := tag.(type) {
	case Byte:
		return int8(v), true
	case Short:
		return int8(v), true
	case Int:
		return int8(v), true
	case Long:
		return int8(v), true
	case Float:
		return int8(clampInt(math.Floor(float64(v)))), true
	case Double:
		return int8(clampInt(math.Floor(float64(v)))), true
	}
	return 0, false
}

// clampInt is Java's cast of a float to an int.
func clampInt(f float64) int32 {
	switch {
	case math.IsNaN(f):
		return 0
	case f >= math.MaxInt32:
		return math.MaxInt32
	case f <= math.MinInt32:
		return math.MinInt32
	default:
		return int32(math.Trunc(f))
	}
}

// clampLong is Java's cast of a float to a long.
func clampLong(f float64) int64 {
	switch {
	case math.IsNaN(f):
		return 0
	case f >= math.MaxInt64:
		return math.MaxInt64
	case f <= math.MinInt64:
		return math.MinInt64
	default:
		return int64(math.Trunc(f))
	}
}

// maxDepth bounds nesting, of the same order as the game's limit; without
// it a file of nothing but opening compounds would exhaust the stack.
const maxDepth = 512

// Decode reads an uncompressed NBT file, returning the root tag's name too
// (empty in save files, but part of the format).
func Decode(data []byte) (name string, tag Tag, err error) {
	return decode(data, false, nil)
}

// DecodeOrdered reads file compounds with their insertion history intact.
func DecodeOrdered(data []byte) (name string, tag Tag, err error) {
	return decode(data, true, nil)
}

// DecodeOrderedFields retains history below the named root fields, leaving the
// rest map-based for readers that do not need insertion history.
func DecodeOrderedFields(data []byte, fields ...string) (name string, tag Tag, err error) {
	return decode(data, false, fields)
}

func decode(data []byte, ordered bool, fields []string) (name string, tag Tag, err error) {
	d := &decoder{data: data, ordered: ordered, orderedFields: fields}

	tagType := Type(d.u8())
	if d.err != nil {
		return "", nil, d.err
	}
	if tagType == TypeEnd {
		return "", nil, fmt.Errorf("nbt: root tag is empty")
	}

	name = d.string()
	tag = d.payload(tagType, 0)
	if d.err != nil {
		return "", nil, d.err
	}
	return name, tag, nil
}

// decoder walks a file's bytes, keeping the first error; after it every
// read yields zero, so a malformed file falls out of the recursion.
type decoder struct {
	ordered       bool
	orderedFields []string
	data          []byte
	pos           int
	err           error
}

func (d *decoder) fail(format string, args ...any) {
	if d.err == nil {
		d.err = fmt.Errorf("nbt: "+format, args...)
	}
}

// take returns the next n bytes, or nil if there are not that many left.
func (d *decoder) take(n int) []byte {
	if d.err != nil {
		return nil
	}
	if n > len(d.data)-d.pos {
		d.fail("truncated at offset %d: want %d more bytes, have %d", d.pos, n, len(d.data)-d.pos)
		return nil
	}
	b := d.data[d.pos : d.pos+n]
	d.pos += n
	return b
}

func (d *decoder) u8() uint8 {
	b := d.take(1)
	if b == nil {
		return 0
	}
	return b[0]
}

func (d *decoder) u16() uint16 {
	b := d.take(2)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint16(b)
}

func (d *decoder) u32() uint32 {
	b := d.take(4)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

func (d *decoder) u64() uint64 {
	b := d.take(8)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

// count reads an array or list length and checks the data can hold that
// many elements of elemSize bytes, so a corrupt length cannot allocate for
// data that is not there. elemSize 0 means variable, at least one byte.
func (d *decoder) count(elemSize int) int {
	n := int32(d.u32())
	if d.err != nil {
		return 0
	}
	if n < 0 {
		d.fail("negative length %d at offset %d", n, d.pos)
		return 0
	}
	if elemSize == 0 {
		elemSize = 1
	}
	if int(n) > (len(d.data)-d.pos)/elemSize {
		d.fail("length %d at offset %d is longer than the rest of the file", n, d.pos)
		return 0
	}
	return int(n)
}

func (d *decoder) payload(tagType Type, depth int) Tag {
	if depth > maxDepth {
		d.fail("tags nested more than %d deep", maxDepth)
		return nil
	}

	switch tagType {
	case TypeByte:
		return Byte(d.u8())
	case TypeShort:
		return Short(d.u16())
	case TypeInt:
		return Int(d.u32())
	case TypeLong:
		return Long(d.u64())
	case TypeFloat:
		return Float(math.Float32frombits(d.u32()))
	case TypeDouble:
		return Double(math.Float64frombits(d.u64()))
	case TypeString:
		return String(d.string())

	case TypeByteArray:
		n := d.count(1)
		return ByteArray(slices.Clone(d.take(n)))
	case TypeIntArray:
		values := make(IntArray, d.count(4))
		for i := range values {
			values[i] = int32(d.u32())
		}
		return values
	case TypeLongArray:
		values := make(LongArray, d.count(8))
		for i := range values {
			values[i] = int64(d.u64())
		}
		return values

	case TypeList:
		return d.list(depth)
	case TypeCompound:
		return d.compound(depth)

	default:
		d.fail("unknown tag %d at offset %d", tagType, d.pos)
		return nil
	}
}

func (d *decoder) list(depth int) Tag {
	elem := Type(d.u8())
	if d.err != nil {
		return nil
	}

	// The game writes an empty list's type as end; a non-empty list of end
	// tags is unreadable, an end tag having no payload.
	if elem == TypeEnd {
		if n := d.count(0); n != 0 && d.err == nil {
			d.fail("list of %d end tags at offset %d", n, d.pos)
		}
		return List{}
	}

	n := d.count(0)
	// Not presized by n: four bytes can claim a count the data cannot back.
	// vanilla: ListTag.load sizes its ArrayList after charging the
	// accounter four bytes an entry.
	var items []Tag
	for range n {
		item := d.payload(elem, depth+1)
		if d.err != nil {
			return nil
		}
		items = append(items, item)
	}
	// A list of compounds may be the game's wrapping of a heterogeneous
	// list, each non-compound entry under a single empty key; unwrap them.
	// Elem becomes the type the unwrapped entries share, or compound for a
	// mixed list, which encode writes back the same.
	// vanilla: ListTag.tryUnwrap
	if elem == TypeCompound {
		unwrapped := false
		for i, item := range items {
			if value, ok := unwrap(item); ok {
				items[i] = value
				unwrapped = true
			}
		}
		if unwrapped {
			elem = listElem(items, items[0].Type())
		}
	}
	return List{Elem: elem, Items: items}
}

// ListElement returns what a heterogeneous list's wrapper holds, or tag itself
// when it is not such a wrapper.
// vanilla: ListTag.tryUnwrap
func ListElement(tag Tag) Tag {
	if value, ok := unwrap(tag); ok {
		return value
	}
	return tag
}

// unwrap returns what a heterogeneous list's wrapper holds: the one field of
// a compound whose only name is the empty string.
func unwrap(tag Tag) (Tag, bool) {
	compound, ok := tag.(Compound)
	if !ok {
		ordered, ok := tag.(*OrderedCompound)
		if !ok {
			return nil, false
		}
		compound = ordered.values
	}
	if len(compound) != 1 {
		return nil, false
	}
	value, ok := compound[""]
	return value, ok
}

func (d *decoder) compound(depth int) Tag {
	var fields CompoundTag = Compound{}
	if d.ordered {
		fields = &OrderedCompound{}
	}
	for {
		tagType := Type(d.u8())
		if d.err != nil {
			return nil
		}
		if tagType == TypeEnd {
			return fields
		}
		name := d.string()
		ordered := d.ordered
		if depth == 0 && slices.Contains(d.orderedFields, name) {
			d.ordered = true
		}
		value := d.payload(tagType, depth+1)
		d.ordered = ordered
		if d.err != nil {
			return nil
		}
		fields.Set(name, value)
	}
}

// string reads Java's modified UTF-8: NUL takes two bytes and runes outside
// the basic plane are two surrogates.
func (d *decoder) string() string {
	raw := d.take(int(d.u16()))
	if d.err != nil {
		return ""
	}
	if isASCII(raw) {
		return string(raw)
	}

	units := make([]uint16, 0, len(raw))
	for i := 0; i < len(raw); {
		b := raw[i]
		switch {
		case b < 0x80:
			units = append(units, uint16(b))
			i++
		case b&0xE0 == 0xC0 && i+1 < len(raw):
			units = append(units, uint16(b&0x1F)<<6|uint16(raw[i+1]&0x3F))
			i += 2
		case b&0xF0 == 0xE0 && i+2 < len(raw):
			units = append(units, uint16(b&0x0F)<<12|uint16(raw[i+1]&0x3F)<<6|uint16(raw[i+2]&0x3F))
			i += 3
		default:
			d.fail("string at offset %d is not modified UTF-8", d.pos-len(raw))
			return ""
		}
	}
	return string(utf16.Decode(units))
}

// isASCII reports whether no byte of b is 0x80 or above; such bytes are the
// same in modified UTF-8 and Go's UTF-8, a raw NUL included.
func isASCII(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return false
		}
	}
	return true
}

// Encode writes an uncompressed NBT file; save files pass an empty name.
func Encode(name string, tag Tag) ([]byte, error) {
	data, err := AppendEncode(nil, name, tag)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// AppendEncode appends Encode's bytes to dst, so a caller can reuse one
// buffer across encodes. On an error dst comes back with its length unchanged.
func AppendEncode(dst []byte, name string, tag Tag) ([]byte, error) {
	if tag == nil || tag.Type() == TypeEnd {
		return dst, fmt.Errorf("nbt: cannot encode an empty root tag")
	}

	e := &encoder{buf: dst}
	e.u8(uint8(tag.Type()))
	e.string(name)
	e.payload(tag)
	if e.err != nil {
		return dst, e.err
	}
	return e.buf, nil
}

// EncodeNetwork writes NBT as packets carry it: Encode's bytes without the
// root's name.
func EncodeNetwork(tag Tag) ([]byte, error) {
	data, err := Encode("", tag)
	if err != nil {
		return nil, err
	}
	return append(data[:1:1], data[3:]...), nil
}

// DecodeNetwork reads NBT as packets carry it, answering the bytes taken so
// a caller reading a stream of values knows where this one ends.
func DecodeNetwork(data []byte) (tag Tag, n int, err error) {
	return decodeNetwork(data, false)
}

// DecodeNetworkOrdered is DecodeNetwork retaining compound insertion history.
func DecodeNetworkOrdered(data []byte) (tag Tag, n int, err error) {
	return decodeNetwork(data, true)
}

func decodeNetwork(data []byte, ordered bool) (tag Tag, n int, err error) {
	d := &decoder{data: data, ordered: ordered}
	tagType := Type(d.u8())
	if d.err != nil {
		return nil, 0, d.err
	}
	if tagType == TypeEnd {
		return nil, 0, fmt.Errorf("nbt: root tag is empty")
	}
	tag = d.payload(tagType, 0)
	if d.err != nil {
		return nil, 0, d.err
	}
	return tag, d.pos, nil
}

// encoder keeps the first error, as decoder does.
type encoder struct {
	buf []byte
	err error
}

func (e *encoder) fail(format string, args ...any) {
	if e.err == nil {
		e.err = fmt.Errorf("nbt: "+format, args...)
	}
}

func (e *encoder) u8(v uint8)   { e.buf = append(e.buf, v) }
func (e *encoder) u16(v uint16) { e.buf = binary.BigEndian.AppendUint16(e.buf, v) }
func (e *encoder) u32(v uint32) { e.buf = binary.BigEndian.AppendUint32(e.buf, v) }
func (e *encoder) u64(v uint64) { e.buf = binary.BigEndian.AppendUint64(e.buf, v) }

func (e *encoder) payload(tag Tag) {
	if e.err != nil {
		return
	}

	switch v := tag.(type) {
	case Byte:
		e.u8(uint8(v))
	case Short:
		e.u16(uint16(v))
	case Int:
		e.u32(uint32(v))
	case Long:
		e.u64(uint64(v))
	case Float:
		e.u32(math.Float32bits(float32(v)))
	case Double:
		e.u64(math.Float64bits(float64(v)))
	case String:
		e.string(string(v))

	case ByteArray:
		e.u32(uint32(len(v)))
		e.buf = append(e.buf, v...)
	case IntArray:
		e.u32(uint32(len(v)))
		for _, n := range v {
			e.u32(uint32(n))
		}
	case LongArray:
		e.u32(uint32(len(v)))
		for _, n := range v {
			e.u64(uint64(n))
		}

	case List:
		e.list(v)
	case Compound:
		e.compound(v)
	case *OrderedCompound:
		e.compoundEntries(v.values, HashOrderedKeys(v))

	default:
		e.fail("cannot encode %T", tag)
	}
}

func (e *encoder) list(l List) {
	// An empty list is written as a list of end tags, whatever its Elem, as
	// the game writes it.
	if len(l.Items) == 0 {
		e.u8(uint8(TypeEnd))
		e.u32(0)
		return
	}

	// A heterogeneous list is saved as a list of compounds, each entry that
	// is not a plain compound wrapped under a single empty key, and decode
	// unwraps it again.
	// vanilla: ListTag.write
	if heterogeneous(l.Items) {
		e.u8(uint8(TypeCompound))
		e.u32(uint32(len(l.Items)))
		for i, item := range l.Items {
			if item == nil {
				e.fail("list holds nothing at index %d", i)
				return
			}
			e.compoundEntry(item)
		}
		return
	}

	// The type the entries share counts, not Elem, so a stale Elem still
	// saves what the list holds.
	// vanilla: ListTag.identifyRawElementType
	elem := itemType(l.Items[0])
	e.u8(uint8(elem))
	e.u32(uint32(len(l.Items)))
	for i, item := range l.Items {
		if item == nil {
			e.fail("list holds nothing at index %d", i)
			return
		}
		e.payload(item)
	}
}

// compoundEntry writes one entry of a mixed list: a plain compound as is,
// anything else (a wrapper-shaped compound included) under the empty name.
//
// Deliberately unlike vanilla, a list of only compounds is written as is
// even where an entry looks like a wrapper: protocol's text components, the command SNBT reader and the sign encoder
// build mixed lists already wrapped, and wrapping again would nest them.
// vanilla: ListTag.wrapIfNeeded
func (e *encoder) compoundEntry(item Tag) {
	if compound, ok := unwrappedCompound(item); ok {
		e.payload(compound)
	} else {
		// Keep the transient wrapper on the stack, outside the Tag interface.
		e.compound(Compound{"": item})
	}
}

// heterogeneous reports whether a list's entries are not all one tag type;
// any two sub-lists count as the same type, as the wire format names them.
func heterogeneous(items []Tag) bool {
	if len(items) < 2 {
		return false
	}
	first := itemType(items[0])
	for _, item := range items[1:] {
		if itemType(item) != first {
			return true
		}
	}
	return false
}

func itemType(tag Tag) Type {
	if tag == nil {
		return TypeEnd
	}
	return tag.Type()
}

func (e *encoder) compound(c Compound) {
	// Sorted so the same tree always produces the same file; Go's map order
	// is random.
	var stack [16]string
	names := stack[:0]
	for name := range c {
		names = append(names, name)
	}
	slices.Sort(names)
	e.compoundEntries(c, names)
}

func (e *encoder) compoundEntries(c Compound, names []string) {
	for _, name := range names {
		value := c[name]
		if value == nil {
			e.fail("field %q holds nothing", name)
			return
		}
		e.u8(uint8(value.Type()))
		e.string(name)
		e.payload(value)
		if e.err != nil {
			return
		}
	}
	e.u8(uint8(TypeEnd))
}

func (e *encoder) string(s string) {
	buf, ok := AppendString(e.buf, s)
	if !ok {
		e.fail("string is longer than %d bytes once encoded", MaxStringLen)
		return
	}
	e.buf = buf
}

// MaxStringLen is the longest a string may be once encoded; the length in
// front of it is an unsigned short.
const MaxStringLen = math.MaxUint16

// AppendString appends s in NBT's string form: a big-endian uint16 byte
// length, then Java's modified UTF-8, where NUL takes two bytes (no byte is
// zero) and runes outside the basic plane are written as two surrogates.
// A string longer than MaxStringLen encoded is not written: dst comes back
// unchanged and the second result is false. Exported so the packets carrying NBT share
// this encoding rather than a second copy that gets one rule wrong.
func AppendString(dst []byte, s string) ([]byte, bool) {
	// The length is filled in once the encoded size is known.
	start := len(dst)
	dst = binary.BigEndian.AppendUint16(dst, 0)

	for _, r := range s {
		switch {
		case r == 0:
			// Encoded the long way round, so that no byte of a string is zero.
			dst = append(dst, 0xC0, 0x80)
		case r < 0x80:
			dst = append(dst, byte(r))
		case r < 0x800:
			dst = append(dst, byte(0xC0|r>>6), byte(0x80|r&0x3F))
		case r <= 0xFFFF:
			dst = appendThreeByteRune(dst, r)
		default:
			// Each half is a surrogate a UTF-8 encoder refuses to write.
			high, low := utf16.EncodeRune(r)
			dst = appendThreeByteRune(dst, high)
			dst = appendThreeByteRune(dst, low)
		}
	}

	length := len(dst) - start - 2
	if length > MaxStringLen {
		return dst[:start], false
	}
	binary.BigEndian.PutUint16(dst[start:], uint16(length))
	return dst, true
}

// appendThreeByteRune appends a code point in U+0800 to U+FFFF, including
// the surrogate halves a well-formed UTF-8 encoder refuses to produce.
func appendThreeByteRune(dst []byte, r rune) []byte {
	return append(dst, byte(0xE0|r>>12), byte(0x80|r>>6&0x3F), byte(0x80|r&0x3F))
}
