package protocol

import (
	"bytes"
	"sync"
	"testing"
)

func TestCachedPacketReusesConcurrentFrames(t *testing.T) {
	t.Parallel()
	packet := NewCachedPacket(300, bytes.Repeat([]byte("shared broadcast"), 1600))
	var got [50][]byte
	var wg sync.WaitGroup
	for i := range got {
		wg.Go(func() {
			conn := NewConn(&sinkConn{})
			conn.EnableCompression(256)
			var err error
			got[i], err = packet.frame(conn)
			if err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for _, frame := range got {
		if len(frame) == 0 || &frame[0] != &got[0][0] {
			t.Fatal("recipients did not reuse one frame")
		}
	}
}
