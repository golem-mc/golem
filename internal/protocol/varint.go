package protocol

import (
	"errors"
	"io"
)

var ErrVarIntTooLong = errors.New("VarInt is too long")

// ReadVarInt reads a VarInt from a stream, returning the decoded value and the
// number of bytes read. An io.ByteReader is read byte by byte; other readers
// go through a one-byte buffer that escapes to the heap.
func ReadVarInt(r io.Reader) (int32, int, error) {
	return readVarInt(r, 5)
}

func readVarInt(r io.Reader, maxBytes int) (int32, int, error) {
	if br, ok := r.(io.ByteReader); ok {
		return decodeVarInt(br.ReadByte, maxBytes)
	}
	var buf [1]byte
	return decodeVarInt(func() (byte, error) {
		_, err := io.ReadFull(r, buf[:])
		return buf[0], err
	}, maxBytes)
}

// The count returned on error is the number of bytes consumed.
func decodeVarInt(next func() (byte, error), maxBytes int) (int32, int, error) {
	var result uint32
	for i := range maxBytes {
		b, err := next()
		if err != nil {
			return 0, i, err
		}
		result |= uint32(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			return int32(result), i + 1, nil
		}
	}
	return 0, maxBytes, ErrVarIntTooLong
}
