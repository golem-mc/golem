package protocol

import (
	"errors"
	"io"
)

var ErrVarIntTooLong = errors.New("VarInt is too long")

// ReadVarInt reads a VarInt from a stream, returning the decoded value and the
// number of bytes read. Fields inside a packet are read with Reader instead;
// this is for the packet length, which precedes the payload. An
// io.ByteReader is read byte by byte; other readers go through
// readVarIntFrom, whose one-byte buffer escapes to the heap.
func ReadVarInt(r io.Reader) (int32, int, error) {
	br, ok := r.(io.ByteReader)
	if !ok {
		return readVarIntFrom(r)
	}
	var result uint32
	for i := range 5 {
		b, err := br.ReadByte()
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

func readVarIntFrom(r io.Reader) (int32, int, error) {
	var result uint32
	var buf [1]byte
	for i := range 5 {
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return 0, i, err
		}
		result |= uint32(buf[0]&0x7F) << (7 * i)
		if buf[0]&0x80 == 0 {
			return int32(result), i + 1, nil
		}
	}
	return 0, 5, ErrVarIntTooLong
}
