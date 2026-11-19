package management

import (
	"bufio"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxMessage is the largest text message, in bytes, joined from its frames.
// vanilla: ManagementServer.start, WebSocketFrameAggregator(65536)
const maxMessage = 65536

// writeTimeout bounds one frame's write.
const writeTimeout = 10 * time.Second

// closeDrain bounds how long a connection closed for a violation reads on
// for the peer's own close.
const closeDrain = 2 * time.Second

// wsGUID is the RFC 6455 key suffix the accept hash is taken over.
const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// subprotocol is the Sec-WebSocket-Protocol value a 101 echoes to a client
// that sent its key in that header. vanilla: AuthenticationHandler
const subprotocol = "minecraft-v1"

// RFC 6455 close codes.
const (
	CloseNormal        uint16 = 1000
	CloseProtocolError uint16 = 1002
	CloseTooBig        uint16 = 1009
)

// Frame opcodes.
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xA
)

// CloseError is the close frame that ended a WSConn, sent or received. It
// matches io.EOF under errors.Is.
type CloseError struct{ Code uint16 }

func (e *CloseError) Error() string { return "websocket closed with code " + strconv.Itoa(int(e.Code)) }

// Is makes a close read as the end of the stream.
func (e *CloseError) Is(target error) bool { return target == io.EOF }

// upgrade answers a WebSocket handshake on path / and returns the connection
// it hijacked, or nil after writing the refusal.
func upgrade(w http.ResponseWriter, r *http.Request, withSubprotocol bool) *WSConn {
	if r.Method != http.MethodGet || r.URL.Path != "/" ||
		!headerHasToken(r.Header, "Upgrade", "websocket") || !headerHasToken(r.Header, "Connection", "upgrade") {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return nil
	}
	if r.Header.Get("Sec-WebSocket-Version") != "13" {
		w.Header().Set("Sec-WebSocket-Version", "13")
		http.Error(w, "Upgrade Required", http.StatusUpgradeRequired)
		return nil
	}
	key := r.Header.Get("Sec-WebSocket-Key")
	if raw, err := base64.StdEncoding.DecodeString(key); err != nil || len(raw) != 16 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return nil
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return nil
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil
	}
	// The HTTP server's header deadline must not cut the socket later.
	_ = conn.SetDeadline(time.Time{})
	sum := sha1.Sum([]byte(key + wsGUID))
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(sum[:]) + "\r\n"
	if withSubprotocol {
		resp += "Sec-WebSocket-Protocol: " + subprotocol + "\r\n"
	}
	if _, err := brw.WriteString(resp + "\r\n"); err != nil || brw.Flush() != nil {
		conn.Close()
		return nil
	}
	// Reads go through the hijack's reader, which may already hold the first
	// frame; writes go straight to the socket.
	return NewWSConn(hijacked{brw.Reader, conn}, false)
}

// hijacked is a hijacked socket read through its buffered reader.
type hijacked struct {
	*bufio.Reader
	net.Conn
}

func (h hijacked) Read(p []byte) (int, error) { return h.Reader.Read(p) }

