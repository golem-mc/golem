package protocol_test

import (
	"math/rand/v2"
	"net"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// discardConn is a connection whose writes go nowhere, so a benchmark of
// WritePacket measures the framing and not a peer reading it.
type discardConn struct{ net.Conn }

func (discardConn) Write(p []byte) (int, error) { return len(p), nil }

func (discardConn) SetWriteDeadline(time.Time) error { return nil }

// BenchmarkWritePacket is the cost of framing a packet the size of a chunk's
// data, with compression and encryption enabled independently.
func BenchmarkWritePacket(b *testing.B) {
	payload := make([]byte, 100<<10)
	r := rand.New(rand.NewPCG(1, 2))
	for i := range payload {
		// A few distinct values, so deflate has something to find.
		payload[i] = byte(r.IntN(4))
	}
	for _, c := range []struct {
		name      string
		threshold int
		encrypted bool
	}{
		{"uncompressed", -1, false},
		{"deflated", 256, false},
		{"encrypted", -1, true},
		{"encrypted-deflated", 256, true},
	} {
		b.Run(c.name, func(b *testing.B) {
			conn := protocol.NewConn(discardConn{})
			if c.threshold >= 0 {
				conn.EnableCompression(c.threshold)
			}
			if c.encrypted {
				if err := conn.EnableEncryption([]byte("0123456789abcdef")); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(payload)))
			for b.Loop() {
				if err := conn.WritePacket(0x2D, payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkWritePackets measures a mailbox batch that reaches the flush size.
func BenchmarkWritePackets(b *testing.B) {
	const count = 64
	payload := make([]byte, 1024)
	at := func(int) (int32, []byte) { return 0x2D, payload }
	for _, name := range []string{"uncompressed", "encrypted"} {
		b.Run(name, func(b *testing.B) {
			conn := protocol.NewConn(discardConn{})
			if name == "encrypted" {
				if err := conn.EnableEncryption([]byte("0123456789abcdef")); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.SetBytes(count * int64(len(payload)))
			for b.Loop() {
				if err := conn.WritePackets(count, at); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// loopConn is a connection whose reads repeat one packet's bytes forever.
type loopConn struct {
	net.Conn
	frame []byte
	pos   int
}

func (c *loopConn) Read(p []byte) (int, error) {
	n := copy(p, c.frame[c.pos:])
	c.pos = (c.pos + n) % len(c.frame)
	return n, nil
}

// BenchmarkReadPacket is the cost of reading a packet uncompressed: one the
// size of a movement, read into a buffer of its length, and one past the
// size read in one go, which grows as its bytes arrive.
func BenchmarkReadPacket(b *testing.B) {
	for _, c := range []struct {
		name string
		size int
	}{{"small", 32}, {"large", 1 << 20}} {
		b.Run(c.name, func(b *testing.B) {
			var frame protocol.Buffer
			frame.PutVarInt(int32(c.size + 1))
			frame.PutU8(0x2D)
			frame.PutRaw(make([]byte, c.size))
			conn := protocol.NewConn(&loopConn{frame: frame.Bytes()})
			b.ReportAllocs()
			b.SetBytes(int64(c.size))
			for b.Loop() {
				if _, _, err := conn.ReadPacket(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkFramePacketRaw compares framing a 261-byte incompressible body
// (a signed chat packet) deflated and raw.
func BenchmarkFramePacketRaw(b *testing.B) {
	body := make([]byte, 261)
	r := rand.New(rand.NewPCG(5, 6))
	for i := range body {
		body[i] = byte(r.IntN(256))
	}
	for _, raw := range []bool{false, true} {
		name := "deflated"
		if raw {
			name = "raw"
		}
		b.Run(name, func(b *testing.B) {
			conn := protocol.NewConn(discardConn{})
			conn.EnableCompression(256)
			at := func(int) (int32, []byte, *protocol.CachedPacket, bool) { return 0x42, body, nil, raw }
			b.ReportAllocs()
			for b.Loop() {
				if err := conn.WritePacketsRaw(1, at); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
