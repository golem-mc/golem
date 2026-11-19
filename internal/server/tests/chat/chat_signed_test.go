package chat

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/google/uuid"
)

type chatKeysFunc func(context.Context) (*chatsecurity.KeySet, error)

func (f chatKeysFunc) Keys(ctx context.Context) (*chatsecurity.KeySet, error) { return f(ctx) }

func installChatSession(c *servertest.Client, data chatsecurity.SessionData) {
	c.T.Helper()
	c.Send(protocol.IDPlayPlayerSession, func(b *protocol.Buffer) {
		b.PutUUID(data.ID)
		b.PutI64(data.ExpiresAt)
		b.PutBytes(data.PublicKey)
		b.PutBytes(data.Certificate)
	})
}

func TestTypedSignedChatAndAcknowledgements(t *testing.T) {
	for _, enforce := range []bool{true, false} {
		t.Run(fmt.Sprint(enforce), func(t *testing.T) {
			profile := protocol.OfflineUUID("Testificate")
			key, session, roots := servertest.SignedSession(t, profile)
			cfg := config.Default()
			cfg.EnforceSecureProfile = enforce
			cfg.ChatSpamThresholdSeconds = 0
			var srv *server.Server
			c := onlineChatClient(t, cfg, func(s *server.Server) {
				srv = s
				s.SetChatKeyProvider(chatKeysFunc(func(context.Context) (*chatsecurity.KeySet, error) { return roots, nil }))
			})
			watcher := servertest.DialServer(t, srv)
			servertest.CompleteOnlineChatLogin(watcher, "Watcher")
			c.ExpectPlayerAdded("Watcher")
			installChatSession(c, session)
			for _, recipient := range []*servertest.Client{c, watcher} {
				r := protocol.NewReader(recipient.AwaitPlay(protocol.IDPlayPlayerInfoUpdate))
				if r.ReadU8() != 2 || r.ReadVarInt() != 1 || r.ReadUUID() != profile || !r.ReadBool() || r.ReadUUID() != session.ID || r.ReadI64() != session.ExpiresAt || !bytes.Equal(r.ReadBytes(512), session.PublicKey) || !bytes.Equal(r.ReadBytes(4096), session.Certificate) || r.Err() != nil || r.Remaining() != 0 {
					t.Fatal("invalid INITIALIZE_CHAT")
				}
			}
			var seen []chatsecurity.Signature
			for i := range 25 {
				body := chatsecurity.NewBody(fmt.Sprintf("héllo  世界 %d", i), time.Now().UnixMilli(), int64(i+17), seen)
				digest := sha256.Sum256(servertest.SignedChatBytes(profile, session.ID, int32(i), body))
				signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
				if err != nil {
					t.Fatal(err)
				}
				offset := int32(0)
				if i > 0 {
					offset = 1
				}
				if i%2 == 1 {
					c.Send(protocol.IDPlayChatAck, func(b *protocol.Buffer) { b.PutVarInt(offset) })
					offset = 0
				}
				c.Send(protocol.IDPlayChatMessage, func(b *protocol.Buffer) {
					b.PutString(body.Content())
					b.PutI64(body.Timestamp())
					b.PutI64(body.Salt())
					b.PutBool(true)
					b.PutRaw(signature)
					b.PutVarInt(offset)
					bits := uint32((1<<len(seen))-1) << (20 - len(seen))
					for j := range 3 {
						b.PutU8(byte(bits >> (j * 8)))
					}
					b.PutU8(chatsecurity.Checksum(seen))
				})
				for _, recipient := range []*servertest.Client{c, watcher} {
					r := protocol.NewReader(recipient.AwaitPlay(protocol.IDPlayPlayerChat))
					if r.ReadVarInt() != int32(i) || r.ReadUUID() != profile || r.ReadVarInt() != int32(i) || !r.ReadBool() {
						t.Fatal("invalid chat link")
					}
					relaySignature := make([]byte, 256)
					for j := range relaySignature {
						relaySignature[j] = r.ReadU8()
					}
					content, timestamp, salt := r.ReadString(256), r.ReadI64(), r.ReadI64()
					var relaySeen []chatsecurity.Signature
					for n := r.ReadVarInt(); n > 0; n-- {
						if r.ReadVarInt() != 0 {
							t.Fatal("compressed signature")
						}
						var sig chatsecurity.Signature
						for j := range sig {
							sig[j] = r.ReadU8()
						}
						relaySeen = append(relaySeen, sig)
					}
					relayBody := chatsecurity.NewBody(content, timestamp, salt, relaySeen)
					relayDigest := sha256.Sum256(servertest.SignedChatBytes(profile, session.ID, int32(i), relayBody))
					if r.Err() != nil || rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, relayDigest[:], relaySignature) != nil {
						t.Fatal("relayed chat fails independent verification")
					}
				}
				seen = append(seen, *(*chatsecurity.Signature)(signature))
				if len(seen) > 20 {
					seen = seen[1:]
				}
			}
			// Installed sessions verify even when enforcement is disabled.
			c.Send(protocol.IDPlayChatMessage, func(b *protocol.Buffer) {
				b.PutString("unsigned after session")
				b.PutI64(time.Now().UnixMilli())
				b.PutI64(0)
				b.PutBool(false)
				b.PutVarInt(1)
				b.PutRaw([]byte{255, 255, 15})
				b.PutU8(chatsecurity.Checksum(seen))
			})
			c.ExpectSystemText(protocol.Text{Translate: "chat.disabled.missingProfileKey", Color: protocol.ColorRed})
			for _, failure := range []string{"chat.disabled.invalid_signature", "chat.disabled.chain_broken"} {
				c.Send(protocol.IDPlayChatMessage, func(b *protocol.Buffer) {
					b.PutString("tampered")
					b.PutI64(time.Now().UnixMilli())
					b.PutI64(0)
					b.PutBool(true)
					b.PutRaw(make([]byte, 256))
					b.PutVarInt(0)
					b.PutRaw([]byte{255, 255, 15})
					b.PutU8(chatsecurity.Checksum(seen))
				})
				c.ExpectSystemText(protocol.Text{Translate: failure, Color: protocol.ColorRed})
			}
		})
	}
}

func TestChatSessionRejectsInvalidTrust(t *testing.T) {
	for _, kind := range []string{"DER", "certificate", "profile", "absent trust"} {
		t.Run(kind, func(t *testing.T) {
			profile := protocol.OfflineUUID("Testificate")
			if kind == "profile" {
				profile = uuid.New()
			}
			_, session, roots := servertest.SignedSession(t, profile)
			if kind == "DER" {
				session.PublicKey = []byte("bad")
			}
			if kind == "certificate" {
				session.Certificate[0] ^= 1
			}
			if kind == "absent trust" {
				roots = nil
			}
			c := onlineChatClient(t, config.Default(), func(s *server.Server) {
				s.SetChatKeyProvider(chatKeysFunc(func(context.Context) (*chatsecurity.KeySet, error) { return roots, nil }))
			})
			installChatSession(c, session)
			if kind == "absent trust" {
				c.Chat("refused")
				c.ExpectSystemText(protocol.Text{Translate: "chat.disabled.missingProfileKey", Color: protocol.ColorRed})
			} else {
				servertest.ExpectDisconnect(t, c, protocol.Text{Translate: "multiplayer.disconnect.invalid_public_key_signature"})
			}
		})
	}
}
