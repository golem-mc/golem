package protocol

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/klauspost/compress/zlib"
)

// State is the connection state, which decides how packet IDs are interpreted.
type State int

const (
	StateHandshaking State = iota
	StateStatus
	StateLogin
	StateConfiguration
	StatePlay
)

func (s State) String() string {
	switch s {
	case StateHandshaking:
		return "handshaking"
	case StateStatus:
		return "status"
	case StateLogin:
		return "login"
	case StateConfiguration:
		return "configuration"
	case StatePlay:
		return "play"
	default:
		return fmt.Sprintf("state(%d)", int(s))
	}
}

const (
	// vanilla: Varint21FrameDecoder.MAX_VARINT21_BYTES
	packetLengthBytes = 3
	// maxDecompressedSize caps how much a single packet may expand to, so that
	// a small packet cannot be used to exhaust memory.
	maxDecompressedSize = 8 * 1024 * 1024
	// compressionDisabled is the threshold value used while compression is off.
	compressionDisabled = -1
	// writeTimeout is how long a single packet may take to hand off to the
	// network before the peer is considered stuck.
	writeTimeout = 30 * time.Second
)

// Conn is a Minecraft protocol connection. Packets are read from a single
// goroutine, but WritePacket may be called concurrently.
type Conn struct {
	conn net.Conn
	br   *bufio.Reader

	// r is the encrypted view of the connection once encryption is enabled,
	// and the buffered connection before that.
	r io.Reader

	// State is the connection state. It is only touched by the goroutine
	// reading from the connection.
	State State

	// Trace, when set, logs every packet at debug level: the last packet
	// before a client hangs up is usually the one it rejected.
	Trace *slog.Logger

	writeMu   sync.Mutex
	encrypter *cfb8
	threshold int

	received atomic.Int64
}

// NewConn wraps a network connection in the Minecraft framing.
func NewConn(conn net.Conn) *Conn {
	br := bufio.NewReader(conn)
	return &Conn{
		conn:      conn,
		br:        br,
		r:         br,
		threshold: compressionDisabled,
	}
}

// TakeReceived returns the packets read since the last call and resets the
// count; any goroutine may call it.
func (c *Conn) TakeReceived() int64 { return c.received.Swap(0) }

// RemoteAddr returns the address of the peer.
func (c *Conn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }

// SetReadDeadline sets the deadline for reading the next packet.
func (c *Conn) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }

// Close closes the underlying connection.
func (c *Conn) Close() error { return c.conn.Close() }

// Peek returns the next n bytes without consuming them.
func (c *Conn) Peek(n int) ([]byte, error) { return c.br.Peek(n) }

// PeekBuffered returns the bytes already received and not yet consumed,
// waiting for at least one.
func (c *Conn) PeekBuffered() ([]byte, error) {
	if _, err := c.br.Peek(1); err != nil {
		return nil, err
	}
	return c.br.Peek(c.br.Buffered())
}

// WriteRaw writes bytes outside the packet framing, for the legacy ping reply
// before encryption is enabled.
func (c *Conn) WriteRaw(b []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err := c.conn.Write(b)
	return err
}

// EnableCompression switches both directions to the compressed format,
// deflating packets of at least threshold bytes; Set Compression must be
// written first.
func (c *Conn) EnableCompression(threshold int) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.threshold = threshold
}

// EnableEncryption turns on AES/CFB8 using the shared secret as both key and
// IV; everything after this point, including packet lengths, is encrypted, so
// the Encryption Response must have been read before.
func (c *Conn) EnableEncryption(secret []byte) error {
	// The secret is also the feedback register, so any other key size would
	// garble the stream a few packets later rather than fail here.
	if len(secret) != aes.BlockSize {
		return fmt.Errorf("shared secret is %d bytes, want %d", len(secret), aes.BlockSize)
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return fmt.Errorf("shared secret: %w", err)
	}

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	// The buffered reader sits below the cipher, so bytes it read ahead are
	// decrypted as they are consumed.
	c.r = cipher.StreamReader{S: newCFB8(block, secret, true), R: c.br}
	c.encrypter = newCFB8(block, secret, false)
	return nil
}

