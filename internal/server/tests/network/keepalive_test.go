package network

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// settleUnanswered is settle that leaves keep-alives unanswered: record sees
// every packet before the answer to the ping, the keep-alives included.
func settleUnanswered(c *servertest.Client, record func(id int32, data []byte)) {
	c.T.Helper()
	c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(servertest.SettlePing) })
	deadline := time.Now().Add(servertest.ExpectTimeout)
	for {
		id, data, err := c.ReadBy(deadline)
		if err != nil {
			c.T.Fatalf("waiting for the ping's answer: %v", err)
		}
		switch {
		case servertest.IsPingAnswer(id, data, servertest.SettlePing):
			return
		case id != protocol.IDPlayUpdateTime:
			record(id, data)
		}
	}
}

func disconnectTimeoutText() []byte {
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: "disconnect.timeout"})
	return want.Bytes()
}

// TestAnUnansweredKeepAliveIsDroppedWithATimeoutMessage leaves a client's
// keep-alives unanswered - it reads what the server sends, and sends back
// nothing - and checks that the timeout drops it: the Disconnect carries
// vanilla's timeout reason, and comes on the tick the timeout is reached
// after the unanswered ping was sent (vanilla drops at >= 15000 ms), not one
// tick later.
func TestAnUnansweredKeepAliveIsDroppedWithATimeoutMessage(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate")
	servertest.HoldKeepAlive(t, srv, servertest.KeepAliveWaitTicks)

	// Each tick is followed by a settle that records what it sent and
	// answers nothing, so the tick that sends the ping and the tick that
	// drops are told apart.
	var pingTick uint64
	servertest.RunTicks(t, srv, 1)
	settleUnanswered(c, func(id int32, _ []byte) {
		if id == protocol.IDPlayKeepAlive {
			pingTick = srv.TicksRun()
		}
	})
	if pingTick == 0 {
		t.Fatal("the server sent no ping on the first tick of the interval")
	}
	for srv.TicksRun() < pingTick+servertest.KeepAliveWaitTicks-1 {
		servertest.RunTicks(t, srv, 1)
		settleUnanswered(c, func(id int32, _ []byte) {
			if id == protocol.IDPlayDisconnect {
				t.Fatalf("dropped %d ticks after the ping, want it kept for the timeout's %d", srv.TicksRun()-pingTick, servertest.KeepAliveWaitTicks)
			}
		})
	}

	servertest.RunTicks(t, srv, 1)
	for {
		packetID, data, err := c.ReadBy(time.Now().Add(servertest.ExpectTimeout))
		if err != nil {
			t.Fatalf("the tick reaching the timeout brought no Disconnect: %v", err)
		}
		if packetID != protocol.IDPlayDisconnect {
			continue
		}
		if want := disconnectTimeoutText(); !bytes.Equal(data, want) {
			t.Errorf("disconnect reason = % X, want % X", data, want)
		}
		break
	}
	// One Disconnect, then the connection ends: nothing more is queued for a
	// client already being dropped.
	for {
		packetID, _, err := c.ReadBy(time.Now().Add(servertest.ExpectTimeout))
		if err != nil {
			break
		}
		if packetID == protocol.IDPlayDisconnect {
			t.Fatal("a second Disconnect followed the first")
		}
	}
	c.AwaitServed()
}

// TestAWrongKeepAliveIsDroppedAtOnce answers the server's ping with another
// id and checks that the client is dropped at once with vanilla's timeout
// reason, rather than at the ping's timeout. vanilla:
// ServerCommonPacketListenerImpl.handleKeepAlive disconnects on any id that is
// not the pending one. The loop is held, so no tick runs between the wrong
// answer and the Disconnect: a server that only ignored the answer could not
// produce it.
func TestAWrongKeepAliveIsDroppedAtOnce(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate")
	servertest.HoldKeepAlive(t, srv, 10*20)

	servertest.RunTicks(t, srv, 1)
	var ping []byte
	settleUnanswered(c, func(id int32, data []byte) {
		if id == protocol.IDPlayKeepAlive {
			ping = data
		}
	})
	if ping == nil {
		t.Fatal("the first tick brought no keep-alive")
	}
	wrong := protocol.NewReader(ping).ReadI64() + 1
	c.Send(protocol.IDPlayKeepAliveIn, func(b *protocol.Buffer) { b.PutI64(wrong) })

	ticks := srv.TicksRun()
	for {
		packetID, data, err := c.ReadBy(time.Now().Add(servertest.ExpectTimeout))
		if err != nil {
			t.Fatalf("the wrong answer brought no Disconnect: %v", err)
		}
		if packetID != protocol.IDPlayDisconnect {
			continue
		}
		if want := disconnectTimeoutText(); !bytes.Equal(data, want) {
			t.Errorf("disconnect reason = % X, want % X", data, want)
		}
		if got := srv.TicksRun(); got != ticks {
			t.Errorf("%d ticks ran before the Disconnect, want none", got-ticks)
		}
		c.AwaitServed()
		return
	}
}

