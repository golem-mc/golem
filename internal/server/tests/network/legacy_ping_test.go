package network

import (
	"encoding/binary"
	"github.com/golem-mc/golem/internal/server/servertest"
	"io"
	"net"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

func legacyString(s string) []byte {
	units := utf16.Encode([]rune(s))
	out := binary.BigEndian.AppendUint16(nil, uint16(len(units)))
	for _, u := range units {
		out = binary.BigEndian.AppendUint16(out, u)
	}
	return out
}

// pingHost is the 1.6 ping from a client of the given protocol version.
func pingHost(version byte) []byte {
	payload := []byte{version}
	payload = append(payload, legacyString("localhost")...)
	payload = binary.BigEndian.AppendUint32(payload, 25565)
	out := []byte{0xFE, 0x01, 0xFA}
	out = append(out, legacyString("MC|PingHost")...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(payload)))
	return append(out, payload...)
}

// legacyReply sends request and returns the kick string of the 0xFF answer,
// or false when the server closed without one.
func legacyReply(t *testing.T, request []byte) (string, bool) {
	t.Helper()
	cfg := config.Default()
	cfg.MOTD = "Hello Legacy"
	return legacyReplyWith(t, cfg, request)
}

func legacyReplyWith(t *testing.T, cfg *config.Config, request []byte) (string, bool) {
	t.Helper()
	_, c := servertest.Dial(t, cfg)
	if _, err := c.Raw.Write(request); err != nil {
		t.Fatal(err)
	}
	// Ends the stream so a request the server does not answer fails at EOF
	// instead of waiting out the handshake timeout.
	if tc, ok := c.Raw.(*net.TCPConn); ok {
		_ = tc.CloseWrite()
	}
	data, err := io.ReadAll(c.Raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		return "", false
	}
	if data[0] != 0xFF {
		t.Fatalf("first byte = 0x%02X, want 0xFF", data[0])
	}
	n := int(binary.BigEndian.Uint16(data[1:]))
	if len(data) != 3+2*n {
		t.Fatalf("length %d does not match %d chars", len(data), n)
	}
	units := make([]uint16, n)
	for i := range units {
		units[i] = binary.BigEndian.Uint16(data[3+2*i:])
	}
	return string(utf16.Decode(units)), true
}

func TestLegacyPingV0(t *testing.T) {
	t.Parallel()
	got, ok := legacyReply(t, []byte{0xFE})
	if !ok {
		t.Fatal("no reply")
	}
	if want := "Hello Legacy§0§20"; got != want {
		t.Errorf("reply = %q, want %q", got, want)
	}
}

func TestLegacyPingV1(t *testing.T) {
	t.Parallel()
	for name, req := range map[string][]byte{"bare": {0xFE, 0x01}, "ping host": pingHost(74)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, ok := legacyReply(t, req)
			if !ok {
				t.Fatal("no reply")
			}
			want := "§1\x00127\x00" + protocol.GameVersion + "\x00Hello Legacy\x000\x0020"
			if strings.Join(strings.Split(got, "\x00"), "|") != strings.Join(strings.Split(want, "\x00"), "|") {
				t.Errorf("reply = %q, want %q", got, want)
			}
		})
	}
}

func TestLegacyPingMalformedClosed(t *testing.T) {
	t.Parallel()
	for name, req := range map[string][]byte{"second byte": {0xFE, 0x02}, "old protocol": pingHost(72)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got, ok := legacyReply(t, req); ok {
				t.Errorf("got reply %q, want the connection closed", got)
			}
		})
	}
}

func TestLegacyPingIgnoredWithStatusOff(t *testing.T) {
	t.Parallel()
	for name, req := range map[string][]byte{"bare": {0xFE}, "ping host": pingHost(74)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.EnableStatus = false
			if got, ok := legacyReplyWith(t, cfg, req); ok {
				t.Errorf("got reply %q, want the connection closed", got)
			}
		})
	}
}
