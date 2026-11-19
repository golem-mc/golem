package plugins_test

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// vanilla: ServerConfigurationPacketListenerImpl.addOptionalTasks, handleResourcePackResponse
func TestPluginConfigurationPacksHoldUntilResolved(t *testing.T) {
	for _, tc := range []struct {
		name       string
		required   bool
		remove     bool
		serverPack bool
		secondPack bool
		early      bool
	}{
		{name: "required_loaded", required: true},
		{name: "optional_declined"},
		{name: "required_removed", required: true, remove: true},
		{name: "optional_removed", remove: true},
		{name: "server_and_plugin", serverPack: true},
		{name: "two_plugin_packs", secondPack: true},
		{name: "answered_before_known_packs", required: true, early: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance = config.MinDistance
			packID := protocol.OfflineUUID("plugin pack")
			otherID := protocol.OfflineUUID("other pack")
			if tc.serverPack {
				cfg.ResourcePack = "https://example.invalid/server.zip"
				cfg.ResourcePackID = otherID.String()
			}
			key := plugin.Key{Namespace: "packs", Path: "barrier"}
			srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "packs", Plugin: &testPlugin{enable: func(s plugin.Server) error {
				plugin.Listen(s, func(e *plugin.PlayerConfigureEvent) {
					e.Player.SendResourcePack(packID, "https://example.invalid/plugin.zip", "", tc.required, nil)
					if tc.secondPack {
						e.Player.SendResourcePack(otherID, "https://example.invalid/other.zip", "", false, nil)
					}
				})
				s.Messenger().Listen(key, func(p plugin.Player, data []byte) {
					if bytes.Equal(data, []byte("remove")) {
						p.RemoveResourcePack(packID)
					}
					p.SendPluginMessage(key, data)
				})
				return nil
			}}}})
			c := servertest.DialServer(t, srv)
			c.ViewAsk = config.MinDistance
			c.Login("PackGate")
			c.Expect(protocol.IDConfigPluginMessage)
			if got := protocol.NewReader(c.Expect(protocol.IDConfigResourcePackPush)).ReadUUID(); got != packID {
				t.Fatalf("plugin pack = %s, want %s", got, packID)
			}
			if tc.secondPack {
				c.Expect(protocol.IDConfigResourcePackPush)
			}
			known := c.Expect(protocol.IDKnownPacks)
			respond := func(id plugin.UUID, status plugin.PackStatus) {
				c.Send(protocol.IDConfigResourcePackIn, func(b *protocol.Buffer) {
					b.PutUUID(id)
					b.PutVarInt(int32(status))
				})
			}
			// The tick-thread echo follows all earlier connection packets; an
			// early Finish cannot hide behind a quiet-window timeout.
			barrier := func() {
				t.Helper()
				c.Send(protocol.IDConfigPluginMessageIn, func(b *protocol.Buffer) {
					b.PutString(key.String())
					b.PutRaw([]byte("barrier"))
				})
				for id, data := range c.Stream() {
					switch id {
					case protocol.IDConfigFinish:
						t.Fatal("Finish Configuration arrived with an unresolved pack")
					case protocol.IDConfigPluginMessage:
						r := protocol.NewReader(data)
						if r.ReadString(32767) == key.String() && bytes.Equal(r.Rest(), []byte("barrier")) {
							return
						}
					}
				}
			}
			if tc.early {
				respond(packID, plugin.PackLoaded)
				barrier()
			}
			c.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) { b.PutRaw(known) })
			if !tc.early {
				barrier()
				respond(packID, plugin.PackAccepted)
				respond(packID, plugin.PackDownloaded)
				respond(protocol.OfflineUUID("unknown pack"), plugin.PackLoaded)
				barrier()
				if tc.remove {
					c.Send(protocol.IDConfigPluginMessageIn, func(b *protocol.Buffer) {
						b.PutString(key.String())
						b.PutRaw([]byte("remove"))
					})
					r := protocol.NewReader(c.Expect(protocol.IDConfigResourcePackPop))
					if !r.ReadBool() || r.ReadUUID() != packID || r.Err() != nil {
						t.Fatal("configuration pack removal")
					}
				} else if tc.required {
					respond(packID, plugin.PackLoaded)
				} else {
					respond(packID, plugin.PackDeclined)
				}
				if tc.serverPack || tc.secondPack {
					barrier()
					respond(otherID, plugin.PackLoaded)
				}
			}
			c.AwaitPacket(protocol.IDConfigFinish)
			c.Send(protocol.IDConfigFinishAck, nil)
			c.Playing = true
			c.EnterWorld("PackGate")
		})
	}
}
