package admin

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// layTickHistory stops the main loop and lays down a history the numbers
// can be worked out from: 16 minutes of 2ms ticks at 20 a second, then ten
// seconds at 10 a second whose ticks take 1ms, 2ms, ... 100ms. It is laid an
// hour ahead, so the windows hold none of the loop's own ticks.
func layTickHistory(srv *server.Server) {
	srv.StopTick()
	at := time.Now().Add(time.Hour)
	for range 16 * 60 * 20 {
		srv.RecordTick(at, at.Add(2*time.Millisecond))
		at = at.Add(50 * time.Millisecond)
	}
	for j := range 100 {
		srv.RecordTick(at, at.Add(time.Duration(j+1)*time.Millisecond))
		at = at.Add(100 * time.Millisecond)
	}
}

// TestTickQueryAnswersAsVanilla checks /tick query's three lines: lagging,
// since the last 100 ticks took 50.5ms on average; the rates; and the
// percentiles vanilla indexes out of the sorted window, with its size.
func TestTickQueryAnswersAsVanilla(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	layTickHistory(srv)

	c.ChatCommand("tick query")
	c.ExpectCommandFeedback("commands.tick.status.lagging")
	c.ExpectCommandFeedback("commands.tick.query.rate.running", "20.0", "50.5", "50.0")
	n := int64(100)
	c.ExpectCommandFeedbackText("commands.tick.query.percentiles",
		protocol.Text{Text: "51.0"}, protocol.Text{Text: "96.0"}, protocol.Text{Text: "100.0"},
		protocol.Text{Number: &n})
	c.Disconnect()
}

// TestPerformanceCommandsNeedAnOperator: a player off the operator list is
// offered none of them and is refused /tick.
func TestPerformanceCommandsNeedAnOperator(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, "Testificate")

	for _, cmd := range []string{"tick query", "tps", "mspt", "metrics"} {
		c.ChatCommand(cmd)
		data := c.ExpectPlay(protocol.IDPlaySystemChat)
		got, _ := protocol.ReadText(protocol.NewReader(data))
		if got.Translate != "command.unknown.command" && (len(got.Extra) == 0 || got.Extra[0].Translate != "command.unknown.command") {
			t.Errorf("/%s by a non-operator = %+v, want unknown command", cmd, got)
		}
		c.Settle()
	}
	c.Disconnect()
}

// TestDebugSamplesGoToSubscribedOperators subscribes to the tick-time
// samples behind the F3 TPS chart: an operator gets one a tick, laid out as
// vanilla's ClientboundDebugSamplePacket, and a player off the list none.
func TestDebugSamplesGoToSubscribedOperators(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, op := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	subscribe := func(c *servertest.Client, ids ...int32) {
		c.Send(protocol.IDPlayDebugSubscriptionRequest, func(b *protocol.Buffer) {
			b.PutVarInt(int32(len(ids)))
			for _, id := range ids {
				b.PutVarInt(id)
			}
		})
	}
	subscribe(op, protocol.DebugSubscriptionTickTime)

	var data []byte
	for id, d := range op.Packets(servertest.ExpectTimeout) {
		if id == protocol.IDPlayDebugSample {
			data = d
			break
		}
	}
	if data == nil {
		t.Fatalf("no debug sample within %s", servertest.ExpectTimeout)
	}
	r := protocol.NewReader(data)
	if n := r.ReadVarInt(); n != 4 {
		t.Fatalf("sample has %d longs, want vanilla's 4 TpsDebugDimensions", n)
	}
	var sample [4]int64
	for i := range sample {
		sample[i] = r.ReadI64()
	}
	sampleType := r.ReadVarInt()
	if err := r.Err(); err != nil || r.Remaining() != 0 || sampleType != protocol.DebugSampleTickTime {
		t.Fatalf("sample type %d, err %v, %d bytes left", sampleType, err, r.Remaining())
	}
	full, tick, tasks, idle := sample[0], sample[1], sample[2], sample[3]
	if full <= 0 || tick < 0 || tasks != 0 || idle != full-tick {
		t.Errorf("sample = %v, want the full cycle split into the tick and the idle rest", sample)
	}

	// Unsubscribing stops them, once a sample the loop may have been
	// sending as the request landed is through.
	subscribe(op)
	op.Settle()
	op.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
	op.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDebugSample))
	op.Disconnect()
}