// ReadPacket reads the next packet and returns its ID and payload. The payload
// is the caller's to keep: handlers queue replies made of it for another
// goroutine to write, so it must not share a buffer with the next packet.
func (c *Conn) ReadPacket() (packetID int32, data []byte, err error) {
	length, _, err := readVarInt(c.r, packetLengthBytes)
	if err != nil {
		return 0, nil, fmt.Errorf("read packet length: %w", err)
	}

	frame, err := readFrame(c.r, int(length))
	if err != nil {
		return 0, nil, fmt.Errorf("read packet: %w", err)
	}

	if c.threshold >= 0 {
		frame, err = decompress(frame, c.threshold)
		if err != nil {
			return 0, nil, err
		}
	}

	r := NewReader(frame)
	packetID = r.ReadVarInt()
	if r.Err() != nil {
		return 0, nil, fmt.Errorf("read packet id: %w", r.Err())
	}
	data = r.Rest()
	c.received.Add(1)

	if c.Trace != nil && c.Trace.Enabled(context.Background(), slog.LevelDebug) {
		c.Trace.Debug("packet in", "state", c.State, "id", packetHex(packetID), "bytes", len(data))
	}
	return packetID, data, nil
}

// frameChunk is the most readFrame allocates ahead of the bytes: a frame up
// to it gets one buffer, a larger one grows as it arrives.
const frameChunk = 64 << 10

// readFrame reads exactly n bytes, the io.ReadFull of a fresh buffer. Past
// frameChunk the buffer at most doubles what has already arrived, so a
// length prefix the peer never backs costs memory in proportion to what it
// did send rather than the length it claimed.
func readFrame(r io.Reader, n int) ([]byte, error) {
	if n <= frameChunk {
		frame := make([]byte, n)
		if _, err := io.ReadFull(r, frame); err != nil {
			return nil, err
		}
		return frame, nil
	}
	frame := make([]byte, 0, frameChunk)
	for len(frame) < n {
		if len(frame) == cap(frame) {
			frame = slices.Grow(frame, min(len(frame), n-len(frame)))
		}
		got, err := io.ReadFull(r, frame[len(frame):min(cap(frame), n)])
		frame = frame[:len(frame)+got]
		if err != nil {
			// A frame cut off after its first chunk is cut off midway,
			// whichever chunk the end fell in.
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return nil, err
		}
	}
	return frame, nil
}

// BadCompressionError reports a compressed frame whose declared size is under
// the server's threshold: a sender below it writes the size as zero instead.
type BadCompressionError struct {
	Size, Threshold int
}

func (e *BadCompressionError) Error() string {
	return fmt.Sprintf("Badly compressed packet - size of %d is below server threshold of %d", e.Size, e.Threshold)
}

// packetInflater keeps the input alongside the decoder so Reset needs no new
// reader allocation and pooled entries can release the previous packet's bytes.
type packetInflater struct {
	bytes.Reader
	zr io.ReadCloser
}

var packetInflaters = sync.Pool{New: func() any { return new(packetInflater) }}

// decompress unwraps a packet in the compressed format, whose payload is
// preceded by the uncompressed size, or by zero if it was sent verbatim.
// vanilla: CompressionDecoder.decode
func decompress(frame []byte, threshold int) ([]byte, error) {
	r := NewReader(frame)
	size := r.ReadVarInt()
	if r.Err() != nil {
		return nil, fmt.Errorf("read uncompressed size: %w", r.Err())
	}
	body := r.Rest()
	if size == 0 {
		return body, nil
	}
	if size > 0 && int(size) < threshold {
		return nil, &BadCompressionError{Size: int(size), Threshold: threshold}
	}
	if size < 0 || size > maxDecompressedSize {
		return nil, fmt.Errorf("uncompressed size %d out of range", size)
	}

	inflater := packetInflaters.Get().(*packetInflater)
	inflater.Reset(body)
	var err error
	if inflater.zr == nil {
		inflater.zr, err = zlib.NewReader(&inflater.Reader)
	} else {
		err = inflater.zr.(zlib.Resetter).Reset(&inflater.Reader, nil)
	}
	if err != nil {
		if inflater.zr != nil {
			inflater.zr.Close()
		}
		return nil, fmt.Errorf("decompress packet: %w", err)
	}

	// The declared size is the sender's claim, so the output grows with
	// what the body inflates to rather than being allocated up front.
	out, err := readFrame(inflater.zr, int(size))
	closeErr := inflater.zr.Close()
	inflater.Reset(nil)
	if err != nil {
		return nil, fmt.Errorf("decompress packet: %w", err)
	}
	if closeErr == nil {
		packetInflaters.Put(inflater)
	}
	return out, nil
}

