package network

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestStatusPingReadsOneLong pins the ping request's body as exactly the one
// long it carries: a body cut short or one with bytes past the long fails
// the decode, and the connection closes without a pong.
// vanilla: ServerboundPingRequestPacket
func TestStatusPingReadsOneLong(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body []byte
	}{
		{"short", []byte{1, 2, 3, 4}},
		{"long", []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, c := servertest.Dial(t, config.Default())
			c.Handshake(protocol.NextStateStatus)
			c.Send(protocol.IDStatusPing, func(b *protocol.Buffer) { b.PutRaw(tc.body) })
			if id, _, err := c.Read(); err == nil {
				t.Fatalf("got packet 0x%02X for a %d-byte ping, want the connection closed", id, len(tc.body))
			}
		})
	}
}
