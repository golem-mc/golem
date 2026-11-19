package protocol

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"net"
	"runtime"
	"testing"
)

// allocated is how many bytes f allocates. The tests that call it are not
// parallel, so nothing else in the package is allocating beside it.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// allocBound is far above what a short value's read costs and far below
// what a presize to the counts below would.
const allocBound = 1 << 20

// TestComponentNBTIsChargedWhileRead pins that the NBT an item's components
// carry is held to the network quota as it is read: a list of half a million
// empty compounds, half a megabyte on the wire and over the quota at its
// fifty-two units an entry, is refused before its maps are built.
func TestComponentNBTIsChargedWhileRead(t *testing.T) {
	const count = 500000
	var tag Buffer
	tag.PutU8(0x09) // a list
	tag.PutU8(0x0a) // of compounds
	tag.PutI32(count)
	tag.PutRaw(make([]byte, count)) // each one only its end
	for _, tc := range []struct {
		name string
		read func(*Reader) bool
	}{
		{"custom_name", func(r *Reader) bool { _, ok := ReadText(r); return ok }},
		{"entity_data", func(r *Reader) bool { return readTypedEntityData(r, RegistrySizeEntityType) }},
		{"custom_data", readNetworkNBT},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var value Buffer
			if tc.name == "entity_data" {
				value.PutVarInt(0)
			}
			value.PutRaw(tag.Bytes())
			r := NewReader(value.Bytes())
			var ok bool
			if n := allocated(func() { ok = tc.read(r) }); n > allocBound {
				t.Errorf("refusing the list allocated %d bytes", n)
			}
			if ok || r.Err() == nil {
				t.Errorf("read = %v, %v; want the over-quota tag to fail the read", ok, r.Err())
			}
		})
	}
}

// framedConn is a connection whose reads are the bytes given and then EOF.
type framedConn struct {
	net.Conn
	r io.Reader
}

func (c framedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

// lengthPrefixed is frame behind its VarInt length, claiming length bytes.
func lengthPrefixed(length int32, frame []byte) []byte {
	var b Buffer
	b.PutVarInt(length)
	b.PutRaw(frame)
	return b.Bytes()
}

// TestReadPacketGrowsWithTheBytesReceived pins that a frame claiming the
// most a packet may be costs memory by what arrived: a hundred bytes behind
// a 2 MiB length fail as cut off without the 2 MiB being allocated first.
func TestReadPacketGrowsWithTheBytesReceived(t *testing.T) {
	data := lengthPrefixed(maxPacketSize, make([]byte, 100))
	var err error
	n := allocated(func() {
		_, _, err = NewConn(framedConn{r: bytes.NewReader(data)}).ReadPacket()
	})
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("ReadPacket = %v, want the frame cut off", err)
	}
	if n > allocBound {
		t.Errorf("a 100-byte frame claiming %d allocated %d bytes", maxPacketSize, n)
	}
}

// TestDecompressGrowsWithTheBytesInflated pins the same for a compressed
// frame's declared size: a body that inflates to a few bytes, claiming the
// most a packet may expand to, fails without that much being allocated.
func TestDecompressGrowsWithTheBytesInflated(t *testing.T) {
	var deflated bytes.Buffer
	zw := zlib.NewWriter(&deflated)
	_, _ = zw.Write([]byte{0x2D, 1, 2, 3})
	_ = zw.Close()
	var frame Buffer
	frame.PutVarInt(maxDecompressedSize)
	frame.PutRaw(deflated.Bytes())
	var err error
	n := allocated(func() { _, err = decompress(frame.Bytes(), 256) })
	if err == nil {
		t.Error("a body short of its declared size decompressed")
	}
	if n > 2*allocBound {
		t.Errorf("a %d-byte body claiming %d allocated %d bytes", deflated.Len(), maxDecompressedSize, n)
	}
}
