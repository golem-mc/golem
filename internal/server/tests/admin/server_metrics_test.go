package admin

import (
	"archive/zip"
	"github.com/golem-mc/golem/internal/server/servertest"
	"path/filepath"
	"slices"
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

func colored(text, color string) protocol.Text { return protocol.Text{Text: text, Color: color} }

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

// TestPaperCommandsAnswerAsPaper checks /tps and /mspt against the history:
// the TPS over each window counts its ticks over the time between their
// starts, and the tick times are Paper's mean, least and greatest.
func TestPaperCommandsAnswerAsPaper(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	layTickHistory(srv)

	// 1102 ticks over 60.05s, 5902 over 300.05s, 17902 over 900.05s.
	c.ChatCommand("tps")
	c.ExpectSystemText(protocol.Text{Extra: []protocol.Text{
		colored("TPS from last 1m, 5m, 15m: ", "gold"),
		{Extra: []protocol.Text{
			colored("18.4", "green"), {Text: ", "}, colored("19.7", "green"), {Text: ", "}, colored("19.9", "green"),
		}},
	}})

	c.ChatCommand("mspt")
	gray := func(s string) protocol.Text { return colored(s, "gray") }
	c.ExpectSystemText(protocol.Text{Text: "Server tick times ", Color: "gold", Extra: []protocol.Text{
		{Color: "yellow", Extra: []protocol.Text{
			{Text: "("}, gray("avg"), {Text: "/"}, gray("min"), {Text: "/"}, gray("max"), {Text: ")"},
		}},
		{Text: " from last 5s"}, gray(","), {Text: " 10s"}, gray(","), {Text: " 1m"}, colored(":", "yellow"),
	}})
	slash := protocol.Text{Text: "/"}
	sep := colored(", ", "yellow")
	c.ExpectSystemText(protocol.Text{Text: "◴ ", Color: "gold", Extra: []protocol.Text{{Color: "gray", Extra: []protocol.Text{
		// 5s: the 51 ticks of 50ms to 100ms.
		colored("75.0", "red"), slash, colored("50.0", "red"), slash, colored("100.0", "red"), sep,
		// 10s: the slow hundred and two fast ticks.
		colored("49.5", "yellow"), slash, colored("1.0", "green"), slash, colored("100.0", "red"), sep,
		// 1m: the slow hundred and 1002 fast ticks.
		colored("6.4", "green"), slash, colored("1.0", "green"), slash, colored("100.0", "red"),
	}}}})
	c.Disconnect()
}

// TestMetricsReportsTickDurationsAndTheRuntime checks /metrics: spark's
// min/median/95th/max over 10s and 1m, then the CPU, memory, GC and
// goroutine lines, whose numbers are the process's own.
func TestMetricsReportsTickDurationsAndTheRuntime(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	layTickHistory(srv)

	c.ChatCommand("metrics")
	c.ExpectSystemText(colored("Tick durations (min/med/95%ile/max ms) from last 10s, 1m:", "gold"))
	slash := colored("/", "gray")
	c.ExpectSystemText(protocol.Text{Text: " ", Extra: []protocol.Text{
		colored("1.0", "green"), slash, colored("50.0", "red"), slash, colored("95.0", "red"), slash, colored("100.0", "red"),
		{Text: ";  "},
		colored("1.0", "green"), slash, colored("2.0", "green"), slash, colored("45.0", "yellow"), slash, colored("100.0", "red"),
	}})
	c.ExpectSystemText(colored("CPU usage from last 10s, 1m, 15m:", "gold"))
	for _, label := range []string{"", "Memory: ", "GC: ", "Goroutines: "} {
		data := c.ExpectPlay(protocol.IDPlaySystemChat)
		got, ok := protocol.ReadText(protocol.NewReader(data))
		if !ok {
			t.Fatalf("unreadable line after the CPU header")
		}
		if label == "" {
			if n := len(got.Extra); n != 6 || got.Extra[5].Text != "  (process)" {
				t.Errorf("CPU line = %+v, want three figures and the process label", got)
			}
			continue
		}
		if len(got.Extra) != 2 || got.Extra[0].Text != label || got.Extra[0].Color != "gold" || got.Extra[1].Text == "" {
			t.Errorf("line = %+v, want %q and its figures", got, label)
		}
	}
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

// TestDebugSamplesSkipNonOperators: the subscription is kept, but nothing is
// sent to a player off the operator list, as vanilla's
// ServerPlayer.debugSubscriptions answers.
func TestDebugSamplesSkipNonOperators(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate")

	c.Send(protocol.IDPlayDebugSubscriptionRequest, func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutVarInt(protocol.DebugSubscriptionTickTime)
	})
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDebugSample))
	c.Disconnect()
}

// TestDebugSubscriptionAcceptsIdsAboveTickTime: the minecraft:debug_subscription
// registry holds more entries than the tick-time one, so a valid id above zero
// is kept instead of costing the player their connection.
// vanilla: DebugSubscriptions
func TestDebugSubscriptionAcceptsIdsAboveTickTime(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate")

	c.Send(protocol.IDPlayDebugSubscriptionRequest, func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutVarInt(1) // minecraft:bees, a registry id above the tick-time one
	})
	c.ExpectNoneWithinTicks(srv, 6, servertest.PacketIs(protocol.IDPlayDisconnect))
	if n := srv.PlayerCount(); n != 1 {
		t.Fatalf("player count %d after a valid subscription, want 1", n)
	}
	c.Disconnect()
}

// TestPerfStartStopSavesAReport checks /perf's refusals and that a started
// run stops with its stats and a zip under debug/profiling.
func TestPerfStartStopSavesAReport(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	dir := t.TempDir()
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		s.SetConfigFile(filepath.Join(dir, "server.properties"))
	})

	c.ChatCommand("perf stop")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.perf.notRunning"))
	c.ChatCommand("perf start")
	c.ExpectCommandFeedback("commands.perf.started")
	c.ChatCommand("perf start")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.perf.alreadyRunning"))
	c.ChatCommand("perf stop")
	if m := servertest.FeedbackComponent(t, c); m.Translate != "commands.perf.stopped" || len(m.With) != 3 {
		t.Fatalf("feedback = %+v, want commands.perf.stopped", m)
	}
	m := servertest.FeedbackComponent(t, c)
	if m.Translate != "commands.perf.reportSaved" || len(m.With) != 1 {
		t.Fatalf("feedback = %+v, want commands.perf.reportSaved", m)
	}
	zr, err := zip.OpenReader(filepath.Join(dir, filepath.FromSlash(m.With[0].Text)))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if !slices.Equal(names, []string{"summary.txt", "cpu.pprof"}) {
		t.Errorf("report files = %v", names)
	}
	c.Disconnect()
}
