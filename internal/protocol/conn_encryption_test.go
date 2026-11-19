package protocol

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"net"
	"testing"
	"time"
)

type encryptionConn struct {
	net.Conn
	w io.Writer
}

func (c encryptionConn) Write(b []byte) (int, error)    { return c.w.Write(b) }
func (encryptionConn) SetWriteDeadline(time.Time) error { return nil }

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
