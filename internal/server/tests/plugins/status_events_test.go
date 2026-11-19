package plugins_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"sync/atomic"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// vanilla: ServerStatusPacketListenerImpl.handleStatusRequest, ServerStatus.CODEC
func TestPluginServerListFieldsAndCancellation(t *testing.T) {
	t.Parallel()
	var icon bytes.Buffer
	if err := png.Encode(&icon, image.NewRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"edit", "hide", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			cfg := servertest.BlockTestConfig()
			var calls atomic.Int32
			srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "status", Plugin: &testPlugin{enable: func(s plugin.Server) error {
				plugin.Listen(s, func(e *plugin.ServerListPingEvent) {
					calls.Add(1)
					if e.Address == nil || e.Protocol != 123 || e.Max != cfg.MaxPlayers || e.Online != 0 {
						t.Error("ping inputs")
					}
					// Safe settings reads and writes must not deadlock the callback.
					s.SetMaxPlayers(s.MaxPlayers())
					e.MOTD = plugin.Translate("custom.motd", plugin.Plain("arg")).Color("gold").Bold()
					e.MOTD.Fallback = "Custom %s"
					e.Online, e.Max = 7, 42
					e.Sample = []plugin.Profile{{ID: plugin.UUID(protocol.OfflineUUID("Example")), Name: "Example"}}
					e.Favicon = icon.Bytes()
					e.HidePlayers = mode == "hide"
					e.SetCancelled(mode == "cancel")
				})
				return nil
			}}}})
			// A held tick proves that this callback runs on the connection goroutine.
			servertest.HoldTicks(t, srv)
			c := servertest.DialServer(t, srv)
			c.Send(protocol.IDHandshake, func(b *protocol.Buffer) {
				b.PutVarInt(123)
				b.PutString("localhost")
				b.PutI16(25565)
				b.PutVarInt(protocol.NextStateStatus)
			})
			c.Send(protocol.IDStatusRequest, nil)
			if mode == "cancel" {
				if id, _, err := c.Read(); err == nil {
					t.Fatalf("cancelled ping answered with %x", id)
				}
			} else {
				r := protocol.NewReader(c.Expect(protocol.IDStatusResponse))
				var resp struct {
					Description map[string]any         `json:"description"`
					Players     protocol.StatusPlayers `json:"players"`
					Favicon     string                 `json:"favicon"`
				}
				if err := json.Unmarshal([]byte(r.ReadString(32767)), &resp); err != nil {
					t.Fatal(err)
				}
				if resp.Description["translate"] != "custom.motd" || resp.Description["fallback"] != "Custom %s" || resp.Description["color"] != "gold" || resp.Description["bold"] != true {
					t.Fatalf("motd = %+v", resp.Description)
				}
				if resp.Players.Online != 7 || resp.Players.Max != 42 {
					t.Fatalf("counts = %+v", resp.Players)
				}
				if mode == "hide" {
					if len(resp.Players.Sample) != 0 {
						t.Fatal("hidden sample")
					}
				} else if len(resp.Players.Sample) != 1 || resp.Players.Sample[0].Name != "Example" || resp.Players.Sample[0].ID != protocol.OfflineUUID("Example").String() {
					t.Fatalf("sample = %+v", resp.Players.Sample)
				}
				if resp.Favicon != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(icon.Bytes()) {
					t.Fatal("favicon")
				}
			}
			if n := calls.Load(); n != 1 {
				t.Fatalf("ping callbacks = %d", n)
			}
		})
	}
}
