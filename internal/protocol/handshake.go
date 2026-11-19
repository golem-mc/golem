package protocol

import "fmt"

// Handshake is the first packet a client sends. It announces the protocol
// version it speaks and which state to continue in.
type Handshake struct {
	ProtocolVersion int32
	ServerAddress   string
	ServerPort      uint16
	NextState       int32
}

// Next states requested by a handshake.
const (
	NextStateStatus   = 1
	NextStateLogin    = 2
	NextStateTransfer = 3
)

// ReadHandshake parses a handshake from a packet payload.
func ReadHandshake(data []byte) (Handshake, error) {
	r := NewReader(data)
	h := Handshake{
		ProtocolVersion: r.ReadVarInt(),
		ServerAddress:   r.ReadString(255),
		ServerPort:      r.ReadU16(),
		NextState:       r.ReadVarInt(),
	}
	if err := r.Err(); err != nil {
		return h, fmt.Errorf("handshake: %w", err)
	}
	return h, nil
}
