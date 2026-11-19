package plugins_test

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// vanilla: ServerStatusPacketListenerImpl.handleStatusRequest, ServerStatus.CODEC
func TestPluginServerListFieldsAndCancellation(t *testing.T) {
	var icon bytes.Buffer
	if err := png.Encode(&icon, image.NewRGBA(image.Rect(0, 0, 64, 64))); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"edit", "hide", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			cfg := servertest.BlockTestConfig()
			calls := 0
			srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "status", Plugin: &testPlugin{enable: func(s plugin.Server) error {
				plugin.Listen(s, func(e *plugin.ServerListPingEvent) {
					calls++
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
			if calls != 1 {
				t.Fatalf("ping callbacks = %d", calls)
			}
		})
	}
}

// The server-list client retries this pre-1.7 request after a modern ping fails.
func TestPluginLegacyPingFieldsAndCancellation(t *testing.T) {
	// ServerStatusPinger's MC|PingHost request, protocol 127, localhost:25565.
	fallback, err := hex.DecodeString("fe01fa000b004d0043007c00500069006e00670048006f0073007400197f0009006c006f00630061006c0068006f00730074000063dd")
	if err != nil {
		t.Fatal(err)
	}
	older := bytes.Clone(fallback)
	older[29] = 74 // Protocol byte after the MC|PingHost channel and payload size.
	for _, cancel := range []bool{false, true} {
		for _, tc := range []struct {
			request []byte
			version int
		}{{[]byte{0xFE}, 0}, {[]byte{0xFE, 1}, 0}, {fallback, 127}, {older, 74}} {
			request := tc.request
			kind, _ := protocol.ParseLegacyPing(request)
			t.Run(fmt.Sprintf("cancel=%t/request=%x", cancel, request), func(t *testing.T) {
				calls := 0
				srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "legacy", Plugin: &testPlugin{enable: func(s plugin.Server) error {
					plugin.Listen(s, func(e *plugin.ServerListPingEvent) {
						calls++
						if e.Address == nil {
							t.Error("missing ping address")
						}
						wantProtocol := tc.version
						if cancel && calls == 1 {
							wantProtocol = protocol.Version // The preceding modern request.
						}
						if e.Protocol != wantProtocol {
							t.Errorf("ping protocol = %d, want %d", e.Protocol, wantProtocol)
						}
						e.MOTD = plugin.Plain("Custom legacy").Color("gold")
						e.Online, e.Max = 7, 42
						e.SetCancelled(cancel)
					})
					return nil
				}}}})
				servertest.HoldTicks(t, srv)
				wantCalls := 1
				if cancel {
					modern := servertest.DialServer(t, srv)
					modern.Handshake(protocol.NextStateStatus)
					modern.Send(protocol.IDStatusRequest, nil)
					if id, _, err := modern.Read(); err == nil {
						t.Fatalf("cancelled modern ping answered with %x", id)
					}
					wantCalls++
				}
				c := servertest.DialServer(t, srv)
				if _, err := c.Raw.Write(request); err != nil {
					t.Fatal(err)
				}
				if conn, ok := c.Raw.(*net.TCPConn); ok {
					_ = conn.CloseWrite()
				}
				got, err := io.ReadAll(c.Raw)
				if err != nil {
					t.Fatal(err)
				}
				if cancel {
					if len(got) != 0 {
						t.Fatalf("cancelled legacy ping returned %d bytes", len(got))
					}
				} else if want := protocol.LegacyPingReply(kind, "Custom legacy", 7, 42); !bytes.Equal(got, want) {
					t.Fatalf("legacy reply = %x, want %x", got, want)
				}
				if calls != wantCalls {
					t.Fatalf("ping callbacks = %d, want %d", calls, wantCalls)
				}
			})
		}
	}
}
