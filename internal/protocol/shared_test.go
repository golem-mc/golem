package protocol_test

import (
	"bytes"
	"math/rand/v2"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func TestSharedFramesMatchReference(t *testing.T) {
	t.Parallel()
	for _, size := range []int{0, 253, 254, 255, 510, 70000} {
		data := bytes.Repeat([]byte{31}, size)
		for _, first := range []int{-1, 0, 256, 512} {
			shared := protocol.NewCachedPacket(0x123, data) // the two ID bytes count toward the threshold
			for _, threshold := range []int{first, -1, 0, 256, 512, first} {
				wire := &recordConn{}
				conn := protocol.NewConn(wire)
				conn.EnableCompression(threshold)
				if err := conn.WriteCachedPacket(shared); err != nil {
					t.Fatal(err)
				}
				got := wire.got
				if !bytes.Equal(got, referenceFrame(threshold, 0x123, data)) {
					t.Fatalf("size %d, cached threshold %d, threshold %d: frame differs", size, first, threshold)
				}
			}
		}
	}
}

func TestSharedBatchesKeepEncryptionAndOrder(t *testing.T) {
	t.Parallel()
	// Incompressible enough to force the batch to flush before its last packet.
	data := make([]byte, 70<<10)
	rng := rand.New(rand.NewPCG(6393, 0))
	for i := range data {
		data[i] = byte(rng.Uint32())
	}
	shared := protocol.NewCachedPacket(0x123, data)
	for _, threshold := range []int{-1, 0, 256, 100000} {
		for _, secret := range []string{"0123456789abcdef", "fedcba9876543210"} {
			gotWire, wantWire := &recordConn{}, &recordConn{}
			got, want := protocol.NewConn(gotWire), protocol.NewConn(wantWire)
			got.EnableCompression(threshold)
			want.EnableCompression(threshold)
			for _, conn := range []*protocol.Conn{got, want} {
				if err := conn.EnableEncryption([]byte(secret)); err != nil {
					t.Fatal(err)
				}
			}
			packets := []struct {
				id     int32
				data   []byte
				shared *protocol.CachedPacket
			}{
				{1, []byte("before"), nil}, {0x123, data, shared}, {0x123, data, shared}, {2, []byte("after"), nil},
			}
			if err := got.WritePacketsCached(len(packets), func(i int) (int32, []byte, *protocol.CachedPacket) {
				p := packets[i]
				return p.id, p.data, p.shared
			}); err != nil {
				t.Fatal(err)
			}
			for _, p := range packets {
				if err := want.WritePacket(p.id, p.data); err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(gotWire.got, wantWire.got) {
				t.Fatalf("threshold %d, secret %s: encrypted batch differs", threshold, secret)
			}
		}
	}
}