// headerHasToken reports whether a comma-separated header holds token,
// compared case-insensitively.
func headerHasToken(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for t := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// WSConn is one side of a WebSocket connection: the server's, or with client
// set the test client's, which masks what it sends and expects nothing
// masked back.
type WSConn struct {
	r      *bufio.Reader
	rw     io.ReadWriter
	client bool

	// mu orders writes: the reader answers pings while another goroutine
	// sends messages.
	mu     sync.Mutex
	closed bool
}

// NewWSConn wraps rw, which is closed with the connection when it is an
// io.Closer.
func NewWSConn(rw io.ReadWriter, client bool) *WSConn {
	return &WSConn{r: bufio.NewReader(rw), rw: rw, client: client}
}

// ReadMessage returns the next text message, answering pings and skipping
// binary messages and pongs on the way. A close frame from the peer is
// echoed and returned as a *CloseError; a protocol violation or an oversized
// message closes the connection with 1002 or 1009 and returns that code.
func (c *WSConn) ReadMessage() ([]byte, error) {
	var msg []byte
	inMessage, text := false, false
	for {
		var head [2]byte
		if _, err := io.ReadFull(c.r, head[:]); err != nil {
			return nil, err
		}
		fin, rsv, op := head[0]&0x80 != 0, head[0]&0x70, head[0]&0x0F
		masked, length := head[1]&0x80 != 0, uint64(head[1]&0x7F)
		switch length {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(c.r, b[:]); err != nil {
				return nil, err
			}
			length = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(c.r, b[:]); err != nil {
				return nil, err
			}
			length = binary.BigEndian.Uint64(b[:])
		}
		control := op >= opClose
		if rsv != 0 || masked == c.client || length > 1<<63-1 ||
			control && (!fin || length > 125) ||
			op != opContinuation && op != opText && op != opBinary && op != opClose && op != opPing && op != opPong ||
			op == opContinuation && !inMessage || (op == opText || op == opBinary) && inMessage {
			return nil, c.fail(CloseProtocolError)
		}
		if !control && uint64(len(msg))+length > maxMessage {
			return nil, c.fail(CloseTooBig)
		}
		var key [4]byte
		if masked {
			if _, err := io.ReadFull(c.r, key[:]); err != nil {
				return nil, err
			}
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(c.r, payload); err != nil {
			return nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= key[i%4]
			}
		}
		switch op {
		case opClose:
			code := CloseNormal
			if len(payload) >= 2 {
				code = binary.BigEndian.Uint16(payload)
			}
			_ = c.closeWith(payload[:min(len(payload), 2)])
			return nil, &CloseError{Code: code}
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil {
				return nil, err
			}
			continue
		case opPong:
			continue
		case opText, opBinary:
			inMessage, text = true, op == opText
		}
		msg = append(msg, payload...)
		if fin {
			if text {
				return msg, nil
			}
			msg, inMessage = msg[:0], false
		}
	}
}

// WriteText sends p as one text frame.
func (c *WSConn) WriteText(p []byte) error { return c.writeFrame(opText, p) }

// Close sends a close frame with code and closes the connection.
func (c *WSConn) Close(code uint16) error {
	return c.closeWith(binary.BigEndian.AppendUint16(nil, code))
}

// abort closes the socket without a close frame and without waiting for a
// write in progress.
func (c *WSConn) abort() {
	if closer, ok := c.rw.(io.Closer); ok {
		_ = closer.Close()
	}
}

// fail closes the connection with code and returns the matching error.
//
// The peer's input is read on, up to closeDrain or 1 MiB, before the socket closes:
// closing over unread input resets the connection, which can discard the
// close frame before the peer reads it.
func (c *WSConn) fail(code uint16) error {
	c.mu.Lock()
	if !c.closed {
		_ = c.writeFrameLocked(opClose, binary.BigEndian.AppendUint16(nil, code))
		c.closed = true
	}
	c.mu.Unlock()
	if d, ok := c.rw.(interface{ SetReadDeadline(time.Time) error }); ok {
		_ = d.SetReadDeadline(time.Now().Add(closeDrain))
	}
	_, _ = io.CopyN(io.Discard, c.r, 1<<20)
	c.abort()
	return &CloseError{Code: code}
}

// closeWith sends a close frame carrying payload, once, and closes rw.
func (c *WSConn) closeWith(payload []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	err := c.writeFrameLocked(opClose, payload)
	c.closed = true
	c.abort()
	return err
}

func (c *WSConn) writeFrame(op byte, p []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	return c.writeFrameLocked(op, p)
}

// writeFrameLocked writes one final frame; the client masks it with a fresh
// random key.
func (c *WSConn) writeFrameLocked(op byte, p []byte) error {
	buf := make([]byte, 0, 14+len(p))
	buf = append(buf, 0x80|op)
	var mask byte
	if c.client {
		mask = 0x80
	}
	switch n := len(p); {
	case n <= 125:
		buf = append(buf, mask|byte(n))
	case n <= 0xFFFF:
		buf = binary.BigEndian.AppendUint16(append(buf, mask|126), uint16(n))
	default:
		buf = binary.BigEndian.AppendUint64(append(buf, mask|127), uint64(n))
	}
	if !c.client {
		buf = append(buf, p...)
	} else {
		var key [4]byte
		_, _ = rand.Read(key[:]) // crypto/rand.Read never returns an error
		buf = append(buf, key[:]...)
		for i, b := range p {
			buf = append(buf, b^key[i%4])
		}
	}
	// A client that stops reading fails the write rather than hold the lock.
	if d, ok := c.rw.(interface{ SetWriteDeadline(time.Time) error }); ok {
		_ = d.SetWriteDeadline(time.Now().Add(writeTimeout))
	}
	_, err := c.rw.Write(buf)
	return err
}
