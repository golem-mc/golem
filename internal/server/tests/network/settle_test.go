package network

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func TestSettleReadsPastAnEarlierPingAnswer(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Testificate")

	// The server queues the probe's answer as it reads the probe, ahead of
	// the chat line behind it, which it relays back through the mailbox.
	c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(0x0BAD) })
	c.Chat("hello")

	var sawChat bool
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayPlayerChat {
			sawChat = true
		}
	})
	if !sawChat {
		t.Error("the settle ended at the probe's answer, before the chat line behind it")
	}
}
