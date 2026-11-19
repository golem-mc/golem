package protocol

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// goldenHandshake is a handshake's answer: the type byte, the session as it
// arrived, the token in decimal digits, and the closing NUL.
func goldenHandshake(session [4]byte, token int32) []byte {
	return append(append([]byte{0x09}, session[:]...), append([]byte("6499185"), 0)...)
}

var goldenStat = QueryStatus{
	MOTD:    "A Golden Server",
	Map:     "world",
	Online:  3,
	Max:     20,
	Port:    25565,
	HostIP:  "10.0.0.1",
	Players: []string{"Erum", "Ibex"},
}

func TestParseQueryRequest(t *testing.T) {
	t.Parallel()

	handshake := []byte{0xFE, 0xFD, 0x09, 1, 2, 3, 4}
	req, ok := ParseQueryRequest(handshake)
	if !ok {
		t.Fatal("a handshake was not parsed")
	}
	if req.Type != QueryHandshake {
		t.Errorf("handshake type = %#x, want %#x", req.Type, QueryHandshake)
	}
	if req.Session != [4]byte{1, 2, 3, 4} {
		t.Errorf("handshake session = %v, want [1 2 3 4]", req.Session)
	}
	if _, ok := ParseQueryRequest(append(handshake, 9, 9)); !ok {
		t.Error("a handshake with bytes after its session was refused")
	}

	token := [4]byte{0x01, 0x02, 0x03, 0x04}
	stat := append([]byte{0xFE, 0xFD, 0x00}, token[:]...) // the session
	stat = append(stat, token[:]...)                      // the challenge
	for length, full := range map[int]bool{
		11: false, 12: false, 14: false,
		15: true,
		16: false, 20: false,
	} {
		datagram := stat
		for range length - 11 {
			datagram = append(datagram, 0)
		}
		req, ok := ParseQueryRequest(datagram)
		if !ok {
			t.Fatalf("a %d-byte stat was refused", length)
		}
		if req.Full != full {
			t.Errorf("a %d-byte stat is full = %v, want %v", length, req.Full, full)
		}
		if req.Challenge != int32(binary.BigEndian.Uint32(token[:])) {
			t.Errorf("stat challenge = %#x, want %#x", req.Challenge, token)
		}
	}

	for _, bad := range []string{
		"\x00\x00\x00",                     // the wrong magic
		"\xFE\xFD",                         // no type
		"\xFE\xFD\x09\x01",                 // a handshake with no session
		"\xFE\xFD\x00\x01\x02\x03\x04\x01", // a stat with no token
		"\xFE\xFD\x2A\x01\x02\x03\x04\x05", // a type the server does not speak
	} {
		if _, ok := ParseQueryRequest([]byte(bad)); ok {
			t.Errorf("datagram %q was parsed, want refused", bad)
		}
	}
}

func TestQueryHandshakeReply(t *testing.T) {
	t.Parallel()
	got := QueryHandshakeReply([4]byte{0x0F, 0x12, 0x34, 0x56}, 6499185)
	if want := goldenHandshake([4]byte{0x0F, 0x12, 0x34, 0x56}, 6499185); !bytes.Equal(got, want) {
		t.Errorf("handshake reply = %q, want %q", got, want)
	}
}

// TestQueryReplyReplacesAnInvalidSession checks the echo: the session is
// decoded as UTF-8 and encoded back, so a byte that is not valid UTF-8 comes
// back as the replacement character. vanilla: QueryThreadGs4.
func TestQueryReplyReplacesAnInvalidSession(t *testing.T) {
	t.Parallel()
	session := [4]byte{0x80, 0xFF, 0x01, 0x02}
	want := []byte{0xEF, 0xBF, 0xBD, 0xEF, 0xBF, 0xBD, 0x01, 0x02}

	for name, reply := range map[string][]byte{
		"handshake": QueryHandshakeReply(session, 6499185),
		"basic":     QueryBasicStatReply(session, goldenStat),
		"full":      QueryFullStatReply(session, goldenStat),
	} {
		if got := reply[1:9]; !bytes.Equal(got, want) {
			t.Errorf("%s reply session = % X, want % X", name, got, want)
		}
	}
}

func TestQueryBasicStatReply(t *testing.T) {
	t.Parallel()
	got := QueryBasicStatReply([4]byte{4, 3, 2, 1}, goldenStat)
	want := []byte{0x00, 4, 3, 2, 1}
	want = append(want, "A Golden Server\x00SMP\x00world\x003\x0020\x00"...)
	want = append(want, 25565&0xFF, 25565>>8) // the port alone is little endian
	want = append(want, "10.0.0.1\x00"...)
	if !bytes.Equal(got, want) {
		t.Errorf("basic stat reply = %q, want %q", got, want)
	}
}

func TestQueryFullStatReply(t *testing.T) {
	t.Parallel()
	got := QueryFullStatReply([4]byte{4, 3, 2, 1}, goldenStat)
	want := []byte{0x00, 4, 3, 2, 1}
	want = append(want, "splitnum\x00\x80\x00"...)
	want = append(want, "hostname\x00A Golden Server\x00"...)
	want = append(want, "gametype\x00SMP\x00"...)
	want = append(want, "game_id\x00MINECRAFT\x00"...)
	want = append(want, "version\x00"+GameVersion+"\x00"...)
	want = append(want, "plugins\x00\x00"...)
	want = append(want, "map\x00world\x00"...)
	want = append(want, "numplayers\x003\x00"...)
	want = append(want, "maxplayers\x0020\x00"...)
	want = append(want, "hostport\x0025565\x00"...)
	want = append(want, "hostip\x0010.0.0.1\x00"...)
	want = append(want, 0x00, 0x01)
	want = append(want, "player_\x00\x00"...)
	want = append(want, "Erum\x00Ibex\x00"...)
	want = append(want, 0)
	if !bytes.Equal(got, want) {
		t.Errorf("full stat reply = %q, want %q", got, want)
	}
}
