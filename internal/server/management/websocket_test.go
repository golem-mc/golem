package management

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// clientFrame encodes one client frame, masked unless unmasked is set.
func clientFrame(fin bool, op byte, payload string, unmasked bool) []byte {
	b := []byte{op}
	if fin {
		b[0] |= 0x80
	}
	if unmasked {
		return append(append(b, byte(len(payload))), payload...)
	}
	key := [4]byte{1, 2, 3, 4}
	b = append(append(b, 0x80|byte(len(payload))), key[:]...)
	for i := range len(payload) {
		b = append(b, payload[i]^key[i%4])
	}
	return b
}

// pipe returns a server WSConn and the raw client end, closed with the test.
func pipe(t *testing.T) (*WSConn, net.Conn) {
	t.Helper()
	srv, cli := net.Pipe()
	t.Cleanup(func() { srv.Close(); cli.Close() })
	if err := cli.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	return NewWSConn(srv, false), cli
}

type readResult struct {
	msg []byte
	err error
}

func readAsync(c *WSConn) <-chan readResult {
	ch := make(chan readResult, 1)
	go func() {
		msg, err := c.ReadMessage()
		ch <- readResult{msg, err}
	}()
	return ch
}

func write(t *testing.T, conn net.Conn, frames ...[]byte) {
	t.Helper()
	for _, f := range frames {
		if _, err := conn.Write(f); err != nil {
			t.Fatalf("write frame: %v", err)
		}
	}
}

func TestWSConnJoinsFragmentsAndAnswersPing(t *testing.T) {
	t.Parallel()
	srv, cli := pipe(t)
	got := readAsync(srv)

	write(t, cli, clientFrame(false, opText, "hel", false), clientFrame(true, opPing, "p", false))
	pong := make([]byte, 3)
	if _, err := io.ReadFull(cli, pong); err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if want := []byte{0x80 | opPong, 1, 'p'}; string(pong) != string(want) {
		t.Errorf("pong = % x, want % x", pong, want)
	}
	// A pong between the fragments, and a binary message before the next
	// text, are skipped.
	write(t, cli, clientFrame(true, opPong, "", false), clientFrame(true, opContinuation, "lo", false))
	if r := <-got; r.err != nil || string(r.msg) != "hello" {
		t.Fatalf("ReadMessage = %q, %v, want hello", r.msg, r.err)
	}
	got = readAsync(srv)
	write(t, cli, clientFrame(true, opBinary, "bin", false), clientFrame(true, opText, "next", false))
	if r := <-got; r.err != nil || string(r.msg) != "next" {
		t.Fatalf("ReadMessage = %q, %v, want next", r.msg, r.err)
	}
}

func TestWSConnClosesOnUnmaskedClientFrame(t *testing.T) {
	t.Parallel()
	srv, cli := pipe(t)
	got := readAsync(srv)
	write(t, cli, clientFrame(true, opText, "hi", true))

	client := NewWSConn(cli, true)
	_, err := client.ReadMessage()
	var closed *CloseError
	if !errors.As(err, &closed) || closed.Code != CloseProtocolError {
		t.Errorf("client read %v, want a close with %d", err, CloseProtocolError)
	}
	if !errors.Is(err, io.EOF) {
		t.Errorf("a close %v does not read as io.EOF", err)
	}
	if r := <-got; !errors.As(r.err, &closed) || closed.Code != CloseProtocolError {
		t.Errorf("server read %v, want a close with %d", r.err, CloseProtocolError)
	}
}
