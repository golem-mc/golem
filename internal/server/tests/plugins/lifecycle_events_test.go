package plugins_test

import (
	"os"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: PlayerList.placeNewPlayer, remove; ServerGamePacketListenerImpl.onDisconnect
func TestPluginJoinQuitMessages(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"edit", "suppress", "default"} {
		t.Run(mode, func(t *testing.T) {
			cfg := servertest.BlockTestConfig()
			var departing plugin.Player
			joins, quits := 0, 0
			srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "lifecycle", Plugin: &testPlugin{enable: func(s plugin.Server) error {
				plugin.Listen(s, func(e *plugin.PlayerJoinEvent) {
					if e.Player.Name() != "Subject" {
						return
					}
					joins++
					if !e.Player.IsValid() || e.Player.UUID() != protocol.OfflineUUID("Subject") || len(s.Players()) != 2 || e.Message == nil || e.Message.Translate != "multiplayer.player.joined" {
						t.Error("join inputs")
					}
					departing = e.Player
					e.Player.SendPluginMessage(plugin.Key{Namespace: "lifecycle", Path: "joined"}, nil)
					switch mode {
					case "edit":
						text := plugin.Plain("Welcome Subject").Color("gold").Bold()
						e.Message = &text
					case "suppress":
						e.Message = nil
					}
				})
				plugin.Listen(s, func(e *plugin.PlayerQuitEvent) {
					if e.Player.Name() != "Subject" {
						return
					}
					quits++
					if !e.Player.IsValid() || len(s.Players()) != 2 || e.Message == nil || e.Message.Translate != "multiplayer.player.left" {
						t.Error("quit inputs")
					}
					if _, err := os.Stat(servertest.PlayerFile(cfg.LevelName, "Subject")); !os.IsNotExist(err) {
						t.Error("quit callback ran after saving")
					}
					switch mode {
					case "edit":
						text := plugin.Plain("Farewell Subject").Color("gold").Bold()
						e.Message = &text
					case "suppress":
						e.Message = nil
					}
				})
				return nil
			}}}})
			watcher := pluginJoin(t, srv, "Watcher")
			subject := servertest.DialServer(t, srv)
			subject.ViewAsk = 2
			subject.Login("Subject")
			subject.Configure()
			chunks := false
			subject.ReadJoin(func(id int32, _ []byte) {
				chunks = chunks || id == protocol.IDPlayChunkData
				if id == protocol.IDPlayPluginMessage && !chunks {
					t.Error("join callback sent a packet before the initial chunks")
				}
			})
			servertest.HoldTicks(t, srv)
			defer servertest.ReleaseTicks(t, srv)
			servertest.UntilTicks(t, srv, 4, "join callback", func() bool { return joins == 1 })
			if mode == "suppress" {
				watcher.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySystemChat))
			} else {
				data := watcher.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
				if mode == "default" {
					checkLifecycleTranslation(t, data, "multiplayer.player.joined")
				} else {
					text, ok := protocol.ReadText(protocol.NewReader(data))
					if !ok || text.Plain() != "Welcome Subject" || !lifecycleStyle(text, "gold", true) {
						t.Fatalf("join message = %+v", text)
					}
				}
			}
			subject.Raw.Close()
			// The free loop lets the connection enqueue its quit callback before
			// we hold it again; its completion is the synchronization boundary.
			servertest.ReleaseTicks(t, srv)
			subject.AwaitServed()
			servertest.HoldTicks(t, srv)
			if quits != 1 || departing.IsValid() {
				t.Fatalf("quits = %d, valid = %v", quits, departing.IsValid())
			}
			if mode == "suppress" {
				watcher.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySystemChat))
			} else {
				data := watcher.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
				if mode == "default" {
					checkLifecycleTranslation(t, data, "multiplayer.player.left")
				} else {
					text, ok := protocol.ReadText(protocol.NewReader(data))
					if !ok || text.Plain() != "Farewell Subject" || !lifecycleStyle(text, "gold", true) {
						t.Fatalf("quit message = %+v", text)
					}
				}
			}
		})
	}
}