// varIntRoom is the most bytes a VarInt takes, the room WritePacket leaves
// for the length prefix.
const varIntRoom = 5

// zlibWriters holds the deflate state of finished packets: a fresh writer
// allocates and clears about a megabyte of tables, more than a small packet
// costs to compress, and Reset yields the same bytes as a new one.
var zlibWriters = sync.Pool{New: func() any {
	// The standard library default is level 6; klauspost's default is 5.
	zw, _ := zlib.NewWriterLevel(nil, 6)
	return zw
}}

// frame is what WritePacket builds a packet in: id holds the packet ID and
// then the length prefix. Frames are pooled: a frame is written to the
// connection before WritePacket returns, and an io.Writer may not keep what it
// was given.
type frame struct{ id, out Buffer }

var frames = sync.Pool{New: func() any { return new(frame) }}

// WritePacket writes a packet with the given ID and payload.
func (c *Conn) WritePacket(packetID int32, data []byte) error {
	f := frames.Get().(*frame)
	defer frames.Put(f)

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	packet, err := c.framePacket(f, packetID, data, false)
	if err != nil {
		return err
	}
	if err := c.writeFrames(packet); err != nil {
		return err
	}
	c.traceOut(packetID, len(data))
	return nil
}

// batchFlushSize is the framed bytes WritePackets gathers before it writes
// them, bounding the buffer a large batch needs.
const batchFlushSize = 64 << 10

// batches holds the buffers WritePackets gathers framed packets in.
var batches = sync.Pool{New: func() any { return new(Buffer) }}

// WritePackets writes n packets, at(i) giving packet i's ID and payload, in
// order. Packets are gathered and written in as few network writes as
// batchFlushSize allows, under one deadline, and none of another writer's
// packets lands inside the batch.
func (c *Conn) WritePackets(n int, at func(i int) (int32, []byte)) error {
	return c.WritePacketsCached(n, func(i int) (int32, []byte, *CachedPacket) {
		id, data := at(i)
		return id, data, nil
	})
}

// WritePacketsCached is WritePackets with an optional shared packet per entry;
// a non-nil cache supplies both the ID and data instead of the first two values.
func (c *Conn) WritePacketsCached(n int, at func(i int) (int32, []byte, *CachedPacket)) error {
	return c.WritePacketsRaw(n, func(i int) (int32, []byte, *CachedPacket, bool) {
		id, data, cached := at(i)
		return id, data, cached, false
	})
}

// WritePacketsRaw is WritePacketsCached where an entry's raw flag sends it
// uncompressed even above the threshold (see framePacket); it is ignored for
// a cached entry.
func (c *Conn) WritePacketsRaw(n int, at func(i int) (int32, []byte, *CachedPacket, bool)) error {
	f := frames.Get().(*frame)
	defer frames.Put(f)
	batch := batches.Get().(*Buffer)
	defer batches.Put(batch)
	batch.Reset()

	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	for i := range n {
		packetID, data, cached, raw := at(i)
		var packet []byte
		var err error
		if cached != nil {
			packetID, data = cached.id, cached.data
			packet, err = cached.frame(c)
		} else {
			packet, err = c.framePacket(f, packetID, data, raw)
		}
		if err != nil {
			return err
		}
		batch.PutRaw(packet)
		c.traceOut(packetID, len(data))
		if batch.Len() >= batchFlushSize {
			if err := c.writeFrames(batch.Bytes()); err != nil {
				return err
			}
			batch.Reset()
		}
	}
	if batch.Len() == 0 {
		return nil
	}
	return c.writeFrames(batch.Bytes())
}

