package plugins_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

type pluginChatKeys struct{ keys *chatsecurity.KeySet }

func (p pluginChatKeys) Keys(context.Context) (*chatsecurity.KeySet, error) { return p.keys, nil }

type pluginChatKeyFunc func(context.Context) (*chatsecurity.KeySet, error)

func (f pluginChatKeyFunc) Keys(ctx context.Context) (*chatsecurity.KeySet, error) { return f(ctx) }

// vanilla: SignedMessageChain.Decoder, PlayerList.broadcastChatMessage
func TestPluginChatPreservesSignedChain(t *testing.T) {
	profile := protocol.OfflineUUID("Signed")
	service, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	key, session, roots := servertest.SignedSession(t, profile, service)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": strings.ReplaceAll(profile.String(), "-", ""), "name": "Signed", "properties": []any{}})
	}))
	defer stub.Close()
	cfg := servertest.BlockTestConfig()
	cfg.OnlineMode = true
	cfg.EnforceSecureProfile = true
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	calls := 0
	broadcasts := 0
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "signed", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.PlayerChatEvent) {
			calls++
			switch e.Message {
			case "edit":
				e.Message = "changed"
			case "cancel":
				e.SetCancelled(true)
			}
		})

		plugin.Listen(s, func(e *plugin.BroadcastMessageEvent) {
			broadcasts++
			if e.Message.Args[1].Literal == "say-edit" {
				e.Message.Args[1].Literal = "changed"
			}
			if e.Message.Args[1].Literal == "say-cancel" {
				e.SetCancelled(true)
			}
		})
		return nil
	}}}}, servertest.OpsListing(t, "Signed"))
	srv.SetSessionServer(stub.URL)
	srv.SetChatKeyProvider(pluginChatKeys{roots})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	servertest.CompleteOnlineChatLogin(c, "Signed")
	c.Send(protocol.IDPlayPlayerSession, func(b *protocol.Buffer) {
		b.PutUUID(session.ID)
		b.PutI64(session.ExpiresAt)
		b.PutBytes(session.PublicKey)
		b.PutBytes(session.Certificate)
	})
	c.AwaitPlay(protocol.IDPlayPlayerInfoUpdate)
	servertest.HoldTicks(t, srv)
	c.Settle()
	wireIndex := int32(0)
	for i, message := range []string{"first", "edit", "cancel", "last", "say", "say-edit", "say-cancel", "replacement"} {
		timestamp, salt := time.Now().UnixMilli(), int64(i+17)
		body := chatsecurity.NewBody(message, timestamp, salt, nil)
		hash := sha256.Sum256(servertest.SignedChatBytes(profile, session.ID, int32(i), body))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
		if err != nil {
			t.Fatal(err)
		}

		packet := int32(protocol.IDPlayChatMessage)
		if i >= 4 && i <= 6 {
			packet = protocol.IDPlaySignedChatCommand
		}
		c.Send(packet, func(b *protocol.Buffer) {
			if i >= 4 && i <= 6 {
				b.PutString("say " + message)
			} else {
				b.PutString(message)
			}
			b.PutI64(timestamp)
			b.PutI64(salt)
			if i >= 4 && i <= 6 {
				b.PutVarInt(1)
				b.PutString("message")
			} else {
				b.PutBool(true)
			}
			b.PutRaw(signature)
			b.PutVarInt(0)
			b.PutRaw([]byte{0, 0, 0, 0})
		})

		c.Settle()
		if message == "replacement" {
			entered := make(chan struct{})
			srv.SetChatKeyProvider(pluginChatKeyFunc(func(context.Context) (*chatsecurity.KeySet, error) { close(entered); return roots, nil }))
			renewed := session.ExpiresAt + 1000
			renewal := append([]byte{}, profile[:]...)
			renewal = binary.BigEndian.AppendUint64(renewal, uint64(renewed))
			renewal = append(renewal, session.PublicKey...)
			renewalHash := sha1.Sum(renewal)
			renewalCert, err := rsa.SignPKCS1v15(rand.Reader, service, crypto.SHA1, renewalHash[:])
			if err != nil {
				t.Fatal(err)
			}
			c.Send(protocol.IDPlayPlayerSession, func(b *protocol.Buffer) {
				b.PutUUID(uuid.New())
				b.PutI64(renewed)
				b.PutBytes(session.PublicKey)
				b.PutBytes(renewalCert)
			})
			select {
			case <-entered:
			case <-time.After(servertest.ExpectTimeout):
				t.Fatal("replacement session never reached validation")
			}
		}
		servertest.RunTicks(t, srv, 1)
		var chatPacket []byte
		if message == "replacement" {
			// Read raw packet order: a new session before this relay invalidates its old-session signature.
			for {
				id, data, err := c.Read()
				if err != nil {
					t.Fatal(err)
				}
				if id == protocol.IDPlayPlayerInfoUpdate {
					t.Fatal("replacement session published before old signed chat")
				}
				if id == protocol.IDPlayPlayerChat {
					chatPacket = data
					break
				}
			}
		}
		switch message {
		case "cancel", "say-cancel":
			c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayPlayerChat, protocol.IDPlaySystemChat))
		case "edit", "say-edit":
			want := "<Signed> changed"
			if i >= 4 && i <= 6 {
				want = "[Signed] changed"
			}
			assertPluginSystemChat(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), want)
			c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayPlayerChat))
		default:
			data := chatPacket
			if data == nil {
				data = c.ExpectWithinTicks(srv, 0, protocol.IDPlayPlayerChat)
			}
			r := protocol.NewReader(data)
			if r.ReadVarInt() != wireIndex || r.ReadUUID() != profile || r.ReadVarInt() != int32(i) || !r.ReadBool() {
				t.Fatal("signed link changed")
			}
			actual := make([]byte, 256)
			for j := range actual {
				actual[j] = r.ReadU8()
			}
			if !bytes.Equal(actual, signature) {
				t.Fatal("signature changed")
			}
			if r.ReadString(256) != message || r.ReadI64() != timestamp || r.ReadI64() != salt || r.ReadVarInt() != 0 || r.ReadBool() || r.ReadVarInt() != 0 {
				t.Fatal("signed body changed or unexpected unsigned content")
			}
			if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, hash[:], actual); err != nil {
				t.Fatal(err)
			}
			wireIndex++
		}
		wantCalls := min(i+1, 4)
		if message == "replacement" {
			wantCalls++
		}
		if calls != wantCalls || broadcasts != min(max(i-3, 0), 3) {
			t.Fatalf("signed callbacks = %d", calls)
		}
		if message == "replacement" {
			servertest.ReleaseTicks(t, srv)
			c.AwaitPlay(protocol.IDPlayPlayerInfoUpdate)
		}
	}
}