// vanilla: PlayerList.disconnectAllPlayersWithProfile
func TestPluginDuplicateLoginKickCancellation(t *testing.T) {
	t.Parallel()
	var cancel = true
	kicks := 0
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "duplicate", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.PlayerKickEvent) {
			kicks++
			if e.Reason.Translate != "multiplayer.disconnect.duplicate_login" || e.Player.Name() != "Subject" {
				t.Error("duplicate kick inputs")
			}
			_ = s.Players()
			e.SetCancelled(cancel)
			message := plugin.Plain("Session replaced")
			e.Message = &message
		})
		return nil
	}}}})
	watcher := pluginJoin(t, srv, "Watcher")
	old := pluginJoin(t, srv, "Subject")
	servertest.HoldTicks(t, srv)
	defer servertest.ReleaseTicks(t, srv)
	watcher.Settle()
	servertest.ReleaseTicks(t, srv)
	refused := servertest.DialServer(t, srv)
	refused.Login("Subject")
	refused.Configure()
	servertest.ExpectDisconnect(t, refused, protocol.Text{Translate: "multiplayer.disconnect.duplicate_login"})
	refused.AwaitServed()
	servertest.HoldTicks(t, srv)
	if kicks != 1 || srv.PlayerCount() != 2 {
		t.Fatal("duplicate login cancellation did not preserve the old session")
	}
	old.Settle()
	old.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayDisconnect))
	cancel = false
	servertest.ReleaseTicks(t, srv)
	pluginJoin(t, srv, "Subject")
	old.AwaitPacket(protocol.IDPlayDisconnect)
	old.AwaitServed()
	servertest.HoldTicks(t, srv)
	if kicks != 2 || srv.PlayerCount() != 2 {
		t.Fatal("duplicate login did not retry its cancelled kick")
	}
	text, ok := protocol.ReadText(protocol.NewReader(watcher.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)))
	if !ok || text.Plain() != "Session replaced" {
		t.Fatalf("kick message without quit listener = %+v", text)
	}
}

// vanilla: ServerCommonPacketListenerImpl.disconnect
func TestPluginKickCancellationAndMessages(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"edit", "suppress", "quit-edit"} {
		t.Run(mode, func(t *testing.T) {
			var host plugin.Server
			kicks, quits := 0, 0
			srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "kick", Plugin: &testPlugin{enable: func(s plugin.Server) error {
				host = s
				plugin.Listen(s, func(e *plugin.PlayerKickEvent) {
					kicks++
					if !e.Player.IsValid() || e.Player.Name() != "Subject" || len(s.Players()) != 2 || e.Message == nil || e.Message.Translate != "multiplayer.player.left" {
						t.Error("kick inputs")
					}
					if e.Reason.Literal == "cancel" {
						e.SetCancelled(true)
						return
					}
					e.Reason = plugin.Plain("Changed reason").Color("red").Bold()
					if mode == "suppress" {
						e.Message = nil
					} else {
						text := plugin.Plain("Removed Subject").Color("gold")
						e.Message = &text
					}
					// A recursive kick must not emit a second event or disconnect.
					e.Player.Kick(plugin.Plain("recursive"))
				})
				plugin.Listen(s, func(e *plugin.PlayerQuitEvent) {
					if e.Player.Name() == "Subject" {
						quits++
						if mode == "suppress" && e.Message != nil || mode != "suppress" && (e.Message == nil || e.Message.Literal != "Removed Subject") {
							t.Error("quit did not receive kick message")
						}
						if mode == "quit-edit" {
							text := plugin.Plain("Quit override")
							e.Message = &text
						}
					}
				})
				return nil
			}}}})
			watcher := pluginJoin(t, srv, "Watcher")
			subject := pluginJoin(t, srv, "Subject")
			servertest.HoldTicks(t, srv)
			defer servertest.ReleaseTicks(t, srv)
			watcher.Settle()
			subject.Settle()
			p, _ := host.PlayerNamed("Subject")
			p.Kick(plugin.Plain("cancel"))
			if kicks != 0 {
				t.Fatal("kick callback ran off the tick")
			}
			servertest.RunTicks(t, srv, 1)
			subject.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayDisconnect))
			if kicks != 1 || quits != 0 || !p.IsValid() {
				t.Fatal("cancelled kick removed the player")
			}
			subject.Chat("still here")
			subject.Settle()
			servertest.RunTicks(t, srv, 1)
			watcher.ExpectWithinTicks(srv, 0, protocol.IDPlayPlayerChat)
			p.Kick(plugin.Plain("accepted"))
			servertest.RunTicks(t, srv, 1)
			text, ok := protocol.ReadText(protocol.NewReader(subject.AwaitPacket(protocol.IDPlayDisconnect)))
			if !ok || text.Plain() != "Changed reason" || !lifecycleStyle(text, "red", true) {
				t.Fatalf("kick reason = %+v", text)
			}
			servertest.ReleaseTicks(t, srv)
			subject.AwaitServed()
			servertest.HoldTicks(t, srv)
			if kicks != 2 || quits != 1 || p.IsValid() {
				t.Fatalf("kicks = %d, quits = %d, valid = %v", kicks, quits, p.IsValid())
			}
			if mode == "suppress" {
				watcher.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySystemChat))
			} else {
				text, ok = protocol.ReadText(protocol.NewReader(watcher.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)))
				want := "Removed Subject"
				if mode == "quit-edit" {
					want = "Quit override"
				}
				if !ok || text.Plain() != want {
					t.Fatalf("departure = %+v, want %s", text, want)
				}
			}
		})
	}
}