// framePacket builds the framed packet in f, which the result aliases; the
// caller holds writeMu. raw sends the body with a Data Length of 0 even at or
// above the threshold, which the client's decoder accepts at any size; it
// spares deflating a body that cannot shrink.
// vanilla: CompressionDecoder.decode
func (c *Conn) framePacket(f *frame, packetID int32, data []byte, raw bool) ([]byte, error) {
	id, out := &f.id, &f.out
	id.Reset()
	id.PutVarInt(packetID)
	bodyLen := id.Len() + len(data)

	// Room is left for the length prefix, filled in once the size is known,
	// so the payload is copied once. The body with a size ahead of it bounds
	// an uncompressed frame, and a compressed one unless deflate cannot shrink
	// the body.
	out.data = slices.Grow(out.data[:0], varIntRoom+varIntRoom+bodyLen)[:varIntRoom]
	switch {
	case c.threshold < 0:
		out.PutRaw(id.Bytes())
		out.PutRaw(data)
	case raw || bodyLen < c.threshold:
		// Below the threshold packets are sent as-is, marked by a zero size.
		out.PutVarInt(0)
		out.PutRaw(id.Bytes())
		out.PutRaw(data)
	default:
		out.PutVarInt(int32(bodyLen))
		zw := zlibWriters.Get().(*zlib.Writer)
		defer zlibWriters.Put(zw)
		zw.Reset(out)
		if _, err := zw.Write(id.Bytes()); err != nil {
			return nil, fmt.Errorf("compress packet: %w", err)
		}
		if _, err := zw.Write(data); err != nil {
			return nil, fmt.Errorf("compress packet: %w", err)
		}
		if err := zw.Close(); err != nil {
			return nil, fmt.Errorf("compress packet: %w", err)
		}
	}

	prefix := id
	prefix.Reset()
	prefix.PutVarInt(int32(out.Len() - varIntRoom))
	start := varIntRoom - prefix.Len()
	copy(out.data[start:], prefix.Bytes())
	return out.data[start:], nil
}

// writeFrames writes framed bytes under a fresh deadline: a client that stops
// reading must not be able to block us forever. The caller holds writeMu and
// owns b, which encryption modifies in place.
func (c *Conn) writeFrames(b []byte) error {
	if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	if c.encrypter != nil {
		c.encrypter.XORKeyStream(b, b)
	}
	if _, err := c.conn.Write(b); err != nil {
		return fmt.Errorf("write packet: %w", err)
	}
	return nil
}

// traceOut logs an outgoing packet when debug logging is on; packetHex is not
// formatted otherwise.
func (c *Conn) traceOut(packetID int32, size int) {
	if c.Trace != nil && c.Trace.Enabled(context.Background(), slog.LevelDebug) {
		c.Trace.Debug("packet out", "state", c.State, "id", packetHex(packetID), "bytes", size)
	}
}

// packetHex formats a packet ID the way the protocol documentation does.
func packetHex(packetID int32) string { return fmt.Sprintf("0x%02X", packetID) }

// cfb8 is AES/CFB8. Go's crypto/cipher only has full-block CFB; Minecraft
// shifts the feedback register a byte per byte.
type cfb8 struct {
	block   cipher.Block
	iv      []byte
	keyByte []byte
	decrypt bool
}

func newCFB8(block cipher.Block, iv []byte, decrypt bool) *cfb8 {
	return &cfb8{
		block:   block,
		iv:      bytes.Clone(iv),
		keyByte: make([]byte, block.BlockSize()),
		decrypt: decrypt,
	}
}

func (c *cfb8) XORKeyStream(dst, src []byte) {
	for i, in := range src {
		c.block.Encrypt(c.keyByte, c.iv)
		out := in ^ c.keyByte[0]
		// The feedback register keeps the last block of ciphertext.
		copy(c.iv, c.iv[1:])
		if c.decrypt {
			c.iv[len(c.iv)-1] = in
		} else {
			c.iv[len(c.iv)-1] = out
		}
		dst[i] = out
	}
}
