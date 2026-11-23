package protocol

import (
	"errors"
	"io"
)

var ErrVarIntTooLong = errors.New("VarInt is too long")

// ReadVarInt reads a VarInt from a stream, returning the decoded value and the
// number of bytes read. Fields inside a packet are read with Reader instead;
// this is for the packet length, which precedes the payload. An
// io.ByteReader is read byte by byte; other readers go through a one-byte
// buffer that escapes to the heap.
func ReadVarInt(r io.Reader) (int32, int, error) {
	if br, ok := r.(io.ByteReader); ok {
		return readVarInt(br.ReadByte)
	}
	var buf [1]byte
	return readVarInt(func() (byte, error) {
		_, err := io.ReadFull(r, buf[:])
		return buf[0], err
	})
}

// readVarInt decodes at most five bytes from next; the count returned on error
// is the bytes consumed before it.
func readVarInt(next func() (byte, error)) (int32, int, error) {
	var result uint32
	for i := range 5 {
		b, err := next()
		if err != nil {
			return 0, i, err
		}
		result |= uint32(b&0x7F) << (7 * i)
		if b&0x80 == 0 {
			return int32(result), i + 1, nil
		}
	}
	return 0, 5, ErrVarIntTooLong
}
