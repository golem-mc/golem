package protocol_test

import (
	"bytes"
	"math"
	"math/rand/v2"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/klauspost/compress/zlib"

	"github.com/golem-mc/golem/internal/protocol"
)

// recordConn is a connection that keeps a copy of everything written to it.
type recordConn struct {
	net.Conn
	got []byte
}

func (c *recordConn) Write(p []byte) (int, error) {
	c.got = append(c.got, p...)
	return len(p), nil
}

func (*recordConn) SetWriteDeadline(time.Time) error { return nil }

// referenceFrame is a packet's frame built from fresh buffers and a fresh
// deflate writer, as the protocol lays it out.
func referenceFrame(threshold int, id int32, payload []byte) []byte {
	var body, inner, frame protocol.Buffer
	body.PutVarInt(id)
	body.PutRaw(payload)
	switch {
	case threshold < 0:
		inner.PutRaw(body.Bytes())
	case body.Len() < threshold:
		inner.PutVarInt(0)
		inner.PutRaw(body.Bytes())
	default:
		inner.PutVarInt(int32(body.Len()))
		zw, _ := zlib.NewWriterLevel(&inner, 6)
		_, _ = zw.Write(body.Bytes())
		_ = zw.Close()
	}
	frame.PutVarInt(int32(inner.Len()))
	frame.PutRaw(inner.Bytes())
	return frame.Bytes()
}

// TestWritePacketPooledFramesMatchReference writes packets of growing and
// shrinking sizes through connections on goroutines of their own, so frames
// are reused across packets and connections, and checks every byte each
// connection was sent against the reference frames.
func TestWritePacketPooledFramesMatchReference(t *testing.T) {
	t.Parallel()
	sizes := []int{100 << 10, 0, 3, 300, 70 << 10, 17, 2 << 10}
	for _, threshold := range []int{-1, 256} {
		var wg sync.WaitGroup
		for g := range 4 {
			wg.Go(func() {
				rc := &recordConn{}
				conn := protocol.NewConn(rc)
				if threshold >= 0 {
					conn.EnableCompression(threshold)
				}
				var want []byte
				for i, n := range sizes {
					payload := make([]byte, n)
					for j := range payload {
						payload[j] = byte(g + i + j%7)
					}
					id := int32(i*100 + g)
					if err := conn.WritePacket(id, payload); err != nil {
						t.Errorf("write packet: %v", err)
						return
					}
					want = append(want, referenceFrame(threshold, id, payload)...)
				}
				if !bytes.Equal(rc.got, want) {
					t.Errorf("threshold %d, connection %d: %d bytes sent differ from the %d of the reference frames",
						threshold, g, len(rc.got), len(want))
				}
			})
		}
		wg.Wait()
	}
}

// TestWritePacketsMatchFrames writes a mixed batch through one connection
// write and checks it is byte-identical to framing each packet separately.
func TestWritePacketsMatchFrames(t *testing.T) {
	t.Parallel()
	packets := []struct {
		ID   int32
		Data []byte
	}{
		{ID: 0x123, Data: []byte("under")},
		{ID: 0x7f, Data: []byte("")},
		{ID: 0x20, Data: make([]byte, 255)},
		{ID: 0xffffff, Data: make([]byte, 300)},
		{ID: 0x01, Data: make([]byte, 100<<10)},
		{ID: 0x04, Data: []byte("after")},
	}
	for i, data := range packets[2:5] {
		for j := range data.Data {
			data.Data[j] = byte(i + j%31)
		}
	}
	for _, threshold := range []int{-1, 256} {
		batched := &recordConn{}
		batchedConn := protocol.NewConn(batched)
		individual := &recordConn{}
		individualConn := protocol.NewConn(individual)
		for _, conn := range []*protocol.Conn{batchedConn, individualConn} {
			if threshold >= 0 {
				conn.EnableCompression(threshold)
			}
		}
		err := batchedConn.WritePackets(len(packets), func(i int) (int32, []byte) {
			return packets[i].ID, packets[i].Data
		})
		if err != nil {
			t.Fatalf("write packets: %v", err)
		}
		for _, packet := range packets {
			if err := individualConn.WritePacket(packet.ID, packet.Data); err != nil {
				t.Fatalf("write packet: %v", err)
			}
		}
		if !bytes.Equal(batched.got, individual.got) {
			difference := min(len(batched.got), len(individual.got))
			for i := range difference {
				if batched.got[i] != individual.got[i] {
					difference = i
					break
				}
			}
			t.Errorf("first difference at %d: batch %02x, separate %02x",
				difference, batched.got[difference], individual.got[difference])
			t.Errorf("threshold %d: %d bytes sent in one write differ from the %d of separate frames",
				threshold, len(batched.got), len(individual.got))
		}
	}
}

