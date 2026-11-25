package protocol

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"testing"
	"testing/iotest"
	"time"
)

type encryptionConn struct {
	net.Conn
	w io.Writer
}

func (c encryptionConn) Write(b []byte) (int, error)    { return c.w.Write(b) }
func (encryptionConn) SetWriteDeadline(time.Time) error { return nil }

const encryptedReadSecret = "0123456789abcdef"

func TestCFB8MatchesOpenSSLVector(t *testing.T) {
	var plaintext [32]byte
	for i := range plaintext {
		plaintext[i] = byte(i)
	}
	iv := plaintext[:aes.BlockSize]
	block, err := aes.NewCipher(iv)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := hex.DecodeString("0a22f796e1b93e9032cff804838adfc3a5e4b3ffdd47108575533e672ef5d8ef")
	if err != nil {
		t.Fatal(err)
	}

	for _, direction := range []struct {
		name    string
		decrypt bool
		in      []byte
		want    []byte
	}{
		{"encrypt", false, plaintext[:], ciphertext},
		{"decrypt", true, ciphertext, plaintext[:]},
	} {
		t.Run(direction.name, func(t *testing.T) {
			for _, ends := range [][]int{{len(plaintext)}, {1, 15, 19, len(plaintext)}} {
				stream := newCFB8(block, iv, direction.decrypt)
				buf := bytes.Clone(direction.in)
				start := 0
				for _, end := range ends {
					chunk := buf[start:end]
					stream.XORKeyStream(chunk, chunk)
					start = end
				}
				if !bytes.Equal(buf, direction.want) {
					t.Errorf("chunk ends %v: got %x, want %x", ends, buf, direction.want)
				}
			}
		})
	}
}

func encryptReadFrames(t testing.TB, wire []byte) {
	t.Helper()
	secret := []byte(encryptedReadSecret)
	block, err := aes.NewCipher(secret)
	if err != nil {
		t.Fatal(err)
	}
	newCFB8(block, secret, false).XORKeyStream(wire, wire)
}

func encryptedSmallPacketBatch(t testing.TB, count int, payload []byte) []byte {
	t.Helper()
	body := append([]byte{1}, payload...)
	wire := bytes.Repeat(lengthPrefixed(int32(len(body)), body), count)
	encryptReadFrames(t, wire)
	return wire
}

func TestReadEncryptedPacketAcrossShortReads(t *testing.T) {
	first := bytes.Repeat([]byte("ab"), 100)
	second := []byte("next")
	wire := lengthPrefixed(int32(len(first)+1), append([]byte{1}, first...))
	wire = append(wire, lengthPrefixed(int32(len(second)+1), append([]byte{2}, second...))...)
	encryptReadFrames(t, wire)

	conn := NewConn(framedConn{r: iotest.OneByteReader(bytes.NewReader(wire))})
	// Encryption can start with ciphertext already buffered.
	if _, err := conn.Peek(1); err != nil {
		t.Fatal(err)
	}
	if err := conn.EnableEncryption([]byte(encryptedReadSecret)); err != nil {
		t.Fatal(err)
	}

	id, firstPayload, err := conn.ReadPacket()
	if err != nil || id != 1 {
		t.Fatalf("first ReadPacket = ID %d, err %v", id, err)
	}
	id, secondPayload, err := conn.ReadPacket()
	if err != nil || id != 2 || !bytes.Equal(secondPayload, second) {
		t.Fatalf("second ReadPacket = ID %d, payload %x, err %v", id, secondPayload, err)
	}
	if !bytes.Equal(firstPayload, first) {
		t.Fatal("first payload differs after reading the second frame")
	}
}

func TestReadEncryptedSmallPacketAllocations(t *testing.T) {
	const runs = 10
	payload := make([]byte, 32)
	// AllocsPerRun reads one frame to warm up before measuring.
	wire := encryptedSmallPacketBatch(t, runs+1, payload)
	conn := NewConn(framedConn{r: bytes.NewReader(wire)})
	if err := conn.EnableEncryption([]byte(encryptedReadSecret)); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(runs, func() {
		id, got, err := conn.ReadPacket()
		if err != nil || id != 1 || !bytes.Equal(got, payload) {
			t.Fatalf("ReadPacket = ID %d, payload %x, err %v", id, got, err)
		}
	})
	if allocs > 1 {
		t.Errorf("ReadPacket allocated %.0f times per frame, want at most one allocation for the frame", allocs)
	}
}

// BenchmarkReadEncryptedSmallPackets measures 1,024 frames per connection,
// including connection setup.
func BenchmarkReadEncryptedSmallPackets(b *testing.B) {
	const count = 1024
	payload := make([]byte, 32)
	wire := encryptedSmallPacketBatch(b, count, payload)

	b.ReportAllocs()
	for b.Loop() {
		conn := NewConn(framedConn{r: bytes.NewReader(wire)})
		if err := conn.EnableEncryption([]byte(encryptedReadSecret)); err != nil {
			b.Fatal(err)
		}
		for i := range count {
			id, got, err := conn.ReadPacket()
			if err != nil || id != 1 || !bytes.Equal(got, payload) {
				b.Fatalf("frame %d: ReadPacket = ID %d, payload %x, err %v", i, id, got, err)
			}
		}
	}
}

// TestEncryptedWritesMatchStreamWriter pins the continuous ciphertext across
// individual, batched and shared frames against the allocating writer.
func TestEncryptedWritesMatchStreamWriter(t *testing.T) {
	secret := []byte("0123456789abcdef")
	block, err := aes.NewCipher(secret)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("shared frame"), 7000)
	unchanged := bytes.Clone(payload)
	cached := NewCachedPacket(300, payload)
	for _, threshold := range []int{-1, 256, 1 << 20} {
		t.Run(fmt.Sprint(threshold), func(t *testing.T) {
			var got, want bytes.Buffer
			conn := NewConn(encryptionConn{w: &got})
			if err := conn.EnableEncryption(secret); err != nil {
				t.Fatal(err)
			}
			reference := NewConn(encryptionConn{w: cipher.StreamWriter{
				S: newCFB8(block, secret, false), W: &want,
			}})
			for _, c := range []*Conn{conn, reference} {
				c.EnableCompression(threshold)
				for range 2 {
					if err := c.WritePacket(1, []byte("before")); err != nil {
						t.Fatal(err)
					}
					if err := c.WritePackets(4, func(i int) (int32, []byte) {
						return int32(i), payload[:i*25000]
					}); err != nil {
						t.Fatal(err)
					}
					if err := c.WriteCachedPacket(cached); err != nil {
						t.Fatal(err)
					}
					if err := c.WritePacketsCached(3, func(i int) (int32, []byte, *CachedPacket) {
						if i == 1 {
							return 0, nil, cached
						}
						return int32(i), nil, nil
					}); err != nil {
						t.Fatal(err)
					}
					if err := c.WritePacket(2, []byte("after")); err != nil {
						t.Fatal(err)
					}
				}
			}
			if !bytes.Equal(got.Bytes(), want.Bytes()) {
				t.Fatalf("encrypted stream differs from StreamWriter: got %d bytes, want %d", got.Len(), want.Len())
			}
			if !bytes.Equal(payload, unchanged) {
				t.Fatal("encryption changed the caller's payload")
			}
		})
	}
}