// TestAConfigurationClientIsPingedAndDroppedWhenItStopsAnswering leaves a
// client in the configuration phase - it never reports the known packs, so it
// never leaves - and checks that it is pinged on the configuration keep-alive
// and, answering nothing, dropped with vanilla's timeout reason. vanilla:
// ServerConfigurationPacketListenerImpl.tick calls keepConnectionAlive, the
// same exchange the play listener runs. The exchange runs on the held loop's
// clock, so the ping comes with the first tick and the drop with the tick
// the timeout is reached.
func TestAConfigurationClientIsPingedAndDroppedWhenItStopsAnswering(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Dial(t, cfg)
	servertest.HoldKeepAlive(t, srv, servertest.KeepAliveWaitTicks)
	c.Login("Testificate")

	// The configuration phase has begun; the client stops here.
	c.Expect(protocol.IDConfigPluginMessage)
	c.Expect(protocol.IDKnownPacks)

	servertest.RunTicks(t, srv, 1)
	c.Expect(protocol.IDConfigKeepAlive)
	servertest.RunTicks(t, srv, servertest.KeepAliveWaitTicks)
	data := c.Expect(protocol.IDConfigDisconnect)
	if want := disconnectTimeoutText(); !bytes.Equal(data, want) {
		t.Errorf("disconnect reason = % X, want % X", data, want)
	}
	c.AwaitServed()
}

// configFrame frames a serverbound configuration packet the way the
// compression the login turned on wants it: a zero uncompressed size then the
// packet, the form the packet takes below the threshold. The tests that split
// a packet across a keep-alive event build it so they can write the halves by
// hand.
func configFrame(packetID int32, fill func(*protocol.Buffer)) []byte {
	var body protocol.Buffer
	body.PutVarInt(packetID)
	if fill != nil {
		fill(&body)
	}
	var frame protocol.Buffer
	frame.PutVarInt(0)
	frame.PutRaw(body.Bytes())
	var out protocol.Buffer
	out.PutVarInt(int32(frame.Len()))
	out.PutRaw(frame.Bytes())
	return out.Bytes()
}

// TestAPartlySentConfigurationPacketSurvivesAPing sends a known-packs packet
// in two halves with a configuration keep-alive due in between and checks
// that the server still parses it: a ping sent while a packet is still
// arriving must not cut the read in half.
func TestAPartlySentConfigurationPacketSurvivesAPing(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Dial(t, cfg)
	servertest.HoldKeepAlive(t, srv, 5*20)
	c.Login("Testificate")

	// The configuration phase has begun; the server's own brand and
	// known-packs packets come before the client's answer under test.
	c.Expect(protocol.IDConfigPluginMessage)
	c.Expect(protocol.IDKnownPacks)

	// The answer, sent in two halves across more than one ping interval.
	frame := configFrame(protocol.IDKnownPacksIn, func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutString("minecraft")
		b.PutString("core")
		b.PutString(registry.Version)
	})
	half := len(frame) / 2
	if _, err := c.Raw.Write(frame[:half]); err != nil {
		t.Fatalf("write first half: %v", err)
	}
	// A ping falls due while the packet is still half sent.
	servertest.RunTicks(t, srv, 3)
	c.Expect(protocol.IDConfigKeepAlive)
	if _, err := c.Raw.Write(frame[half:]); err != nil {
		t.Fatalf("write second half: %v", err)
	}

	// The server parsed the packet and sent the feature flags ahead of the
	// registries, stepping over the pings it sent meanwhile.
	for {
		packetID, _, err := c.ReadBy(time.Now().Add(10 * time.Second))
		if err != nil {
			t.Fatalf("read after the two halves: %v", err)
		}
		if packetID == protocol.IDConfigKeepAlive {
			continue
		}
		if packetID != protocol.IDFeatureFlags {
			t.Fatalf("got packet 0x%02X after the two halves, want feature flags", packetID)
		}
		break
	}
}
