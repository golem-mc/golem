package protocol

import (
	"encoding/binary"
	"fmt"
	"unicode/utf16"
)

// LegacyPing is the kind of pre-1.7 server list ping, which decides the form
// of the reply.
type LegacyPing int

const (
	// LegacyPingNone is a first byte sequence that is no legacy ping.
	LegacyPingNone LegacyPing = iota
	// LegacyPingV0 is a bare 0xFE, from before 1.4.
	LegacyPingV0
	// LegacyPingV1 is 0xFE 0x01, optionally followed by the MC|PingHost
	// plugin message, from 1.4 on.
	LegacyPingV1
)

// legacyPingChannel is the plugin message channel a 1.6 ping carries.
const legacyPingChannel = "MC|PingHost"

// ParseLegacyPing classifies the bytes a connection opened with, as far as they
// have arrived, and returns the advertised protocol (zero for the bare forms).
// vanilla: LegacyQueryHandler.channelRead
func ParseLegacyPing(in []byte) (LegacyPing, int) {
	if len(in) == 0 || in[0] != 0xFE {
		return LegacyPingNone, 0
	}
	rest := in[1:]
	if len(rest) == 0 {
		return LegacyPingV0, 0
	}
	if rest[0] != 1 {
		return LegacyPingNone, 0
	}
	rest = rest[1:]
	if len(rest) == 0 {
		return LegacyPingV1, 0
	}
	if version, ok := validPingHost(rest); ok {
		return LegacyPingV1, version
	}
	return LegacyPingNone, 0
}

// validPingHost validates the MC|PingHost packet and reads its protocol version.
// vanilla: LegacyQueryHandler.readCustomPayloadPacket
func validPingHost(b []byte) (int, bool) {
	if b[0] != 0xFA {
		return 0, false
	}
	channel, b, ok := readLegacyString(b[1:])
	if !ok || channel != legacyPingChannel || len(b) < 2 {
		return 0, false
	}
	size := int(binary.BigEndian.Uint16(b))
	b = b[2:]
	if len(b) != size || size < 1 {
		return 0, false
	}
	// 73 is the protocol version of 1.6.1, the first to send this packet.
	version := int(b[0])
	if version < 73 {
		return 0, false
	}
	_, b, ok = readLegacyString(b[1:])
	// The port is a signed int; vanilla rejects only values above 65535.
	return version, ok && len(b) == 4 && int32(binary.BigEndian.Uint32(b)) <= 65535
}

// readLegacyString reads a short char count and that many UTF-16BE units.
// vanilla: LegacyProtocolUtils.readLegacyString
func readLegacyString(b []byte) (s string, rest []byte, ok bool) {
	if len(b) < 2 {
		return "", nil, false
	}
	n := int(binary.BigEndian.Uint16(b))
	b = b[2:]
	if len(b) < n*2 {
		return "", nil, false
	}
	units := make([]uint16, n)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(units)), b[2*n:], true
}

// LegacyPingReply builds the 0xFF kick packet that answers a legacy ping:
// the MOTD, online count and max players, with the version 1 form adding the
// protocol and version name.
// vanilla: LegacyQueryHandler.createVersion0Response, createVersion1Response
func LegacyPingReply(kind LegacyPing, motd string, online, maxPlayers int) []byte {
	var body string
	if kind == LegacyPingV0 {
		body = fmt.Sprintf("%s§%d§%d", motd, online, maxPlayers)
	} else {
		// 127 is the protocol number vanilla reports to legacy clients, which
		// matches no real release.
		body = fmt.Sprintf("§1\x00%d\x00%s\x00%s\x00%d\x00%d", 127, GameVersion, motd, online, maxPlayers)
	}
	units := utf16.Encode([]rune(body))
	out := make([]byte, 3, 3+2*len(units))
	out[0] = 0xFF
	binary.BigEndian.PutUint16(out[1:], uint16(len(units)))
	for _, u := range units {
		out = binary.BigEndian.AppendUint16(out, u)
	}
	return out
}
