package nbt

import (
	"encoding/binary"

	"github.com/google/uuid"
)

// UUIDToIntArray encodes a UUID as the four big-endian ints the chunk and
// entity formats keep a reference in.
// vanilla: UUIDUtil.uuidToIntArray
func UUIDToIntArray(id uuid.UUID) IntArray {
	words := make(IntArray, 4)
	for i := range words {
		words[i] = int32(binary.BigEndian.Uint32(id[i*4:]))
	}
	return words
}

// UUIDFromIntArray decodes the UUID four big-endian ints name; ok is false
// for any other length.
// vanilla: UUIDUtil.uuidFromIntArray
func UUIDFromIntArray(words IntArray) (uuid.UUID, bool) {
	if len(words) != 4 {
		return uuid.Nil, false
	}
	var id uuid.UUID
	for i, w := range words {
		binary.BigEndian.PutUint32(id[i*4:], uint32(w))
	}
	return id, true
}

// UUIDWords reads the ints of a UUID from any shape the codec's int stream
// accepts: an int array, a byte or long array, or a list of numbers, each
// narrowed as Number.intValue does. ok is false for any other shape; the
// count is left for UUIDFromIntArray to check.
// vanilla: UUIDUtil.CODEC, NbtOps.getIntStream, DynamicOps.getIntStream
func UUIDWords(tag Tag) (IntArray, bool) {
	switch v := tag.(type) {
	case IntArray:
		return v, true
	case ByteArray:
		words := make(IntArray, len(v))
		for i, b := range v {
			words[i] = int32(int8(b))
		}
		return words, true
	case LongArray:
		words := make(IntArray, len(v))
		for i, n := range v {
			words[i] = int32(n)
		}
		return words, true
	case List:
		words := make(IntArray, len(v.Items))
		for i, item := range v.Items {
			n, ok := IntValue(item)
			if !ok {
				return nil, false
			}
			words[i] = n
		}
		return words, true
	}
	return nil, false
}

// UUIDField reads a UUID field in either shape UUIDWords takes; ok is false
// only when the field is missing or malformed, so the zero UUID reads as a
// real UUID.
func UUIDField(c Compound, key string) (uuid.UUID, bool) {
	words, ok := UUIDWords(c[key])
	if !ok {
		return uuid.Nil, false
	}
	return UUIDFromIntArray(words)
}
