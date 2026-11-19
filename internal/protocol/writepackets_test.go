package protocol

import (
	"bytes"
	"net"
	"testing"
	"time"
)

type sinkConn struct {
	net.Conn
	buf    bytes.Buffer
	writes int
}

func (s *sinkConn) Write(b []byte) (int, error)      { s.writes++; return s.buf.Write(b) }
func (s *sinkConn) SetWriteDeadline(time.Time) error { return nil }

func TestWritePacketsMatchesWritePacket(t *testing.T) {
	for _, threshold := range []int{-1, 64} {
		one, many := &sinkConn{}, &sinkConn{}
		a, b := NewConn(one), NewConn(many)
		if threshold >= 0 {
			a.EnableCompression(threshold)
			b.EnableCompression(threshold)
		}
		datas := [][]byte{{1}, bytes.Repeat([]byte{7}, 200), nil, {3, 4}}
		for i, d := range datas {
			if err := a.WritePacket(int32(i), d); err != nil {
				t.Fatal(err)
			}
		}
		err := b.WritePackets(len(datas), func(i int) (int32, []byte) { return int32(i), datas[i] })
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(one.buf.Bytes(), many.buf.Bytes()) {
			t.Fatalf("threshold %d: batch bytes differ", threshold)
		}
		if many.writes != 1 {
			t.Fatalf("threshold %d: %d writes, want 1", threshold, many.writes)
		}
	}
}