// TestWritePacketsRawSkipsDeflate checks a raw entry above the threshold is
// framed with a Data Length of 0, id and data verbatim.
func TestWritePacketsRawSkipsDeflate(t *testing.T) {
	t.Parallel()
	body := make([]byte, 300)
	r := rand.New(rand.NewPCG(3, 4))
	for i := range body {
		body[i] = byte(r.IntN(256))
	}
	rc := &recordConn{}
	conn := protocol.NewConn(rc)
	conn.EnableCompression(256)
	err := conn.WritePacketsRaw(1, func(int) (int32, []byte, *protocol.CachedPacket, bool) {
		return 0x42, body, nil, true
	})
	if err != nil {
		t.Fatal(err)
	}
	rd := protocol.NewReader(rc.got)
	length, dataLen, id := rd.ReadVarInt(), rd.ReadVarInt(), rd.ReadVarInt()
	if int(length) != rd.Remaining()+2 || dataLen != 0 || id != 0x42 {
		t.Fatalf("length %d, data length %d, id %#x", length, dataLen, id)
	}
	if !bytes.Equal(rd.Rest(), body) {
		t.Fatal("body differs")
	}
}

func TestWritePacketsRawMatchesFrames(t *testing.T) {
	t.Parallel()
	packets := []struct {
		id     int32
		size   int
		raw    bool
		cached bool
	}{
		{0x42, 254, false, false},  // one byte below the threshold
		{0x123, 253, false, false}, // one byte below with a two-byte ID
		{0x42, 255, false, false},  // exactly at the threshold
		{0x123, 254, false, false},
		{0x42, 300, true, false},
		{0x123, 300, true, false},
		{0x42, 300, true, true}, // cached framing ignores raw
		{0x123, 125, true, true},
	}
	payloads := make([][]byte, len(packets))
	cached := make([]*protocol.CachedPacket, len(packets))
	for i, packet := range packets {
		payloads[i] = bytes.Repeat([]byte{byte(i)}, packet.size)
		if packet.cached {
			cached[i] = protocol.NewCachedPacket(packet.id, payloads[i])
		}
	}

	for _, threshold := range []int{-1, 256} {
		wire := &recordConn{}
		conn := protocol.NewConn(wire)
		if threshold >= 0 {
			conn.EnableCompression(threshold)
		}
		err := conn.WritePacketsRaw(len(packets), func(i int) (int32, []byte, *protocol.CachedPacket, bool) {
			packet := packets[i]
			if cached[i] != nil {
				return 0, nil, cached[i], packet.raw
			}
			return packet.id, payloads[i], nil, packet.raw
		})
		if err != nil {
			t.Fatal(err)
		}
		var want []byte
		for i, packet := range packets {
			frameThreshold := threshold
			if threshold >= 0 && packet.raw && !packet.cached {
				frameThreshold = math.MaxInt
			}
			want = append(want, referenceFrame(frameThreshold, packet.id, payloads[i])...)
		}
		if !bytes.Equal(wire.got, want) {
			t.Errorf("threshold %d: mixed batch (%d bytes) differs from reference frames (%d bytes)",
				threshold, len(wire.got), len(want))
		}
	}
}
