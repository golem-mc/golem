package world

import (
	"net"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

type chunkSendConn struct{ net.Conn }

func (chunkSendConn) Write(p []byte) (int, error)      { return len(p), nil }
func (chunkSendConn) SetWriteDeadline(time.Time) error { return nil }

func sendBenchmarkChunk(w *World, c *Chunk, conn *protocol.Conn) error {
	return conn.WriteCachedPacket(w.CachedChunkPacket(c))
}

// BenchmarkSend400ChunksTo10Clients includes payload construction, framing and
// compression, but no network I/O. Each operation sends 4,000 chunk packets.
func BenchmarkSend400ChunksTo10Clients(b *testing.B) {
	w, err := NewWorld(b.TempDir(), NewFlatGenerator())
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = w.Close() })
	chunks := make([]*Chunk, 400)
	for i := range chunks {
		pos := ChunkPos{X: int32(i % 20), Z: int32(i / 20)}
		chunks[i], _ = w.Acquire(pos)
		if i%10 == 0 {
			x, z := pos.X*16+1, pos.Z*16+1
			w.SetBlock(x, GroundLevel, z, registry.BlockStates["minecraft:oak_sign"])
			w.EnsureBlockEntity(x, GroundLevel, z, KindSign).Use(func(be *BlockEntity) {
				be.SignFront.Messages[0] = "A shared chunk"
			})
		}
	}
	clients := make([]*protocol.Conn, 10)
	for i := range clients {
		clients[i] = protocol.NewConn(chunkSendConn{})
		clients[i].EnableCompression(256)
	}
	for _, cold := range []bool{true, false} {
		name := "reused"
		if cold {
			name = "changed"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := range b.N {
				if cold {
					state := registry.BlockStates["minecraft:stone"]
					if i%2 == 0 {
						state = registry.BlockStates["minecraft:dirt"]
					}
					for _, c := range chunks {
						w.SetBlock(c.x*16+2, GroundLevel, c.z*16+2, state)
					}
				}
				for _, client := range clients {
					for _, c := range chunks {
						if err := sendBenchmarkChunk(w, c, client); err != nil {
							b.Fatal(err)
						}
					}
				}
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*4000), "ns/send")
		})
	}
}
