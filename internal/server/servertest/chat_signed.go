package servertest

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/binary"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/google/uuid"
)

// SignedSession creates a chat key and certificate under a test service key.
func SignedSession(t *testing.T, profile uuid.UUID, issuer ...*rsa.PrivateKey) (*rsa.PrivateKey, chatsecurity.SessionData, *chatsecurity.KeySet) {
	t.Helper()
	var service *rsa.PrivateKey
	if len(issuer) > 0 {
		service = issuer[0]
	} else {
		var err error
		service, err = rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatal(err)
		}
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.MarshalPKIXPublicKey(&service.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := chatsecurity.ParseKeys(root)
	if err != nil {
		t.Fatal(err)
	}
	data := chatsecurity.SessionData{ID: uuid.New(), ExpiresAt: time.Now().Add(time.Hour).UnixMilli(), PublicKey: der}
	payload := append([]byte{}, profile[:]...)
	payload = binary.BigEndian.AppendUint64(payload, uint64(data.ExpiresAt))
	payload = append(payload, der...)
	digest := sha1.Sum(payload)
	data.Certificate, err = rsa.SignPKCS1v15(rand.Reader, service, crypto.SHA1, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	return key, data, keys
}

// SignedChatBytes is independent of the production SigningBytes encoder.
func SignedChatBytes(profile, session uuid.UUID, index int32, body chatsecurity.Body) []byte {
	var b bytes.Buffer
	for _, value := range []any{int32(1), profile, session, index, body.Salt(), time.UnixMilli(body.Timestamp()).Unix(), int32(len(body.Content()))} {
		_ = binary.Write(&b, binary.BigEndian, value)
	}
	b.WriteString(body.Content())
	seen := body.LastSeen()
	_ = binary.Write(&b, binary.BigEndian, int32(len(seen)))
	for _, signature := range seen {
		b.Write(signature[:])
	}
	return b.Bytes()
}

// CompleteOnlineChatLogin joins through encrypted online-mode login.
func CompleteOnlineChatLogin(c *Client, name string, inspect ...func(int32, []byte)) {
	t := c.T
	t.Helper()
	c.Handshake(protocol.NextStateLogin)
	c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) {
		b.PutString(name)
		b.PutUUID([16]byte{})
	})
	c.CompleteEncryption()

	id, data, err := c.Read()
	if err != nil {
		t.Fatalf("read login: %v", err)
	}
	if id == protocol.IDSetCompression {
		c.Conn.EnableCompression(int(protocol.NewReader(data).ReadVarInt()))
		c.Expect(protocol.IDLoginSuccess)
	} else if id != protocol.IDLoginSuccess {
		t.Fatalf("got packet 0x%02X, want set compression or login success", id)
	}
	c.Send(protocol.IDLoginAcknowledged, nil)
	c.Configure()
	if len(inspect) == 0 {
		c.EnterWorld(name)
	} else {
		c.ReadJoin(inspect[0])
		c.MarkLoaded()
	}
}
