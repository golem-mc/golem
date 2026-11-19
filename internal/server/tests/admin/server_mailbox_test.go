package admin

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestEveryPingIsAnsweredWithItsOwnPayload sends pings back to back, each
// followed by another packet, and checks every answer echoes its own
// payload. The answer is queued and written later on another goroutine, so
// a payload still sharing the connection's read buffer would go out as the
// bytes of whatever packet was read after it.
func TestEveryPingIsAnsweredWithItsOwnPayload(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Pinger")
	c.MarkLoaded()

	const pings = 200
	for i := range int64(pings) {
		c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(0x7E57_0000 + i) })
		c.Send(protocol.IDPlayRotation, func(b *protocol.Buffer) {
			b.PutF32(-1)
			b.PutF32(-1)
			b.PutU8(0x01) // on the ground
		})
	}

	next := int64(0)
	c.Pump(servertest.ExpectTimeout, func(id int32) bool { return id != protocol.IDPlayPingResponse }, func(_ int32, data []byte) bool {
		c.T.Helper()
		if got, want := protocol.NewReader(data).ReadI64(), 0x7E57_0000+next; got != want {
			t.Fatalf("ping response %d echoed %#x, want %#x", next, got, want)
		}
		next++
		return next == pings
	})
	if next != pings {
		t.Fatalf("%d of %d pings answered within %s", next, pings, servertest.ExpectTimeout)
	}
}
