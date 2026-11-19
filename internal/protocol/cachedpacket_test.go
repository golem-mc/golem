package protocol_test

import (
	"bytes"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

func TestCachedPacketsMatchFreshFrames(t *testing.T) {
	payload := bytes.Repeat([]byte("shared chunk data"), 1024)
	cached := protocol.NewCachedPacket(300, payload)
	for _, encrypted := range []bool{false, true} {
		// Revisit thresholds after replacing the cached frame, and exercise the
		// exact threshold including the two-byte packet ID.
		for _, threshold := range []int{-1, 0, 256, len(payload) + 2, len(payload) + 3, -1, 256} {
			gotWire, wantWire := &recordConn{}, &recordConn{}
			got, want := protocol.NewConn(gotWire), protocol.NewConn(wantWire)
			for _, conn := range []*protocol.Conn{got, want} {
				if threshold >= 0 {
					conn.EnableCompression(threshold)
				}
				if encrypted {
					if err := conn.EnableEncryption([]byte("0123456789abcdef")); err != nil {
						t.Fatal(err)
					}
				}
			}
			// Different preceding packets put each connection's cipher at its
			// own stream position before it writes the shared frame.
			for _, conn := range []*protocol.Conn{got, want} {
				if err := conn.WritePacket(1, []byte{byte(threshold)}); err != nil {
					t.Fatal(err)
				}
			}
			if err := got.WriteCachedPacket(cached); err != nil {
				t.Fatal(err)
			}
			if err := want.WritePacket(300, payload); err != nil {
				t.Fatal(err)
			}
			if err := got.WritePacketsCached(3, func(i int) (int32, []byte, *protocol.CachedPacket) {
				if i == 1 {
					return 0, nil, cached
				}
				return int32(i), []byte{byte(i)}, nil
			}); err != nil {
				t.Fatal(err)
			}
			for i := range 3 {
				id, data := int32(i), []byte{byte(i)}
				if i == 1 {
					id, data = 300, payload
				}
				if err := want.WritePacket(id, data); err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(gotWire.got, wantWire.got) {
				t.Fatalf("encrypted=%v threshold=%d: cached wire differs from fresh framing", encrypted, threshold)
			}
		}
	}
}

func TestCachedPacketEncryptedRoundTrip(t *testing.T) {
	payload := bytes.Repeat([]byte("chunk"), 1000)
	packet := protocol.NewCachedPacket(300, payload)
	for _, threshold := range []int{0, 256, 10000} {
		client, server := connPair(t)
		enableEncryption(t, client, server)
		client.EnableCompression(threshold)
		server.EnableCompression(threshold)
		for range 2 {
			done := make(chan error, 1)
			go func() { done <- server.WriteCachedPacket(packet) }()
			id, data, err := client.ReadPacket()
			if err != nil {
				t.Fatal(err)
			}
			if id != 300 || !bytes.Equal(data, payload) {
				t.Fatal("cached packet did not survive encryption and compression")
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestCachedPacketConcurrentConnections(t *testing.T) {
	payload := bytes.Repeat([]byte("shared data"), 500)
	packet := protocol.NewCachedPacket(300, payload)
	var readers sync.WaitGroup
	for i := range 20 {
		readers.Go(func() {
			threshold := []int{-1, 0, 256, 10000}[i%4]
			gotWire, wantWire := &recordConn{}, &recordConn{}
			got, want := protocol.NewConn(gotWire), protocol.NewConn(wantWire)
			if threshold >= 0 {
				got.EnableCompression(threshold)
				want.EnableCompression(threshold)
			}
			if i/4%2 == 1 {
				for _, conn := range []*protocol.Conn{got, want} {
					if err := conn.EnableEncryption([]byte("0123456789abcdef")); err != nil {
						t.Error(err)
						return
					}
				}
			}
			for range 10 {
				if err := got.WriteCachedPacket(packet); err != nil {
					t.Error(err)
					return
				}
				if err := want.WritePacket(300, payload); err != nil {
					t.Error(err)
					return
				}
			}
			if !bytes.Equal(gotWire.got, wantWire.got) {
				t.Errorf("threshold %d: concurrent cached frames differ", threshold)
			}
		})
	}
	readers.Wait()
}
