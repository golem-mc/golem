package protocol

import (
	"bytes"
	"errors"
	"testing"
)

func TestReadPacketLengthPrefix(t *testing.T) {
	t.Run("three bytes", func(t *testing.T) {
		const packetID = 0x12
		payload := make([]byte, (1<<14)-1)
		frame := lengthPrefixed(1<<14, append([]byte{packetID}, payload...))
		conn := NewConn(framedConn{r: bytes.NewReader(frame)})

		id, data, err := conn.ReadPacket()
		if err != nil || id != packetID || !bytes.Equal(data, payload) {
			t.Fatalf("ReadPacket = ID %d, %d payload bytes, %v; want ID %d and the original payload", id, len(data), err, packetID)
		}
	})

	t.Run("continued third byte", func(t *testing.T) {
		prefix := []byte{0x80, 0x80, 0x80}
		conn := NewConn(framedConn{r: bytes.NewReader(prefix)})

		if _, _, err := conn.ReadPacket(); !errors.Is(err, ErrVarIntTooLong) {
			t.Fatalf("ReadPacket = %v, want a length error as soon as the third byte continues", err)
		}
	})
}
