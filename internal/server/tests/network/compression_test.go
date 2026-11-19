package network

import (
	"bytes"
	"compress/zlib"
	"github.com/golem-mc/golem/internal/server/servertest"
	"io"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestCompressionOffJoinsWithoutSetCompression checks the negative end of
// network-compression-threshold: the login is answered with Login Success
// alone — no Set Compression packet in front of it, and its frame bare of the
// size prefix the compressed format puts there — and the join that follows,
// registries to chunks, travels the plain format both ends read without
// being told to.
func TestCompressionOffJoinsWithoutSetCompression(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.NetworkCompressionThreshold = -1

	_, c := servertest.Dial(t, cfg)
	c.SendLoginStart("Testificate")

	frame := c.RawFrame()
	r := protocol.NewReader(frame)
	if got := r.ReadVarInt(); got != protocol.IDLoginSuccess {
		t.Fatalf("first packet of the login = 0x%02X, want login success with no set compression in front", got)
	}
	if got, want := r.ReadUUID(), protocol.OfflineUUID("Testificate"); got != want {
		t.Errorf("login success UUID = %s, want %s", got, want)
	}
	if got := r.ReadString(16); got != "Testificate" {
		t.Errorf("login success name = %q, want %q", got, "Testificate")
	}
	r.SkipProfileProperties()
	r.ReadUUID() // session ID
	if err := r.Err(); err != nil {
		t.Fatalf("login success: %v", err)
	}
	if got := r.Remaining(); got != 0 {
		t.Errorf("login success frame has %d bytes past its fields, want none", got)
	}

	c.Send(protocol.IDLoginAcknowledged, nil)
	c.Configure()
	c.EnterWorld("Testificate")

	// A message both ways proves the connection still plays: the server
	// reads what the client sends and relays its own back.
	c.Chat("hello")
	c.ExpectChat("Testificate", "hello")
}

// TestCompressionThresholdDeflatesPacketsPastIt checks what a positive
// network-compression-threshold buys on the wire: the packets below it ride
// with the compressed format's zero size prefix, and the first one past it —
// the first registry list, which runs to hundreds of bytes — arrives
// deflated, carrying its real size in front.
func TestCompressionThresholdDeflatesPacketsPastIt(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.NetworkCompressionThreshold = 64

	_, c := servertest.Dial(t, cfg)
	c.SendLoginStart("Testificate")

	// Set Compression itself is written before the switch, so it is a plain
	// frame.
	r := protocol.NewReader(c.RawFrame())
	if got := r.ReadVarInt(); got != protocol.IDSetCompression {
		t.Fatalf("first packet of the login = 0x%02X, want set compression", got)
	}
	if got := r.ReadVarInt(); got != 64 {
		t.Errorf("announced threshold = %d, want the configured 64", got)
	}
	c.Conn.EnableCompression(64)

	// The login success is below the threshold: the size prefix says zero
	// and the packet itself follows bare.
	r = protocol.NewReader(c.RawFrame())
	if got := r.ReadVarInt(); got != 0 {
		t.Fatalf("login success size prefix = %d, want 0 (below the threshold)", got)
	}
	if got := r.ReadVarInt(); got != protocol.IDLoginSuccess {
		t.Fatalf("packet = 0x%02X, want login success", got)
	}
	r.ReadUUID()
	if got := r.ReadString(16); got != "Testificate" {
		t.Errorf("login success name = %q, want %q", got, "Testificate")
	}

	// Everything the client writes from here on carries the size prefix
	// too, so the connection has to agree with the compressed format.
	c.Send(protocol.IDLoginAcknowledged, nil)

	// The configuration opens with the brand and the known packs, both far
	// below the threshold; the known pack is answered for the registries to
	// start arriving.
	var namespace, pack, version string
	for {
		r = protocol.NewReader(c.RawFrame())
		if got := r.ReadVarInt(); got != 0 {
			t.Fatalf("frame below the threshold arrived with size prefix %d, want 0", got)
		}
		if got := r.ReadVarInt(); got != protocol.IDKnownPacks {
			r.Rest()
			continue
		}
		packs := r.ReadVarInt()
		namespace, pack, version = r.ReadString(32767), r.ReadString(32767), r.ReadString(32767)
		if err := r.Err(); err != nil {
			t.Fatalf("known packs: %v", err)
		}
		if packs != 1 {
			t.Fatalf("known packs lists %d packs, want 1", packs)
		}
		break
	}
	c.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutString(namespace)
		b.PutString(pack)
		b.PutString(version)
	})

	// The registries start with the biome list, hundreds of bytes: the first
	// frame past the threshold, which has to be deflated.
	for {
		frame := c.RawFrame()
		r = protocol.NewReader(frame)
		size := r.ReadVarInt()
		if size == 0 {
			if body := len(r.Rest()); body >= 64 {
				t.Fatalf("a %d-byte packet rode below the threshold", body)
			}
			continue
		}
		if size < 100 {
			t.Fatalf("first deflated packet says it holds %d bytes, want at least a hundred", size)
		}
		zr, err := zlib.NewReader(bytes.NewReader(r.Rest()))
		if err != nil {
			t.Fatalf("read the deflated packet: %v", err)
		}
		inflated, err := io.ReadAll(zr)
		if err != nil {
			t.Fatalf("inflate the deflated packet: %v", err)
		}
		if got := int32(len(inflated)); got != size {
			t.Errorf("deflated packet inflated to %d bytes, want the %d its size prefix says", got, size)
		}
		if got := protocol.NewReader(inflated).ReadVarInt(); got != protocol.IDRegistryData {
			t.Errorf("first deflated packet = 0x%02X, want registry data", got)
		}
		break
	}
}

