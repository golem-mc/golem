package plugins_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
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
)

// vanilla: ServerGamePacketListenerImpl.chatMessageChain, SignedMessageValidator.KeyBased
func TestPluginChatOrdersSignedCommandsAndFailures(t *testing.T) {
	t.Parallel()
	profile := protocol.OfflineUUID("Signed")
	key, session, roots := servertest.SignedSession(t, profile)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": strings.ReplaceAll(profile.String(), "-", ""), "name": "Signed", "properties": []any{}})
	}))
	defer stub.Close()
	cfg := servertest.BlockTestConfig()
	cfg.OnlineMode, cfg.EnforceSecureProfile = true, true
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "ordering", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.PlayerChatEvent) { _ = s.Players() })
		plugin.Listen(s, func(e *plugin.BroadcastMessageEvent) { _ = s.Players() })
		return nil
	}}}}, servertest.OpsListing(t, "Signed"))
	srv.SetSessionServer(stub.URL)
	srv.SetChatKeyProvider(pluginChatKeyFunc(func(context.Context) (*chatsecurity.KeySet, error) { return roots, nil }))
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
	link := int32(0)
	send := func(packet int32, input, content string, signed bool) {
		body := chatsecurity.NewBody(content, time.Now().UnixMilli(), int64(link+17), nil)
		hash := sha256.Sum256(servertest.SignedChatBytes(profile, session.ID, link, body))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
		if err != nil {
			t.Fatal(err)
		}
		c.Send(packet, func(b *protocol.Buffer) {
			b.PutString(input)
			if packet == protocol.IDPlayChatCommand {
				return
			}
			b.PutI64(body.Timestamp())
			b.PutI64(body.Salt())
			if packet == protocol.IDPlaySignedChatCommand {
				if signed {
					b.PutVarInt(1)
					if strings.HasPrefix(input, "me ") {
						b.PutString("action")
					} else {
						b.PutString("message")
					}
					b.PutRaw(sig)
				} else {
					b.PutVarInt(0)
				}
			} else {
				b.PutBool(true)
				b.PutRaw(sig)
			}
			b.PutVarInt(0)
			b.PutRaw([]byte{0, 0, 0, 0})
		})
		if signed {
			link++
		}
	}
	wireIndex := int32(0)
	for _, tc := range []struct {
		name, command, content string
		packet                 int32
		signed, failure, say   bool
	}{
		{name: "me", command: "me action", content: "action", packet: protocol.IDPlaySignedChatCommand, signed: true},
		{name: "say", command: "say announcement", content: "announcement", packet: protocol.IDPlaySignedChatCommand, signed: true, say: true},
		{name: "unknown", command: "nosuchcommand", packet: protocol.IDPlayChatCommand, failure: true},
		{name: "missing signature", command: "me unsigned", packet: protocol.IDPlaySignedChatCommand, failure: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			firstLink := link
			send(protocol.IDPlayChatMessage, "first", "first", true)
			c.Settle() // The first relay is pending while the command arrives.
			send(tc.packet, tc.command, tc.content, tc.signed)
			servertest.RunTicks(t, srv, 1)
			var packets []struct {
				id   int32
				data []byte
			}
			collect := func(id int32, data []byte) {
				if id == protocol.IDPlayPlayerChat || id == protocol.IDPlaySystemChat {
					packets = append(packets, struct {
						id   int32
						data []byte
					}{id, data})
				}
			}
			c.SettleAndCollect(collect)
			if tc.say {
				servertest.RunTicks(t, srv, 1)
				c.SettleAndCollect(collect)
			}
			wantCount := 2
			if tc.name == "unknown" {
				wantCount = 3
			}
			if len(packets) != wantCount {
				t.Fatalf("chat packets = %d, want %d", len(packets), wantCount)
			}
			for i, p := range packets {
				if i > 0 && tc.failure {
					if p.id != protocol.IDPlaySystemChat {
						t.Fatal("command failure is missing")
					}
					if i == 1 {
						want := "chat.disabled.missingProfileKey"
						if tc.name == "unknown" {
							want = "command.unknown.command"
						}
						if !bytes.Contains(p.data, []byte(want)) {
							t.Fatalf("command failure lacks %s", want)
						}
					}
					continue
				}
				if p.id != protocol.IDPlayPlayerChat {
					text, _ := protocol.ReadText(protocol.NewReader(p.data))
					t.Fatalf("packet %d: command feedback overtook typed chat: %s", i, text.Plain())
				}
				r := protocol.NewReader(p.data)
				if r.ReadVarInt() != wireIndex || r.ReadUUID() != profile || r.ReadVarInt() != firstLink+int32(i) || !r.ReadBool() {
					t.Fatal("signed links relayed out of order")
				}
				sig := make([]byte, 256)
				for j := range sig {
					sig[j] = r.ReadU8()
				}
				body := chatsecurity.NewBody(r.ReadString(256), r.ReadI64(), r.ReadI64(), nil)
				hash := sha256.Sum256(servertest.SignedChatBytes(profile, session.ID, firstLink+int32(i), body))
				if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, hash[:], sig); err != nil {
					t.Fatal(err)
				}
				wireIndex++
			}
		})
		if t.Failed() {
			return
		}
	}
}