func TestPluginConfigurationKickCancellation(t *testing.T) {
	t.Parallel()
	calls := 0
	var saved plugin.Player
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "setup-kick", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.PlayerConfigureEvent) {
			saved = e.Player
			e.Player.Kick(plugin.Plain("cancel"))
		})
		plugin.Listen(s, func(e *plugin.PlayerKickEvent) {
			calls++
			if !e.Player.IsValid() || e.Player.Name() != "Setup" || e.Message != nil || len(s.Players()) != 0 {
				t.Error("configuration kick inputs")
			}
			if e.Reason.Literal == "cancel" {
				e.SetCancelled(true)
				e.Player.SendPluginMessage(plugin.Key{Namespace: "setup", Path: "kept"}, []byte{7})
				s.Scheduler().RunTask(func() { saved.Kick(plugin.Plain("accepted")) })
			} else {
				e.Reason = plugin.Plain("Configuration refused").Color("red")
			}
		})
		return nil
	}}}})
	c := servertest.DialServer(t, srv)
	c.Login("Setup")
	kept := false
	for {
		id, data, err := c.Read()
		if err != nil {
			t.Fatal(err)
		}
		if id == protocol.IDConfigPluginMessage {
			r := protocol.NewReader(data)
			if r.ReadString(32767) == "setup:kept" {
				kept = true
			}
		}
		if id == protocol.IDConfigDisconnect {
			text, ok := protocol.ReadText(protocol.NewReader(data))
			if !kept || !ok || text.Plain() != "Configuration refused" || !lifecycleStyle(text, "red", false) {
				t.Fatalf("configuration kick: kept=%v, reason=%+v", kept, text)
			}
			break
		}
	}
	c.AwaitServed()
	servertest.HoldTicks(t, srv)
	if calls != 2 || saved.IsValid() || srv.PlayerCount() != 0 {
		t.Fatal("configuration kick did not finish the connection")
	}
}

// lifecycleConfigureToFinish leaves the finish acknowledgement to the caller.
func lifecycleConfigureToFinish(c *servertest.Client) {
	c.T.Helper()
	c.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutString("minecraft")
		b.PutString("core")
		b.PutString(registry.Version)
	})
	for {
		id, data, err := c.Read()
		if err != nil {
			c.T.Fatal(err)
		}
		switch id {
		case protocol.IDConfigFinish:
			return
		case protocol.IDRegistryData, protocol.IDFeatureFlags, protocol.IDConfigUpdateTags:
		case protocol.IDConfigKeepAlive:
			c.Send(protocol.IDConfigKeepAliveIn, func(b *protocol.Buffer) { b.PutRaw(data) })
		default:
			c.T.Fatalf("unexpected configuration packet 0x%02X", id)
		}
	}
}

func lifecycleStyle(text protocol.Text, color string, bold bool) bool {
	tag, ok := nbt.CompoundView(protocol.TextTag(text))
	return ok && tag["color"] == nbt.String(color) && nbt.BoolField(tag, "bold") == bold
}

func checkLifecycleTranslation(t *testing.T, data []byte, key string) {
	t.Helper()
	tag, _, err := nbt.DecodeNetwork(data)
	fields, ok := nbt.CompoundView(tag)
	if err != nil || !ok || fields["translate"] != nbt.String(key) || fields["color"] != nbt.String("yellow") {
		t.Fatalf("announcement = %+v, %v", tag, err)
	}
}
