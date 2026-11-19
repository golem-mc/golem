package servertest

import (
	"net"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestReadAcknowledgesChunkBatchesOnlyInPlay reads a configuration Store
// Cookie, which shares its ID with the play Chunk Batch Finished: the reader
// must send nothing back, and must still answer a real play batch.
func TestReadAcknowledgesChunkBatchesOnlyInPlay(t *testing.T) {
	t.Parallel()
	cli, srv := net.Pipe()
	defer cli.Close()
	defer srv.Close()
	c := &Client{T: t, Conn: protocol.NewConn(cli)}
	server := protocol.NewConn(srv)

	got := make(chan int32, 4)
	go func() {
		for {
			id, _, err := server.ReadPacket()
			if err != nil {
				return
			}
			got <- id
		}
	}()
	write := func(id int32) {
		go server.WritePacket(id, nil)
	}
	write(protocol.IDConfigStoreCookie)
	if id, _, err := c.Read(); err != nil || id != protocol.IDConfigStoreCookie {
		t.Fatalf("read = %#x, %v", id, err)
	}
	// The marker is sent after the read: the pipe is ordered, so any
	// acknowledgement the read sent would arrive before it.
	c.Send(protocol.IDPlayKeepAliveIn, nil)
	select {
	case id := <-got:
		if id != protocol.IDPlayKeepAliveIn {
			t.Fatalf("configuration read sent packet %#x before the marker", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("marker was not received")
	}

	c.Playing = true
	write(protocol.IDPlayChunkBatchFinished)
	if _, _, err := c.Read(); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-got:
		if id != protocol.IDPlayChunkBatchReceived {
			t.Fatalf("play batch answer = %#x", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("play chunk batch was not answered")
	}
}
