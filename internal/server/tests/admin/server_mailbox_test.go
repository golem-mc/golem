package admin

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestABurstOfTheWorldsDoingDoesNotDropTheClient kills a crowd of mobs with
// one command: every death is a sound, an event, a removal and a drop for
// the watching client, hundreds of packets queued in one go. The vanilla
// server queues them all and leaves a stalled client to the keep-alive, so
// the client must still be connected afterwards - the burst once overran a
// 256-packet queue and the server hung up on a player who had only typed a
// /kill.
func TestABurstOfTheWorldsDoingDoesNotDropTheClient(t *testing.T) {
	t.Parallel()
	cfg := servertest.SpawningTestConfig()
	srv, c := servertest.Join(t, cfg, "Op", servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID:  protocol.OfflineUUID("Op").String(),
		Name:  "Op",
		Level: 4,
	}))

	for i := range 120 {
		srv.SpawnMob("minecraft:cow", float64(i%12)+2.5, 64, float64(i/12)+2.5)
	}
	c.Settle()
	c.ChatCommand("kill @e[type=minecraft:cow]")

	// Still connected: the ping's answer comes back after the whole burst.
	c.Settle()
	c.Settle()
	c.Disconnect()
}

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