// sendDeflated writes a compressed-format frame by hand that declares size
// bytes for a payload it deflates from body, which Client.Conn cannot
// produce: its writer declares the true size.
func sendDeflated(c *servertest.Client, size int32, body []byte) {
	c.T.Helper()
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	if _, err := zw.Write(body); err != nil {
		c.T.Fatalf("deflate: %v", err)
	}
	if err := zw.Close(); err != nil {
		c.T.Fatalf("deflate: %v", err)
	}
	var frame protocol.Buffer
	frame.PutVarInt(size)
	frame.PutRaw(z.Bytes())
	var out protocol.Buffer
	out.PutVarInt(int32(len(frame.Bytes())))
	out.PutRaw(frame.Bytes())
	if _, err := c.Raw.Write(out.Bytes()); err != nil {
		c.T.Fatalf("write frame: %v", err)
	}
}

// TestCompressedFrameBelowThresholdDisconnects checks that a deflated frame
// declaring fewer bytes than the threshold ends the connection with the
// decoder's message, where a size of zero would have been the way to send
// it plain.
// vanilla: CompressionDecoder.decode
func TestCompressedFrameBelowThresholdDisconnects(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.NetworkCompressionThreshold = 256

	srv, c := servertest.Join(t, cfg, "Testificate")

	var ping protocol.Buffer
	ping.PutVarInt(protocol.IDPlayPingRequest)
	ping.PutI64(1)
	sendDeflated(c, int32(len(ping.Bytes())), ping.Bytes())

	var want protocol.Buffer
	want.PutText(protocol.Text{
		Translate: "disconnect.genericReason",
		With: []protocol.Text{{Text: "Internal Exception: io.netty.handler.codec.DecoderException: " +
			"Badly compressed packet - size of 9 is below server threshold of 256"}},
	})
	if reason := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(reason, want.Bytes()) {
		t.Errorf("disconnect reason % X, want % X", reason, want.Bytes())
	}
	servertest.PollUntil(t, 5*time.Second, func() bool { return srv.PlayerCount() == 0 })
}
