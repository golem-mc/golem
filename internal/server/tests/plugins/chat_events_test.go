package plugins_test

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// vanilla: ServerGamePacketListenerImpl.handleChat, PlayerList.broadcastChatMessage
func TestPluginChatEditsAndCancellation(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	var host plugin.Server
	calls := 0
	cookies := 0
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "chat", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		host = s
		plugin.Listen(s, func(e *plugin.PlayerChatEvent) {
			calls++
			if e.Player.Name() != "Alice" || len(e.Recipients) != 2 {
				t.Error("chat inputs")
			}
			switch e.Message {
			case "cancel":
				e.SetCancelled(true)
			case "edit":
				e.Message = "changed"
				e.Player.RequestCookie(plugin.Key{Namespace: "chat", Path: "cookie"}, func(data []byte, present bool) {
					if !present || len(data) != 1 || data[0] != 7 {
						t.Error("event player cookie callback")
					}
					cookies++
				})
			case "empty":
				e.Recipients = nil
			case "trim":
				e.Recipients = []plugin.Player{e.Recipients[0], e.Recipients[0]}
			case "panic":
				e.Format = func(plugin.Player, plugin.Text, plugin.Player) plugin.Text { panic("renderer failed") }
			case "format":
				e.Format = func(sender plugin.Player, message plugin.Text, viewer plugin.Player) plugin.Text {
					// These server lookups must be safe inside callbacks.
					if _, ok := host.Player(sender.UUID()); !ok {
						t.Error("sender lookup")
					}
					return plugin.Plain(viewer.Name() + ": ").Append(message).Color("gold")
				}
			}
		})
		return nil
	}}}})
	alice := servertest.DialServer(t, srv)
	alice.ViewAsk = config.MinDistance
	alice.Join("Alice")
	bob := servertest.DialServer(t, srv)
	bob.ViewAsk = config.MinDistance
	bob.Join("Bob")
	servertest.HoldTicks(t, srv)
	alice.Settle()
	bob.Settle()
	for _, message := range []string{"cancel", "empty", "edit", "trim", "format", "panic", "unchanged"} {
		before := calls
		alice.Chat(message)
		alice.Settle()
		if calls != before {
			t.Fatal("chat callback ran on connection goroutine")
		}
		servertest.RunTicks(t, srv, 1)
		if calls != before+1 {
			t.Fatal("missing chat callback")
		}
		switch message {
		case "cancel", "empty":
			for _, c := range []*servertest.Client{alice, bob} {
				c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayPlayerChat, protocol.IDPlaySystemChat))
			}

		case "edit", "panic":
			if message == "edit" {
				r := protocol.NewReader(alice.ExpectWithinTicks(srv, 0, protocol.IDPlayCookieRequest))
				if r.ReadString(32767) != "chat:cookie" {
					t.Fatal("event player cookie request")
				}
			}

			want := "<Alice> changed"
			if message == "panic" {
				want = "<Alice> panic"
			}
			for _, c := range []*servertest.Client{alice, bob} {
				assertPluginSystemChat(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), want)
			}
			if message == "edit" {
				alice.Send(protocol.IDPlayCookieResponse, func(b *protocol.Buffer) { b.PutString("chat:cookie"); b.PutBool(true); b.PutBytes([]byte{7}) })
				alice.Settle()
				servertest.RunTicks(t, srv, 1)
				if cookies != 1 {
					t.Fatal("event player lost cookie ownership")
				}
			}
		case "format":
			// Each recipient sees their own rendered name.
			got := 0
			for _, c := range []*servertest.Client{alice, bob} {
				c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
					if id == protocol.IDPlaySystemChat {
						got++
						text, ok := protocol.ReadText(protocol.NewReader(data))
						if !ok || text.Color != "gold" || (text.Plain() != "Alice: format" && text.Plain() != "Bob: format") {
							t.Errorf("formatted text = %+v", text)
						}
					}
					return false
				})
			}
			if got != 2 {
				t.Fatalf("formatted recipients = %d, want 2", got)
			}

		case "trim":
			got := 0
			for _, c := range []*servertest.Client{alice, bob} {
				c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
					if id == protocol.IDPlayPlayerChat {
						got++
					}
					return false
				})
			}
			if got != 1 {
				t.Fatalf("trimmed recipients = %d, want 1", got)
			}
		case "unchanged":
			for _, c := range []*servertest.Client{alice, bob} {
				servertest.ParsePlayerChat(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlayPlayerChat), "minecraft:chat", "Alice", protocol.OfflineUUID("Alice"), message, protocol.Text{Text: message}, "", servertest.ServerStampedChatTimestamp)
			}
		}
	}
}

func assertPluginSystemChat(t *testing.T, data []byte, want string) {
	t.Helper()
	r := protocol.NewReader(data)
	text, ok := protocol.ReadComponentText(r)
	if !ok || text.Plain() != want || r.ReadBool() || r.Err() != nil || r.Remaining() != 0 {
		t.Fatalf("system chat = %+v, want %q (%v)", text, want, r.Err())
	}
}

// vanilla: SayCommand, PlayerList.broadcastSystemMessage
func TestPluginBroadcastEditsAndCancellation(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	var host plugin.Server
	cancel := false
	nested := false
	calls := 0
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "broadcast", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		host = s
		plugin.Listen(s, func(e *plugin.BroadcastMessageEvent) {
			calls++
			if e.Message.Literal != "original" && e.Message.Translate != "chat.type.announcement" {
				t.Errorf("broadcast input = %+v", e.Message)
			}
			if len(s.Players()) != 1 {
				t.Error("broadcast lookup")
			}

			if nested && len(e.Message.Args) > 1 {
				e.Message.Args[1].Literal = "nested"
			} else {
				e.Message = plugin.Plain("replacement").Color("green")
			}
			e.SetCancelled(cancel)
		})
		return nil
	}}}})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Speaker")
	servertest.HoldTicks(t, srv)
	c.Settle()
	for _, source := range []string{"api", "say"} {
		for _, deny := range []bool{false, true} {
			cancel = deny
			before := calls
			if source == "api" {
				host.Broadcast(plugin.Plain("original"))
			} else {
				srv.RunLine("say original")
				c.Settle()
			}
			servertest.RunTicks(t, srv, 1)
			if calls != before+1 {
				t.Fatalf("%s broadcast callbacks = %d", source, calls-before)
			}
			if deny {
				c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySystemChat, protocol.IDPlayDisguisedChat, protocol.IDPlayPlayerChat))
			} else {
				assertPluginSystemChat(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), "replacement")
			}
		}
	}
	nested = true
	cancel = false
	srv.RunLine("say original")
	servertest.RunTicks(t, srv, 1)
	assertPluginSystemChat(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), "[Server] nested")

}
