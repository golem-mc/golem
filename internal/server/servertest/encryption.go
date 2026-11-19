package servertest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// CompleteEncryption answers the Encryption Request with a fresh shared
// secret and switches the client's side of the connection to it.
func (c *Client) CompleteEncryption() {
	c.T.Helper()
	CompleteEncryption(c.T, c.Conn, c.Expect, c.Send)
}

// CompleteEncryption is Client.CompleteEncryption for a client that brings
// its own reader and writer; conn is the connection both act on.
func CompleteEncryption(t *testing.T, conn *protocol.Conn, expect func(id int32) []byte, send func(id int32, fill func(*protocol.Buffer))) {
	t.Helper()
	r := protocol.NewReader(expect(protocol.IDEncryptionRequest))
	r.ReadString(20)
	der := r.ReadBytes(1024)
	token := r.ReadBytes(64)
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		t.Fatalf("server public key: %v", err)
	}
	encrypt := func(b []byte) []byte {
		out, err := rsa.EncryptPKCS1v15( //nolint:staticcheck // SA1019: fixed by the protocol
			rand.Reader, pub.(*rsa.PublicKey), b)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		return out
	}
	secret := make([]byte, 16)
	_, _ = rand.Read(secret)
	send(protocol.IDEncryptionResponse, func(b *protocol.Buffer) {
		b.PutBytes(encrypt(secret))
		b.PutBytes(encrypt(token))
	})
	if err := conn.EnableEncryption(secret); err != nil {
		t.Fatalf("enable encryption: %v", err)
	}
}
