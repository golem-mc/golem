package protocol

import (
	"encoding/binary"
	"strconv"
	"unicode/utf8"
)

// The GS4 (GameSpy4) query protocol on UDP query.port: each request and
// answer starts with the type byte and a four-byte session ID echoed back,
// then its fields, strings NUL-terminated.
// vanilla: QueryThreadGs4
const (
	// QueryHandshake requests a challenge token, and carries nothing
	// after the session ID.
	QueryHandshake byte = 0x09
	// QueryStat requests the server's state, and must carry the token a
	// handshake handed the client as a big-endian int.
	QueryStat byte = 0x00
)

// QueryRequest is one query datagram: the handshake that mints a challenge
// token, or one of the two stats that spend it.
type QueryRequest struct {
	Type      byte
	Session   [4]byte
	Challenge int32
	// Full is a stat whose datagram is fifteen bytes long: the four bytes
	// after the token, whatever they hold, ask for the whole rule set and
	// the player names.
	Full bool
}

// ParseQueryRequest parses one datagram; false for one under seven bytes,
// without the magic, or of an unknown type. A stat needs eleven bytes, its
// token included.
func ParseQueryRequest(data []byte) (QueryRequest, bool) {
	if len(data) < 7 || data[0] != 0xFE || data[1] != 0xFD {
		return QueryRequest{}, false
	}
	var q QueryRequest
	q.Type = data[2]
	copy(q.Session[:], data[3:7])
	switch q.Type {
	case QueryHandshake:
		return q, true
	case QueryStat:
		// vanilla: PktUtils.intFromNetworkByteArray, which yields 0 for a
		// short read; a token of 0 is one roll in 16 million.
		if len(data) < 11 {
			return QueryRequest{}, false
		}
		q.Challenge = int32(binary.BigEndian.Uint32(data[7:11]))
		q.Full = len(data) == 15
		return q, true
	default:
		return QueryRequest{}, false
	}
}

// QueryStatus is the server state a stat answer carries.
type QueryStatus struct {
	// MOTD is the motd of the server.properties, as the server list puts
	// it.
	MOTD string
	// Map is the name of the world, the level-name it plays in.
	Map    string
	Online int
	Max    int
	// Port is the game's TCP port, not the query port the answer travels
	// on.
	Port   int
	HostIP string
	// Players holds the online players' names, in the player list's order.
	// A full stat alone carries them.
	Players []string
}

// QueryHandshakeReply returns the type byte, the session as UTF-8 and the
// challenge in decimal, NUL-terminated: vanilla's "\t<session><token>\0",
// the tab being the type byte.
func QueryHandshakeReply(session [4]byte, challenge int32) []byte {
	b := make([]byte, 0, 16)
	b = append(b, QueryHandshake)
	b = appendQuerySession(b, session)
	b = strconv.AppendInt(b, int64(challenge), 10)
	return append(b, 0)
}

// QueryBasicStatReply returns a basic stat's answer: the MOTD, game type,
// map, player counts, port and host address, each string NUL-terminated and
// the port alone little endian.
func QueryBasicStatReply(session [4]byte, status QueryStatus) []byte {
	b := make([]byte, 0, 80)
	b = append(b, QueryStat)
	b = appendQuerySession(b, session)
	b = appendQueryText(b, status.MOTD)
	b = appendQueryText(b, "SMP")
	b = appendQueryText(b, status.Map)
	b = appendQueryNumber(b, status.Online)
	b = appendQueryNumber(b, status.Max)
	b = binary.LittleEndian.AppendUint16(b, uint16(status.Port))
	return appendQueryText(b, status.HostIP)
}

// QueryFullStatReply returns the basic stat's values as rules, then the
// online players' names. Game type, game id and the empty plugin list are
// a dedicated vanilla server's. Built fresh each time, where vanilla caches
// one for five seconds and can lag.
func QueryFullStatReply(session [4]byte, status QueryStatus) []byte {
	b := make([]byte, 0, 128)
	b = append(b, QueryStat)
	b = appendQuerySession(b, session)
	b = appendQueryText(b, "splitnum")
	b = append(b, 0x80, 0x00)
	for _, rule := range [][2]string{
		{"hostname", status.MOTD},
		{"gametype", "SMP"},
		{"game_id", "MINECRAFT"},
		{"version", GameVersion},
		{"plugins", ""},
		{"map", status.Map},
		{"numplayers", strconv.Itoa(status.Online)},
		{"maxplayers", strconv.Itoa(status.Max)},
		{"hostport", strconv.Itoa(status.Port)},
		{"hostip", status.HostIP},
	} {
		b = appendQueryText(b, rule[0])
		b = appendQueryText(b, rule[1])
	}
	b = append(b, 0) // the empty key that closes the rule list
	b = append(b, 0x01)
	b = appendQueryText(b, "player_")
	b = append(b, 0)
	for _, name := range status.Players {
		b = appendQueryText(b, name)
	}
	return append(b, 0)
}

// appendQuerySession appends a session through UTF-8: vanilla forms it with
// new String(ident, UTF_8), so a byte that does not begin a valid sequence
// becomes the replacement character. The session is echoed in every answer.
func appendQuerySession(b []byte, session [4]byte) []byte {
	for _, r := range string(session[:]) {
		b = utf8.AppendRune(b, r)
	}
	return b
}

// appendQueryText appends a NUL-terminated string, the form the query
// protocol carries every multi-byte field in. Encoding is UTF-8, which the
// server already answers the rest of the protocol in.
func appendQueryText(b []byte, text string) []byte {
	return append(append(b, text...), 0)
}

// appendQueryNumber appends a number as one NUL-terminated decimal string.
func appendQueryNumber(b []byte, n int) []byte {
	return appendQueryText(b, strconv.Itoa(n))
}
