package chat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func preventReportsConfig(t *testing.T) *config.Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "server.properties")
	if err := os.WriteFile(path, []byte("prevent-chat-reports=true\nwhite-list=false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	return cfg
}

// vanilla: ClientboundLoginPacket.write, ClientboundPlayerInfoUpdatePacket.Action.INITIALIZE_CHAT
func checkNoReportSessions(t *testing.T, data []byte) int {
	t.Helper()
	r := protocol.NewReader(data)
	actions := r.ReadU8()
	count := r.ReadVarInt()
	for range count {
		r.ReadUUID()
		if actions&1 != 0 {
			r.ReadString(16)
			for n := r.ReadVarInt(); n > 0; n-- {
				r.ReadString(32767)
				r.ReadString(32767)
				if r.ReadBool() {
					r.ReadString(32767)
				}
			}
		}
		if actions&2 != 0 && r.ReadBool() {
			t.Fatal("INITIALIZE_CHAT published a chat session")
		}
		if actions&4 != 0 {
			r.ReadVarInt()
		}
		if actions&8 != 0 {
			r.ReadBool()
		}
		if actions&16 != 0 {
			r.ReadVarInt()
		}
		if actions&32 != 0 && r.ReadBool() {
			protocol.ReadText(r)
		}
		if actions&64 != 0 {
			r.ReadVarInt()
		}
		if actions&128 != 0 {
			r.ReadBool()
		}
	}
	if r.Err() != nil || r.Remaining() != 0 {
		t.Fatalf("invalid player info: %v, %d trailing bytes", r.Err(), r.Remaining())
	}
	if actions&2 == 0 {
		return 0
	}
	return int(count)
}

func TestPreventChatReportsStatusAndLogin(t *testing.T) {
	t.Parallel()
	cfg := preventReportsConfig(t)
	cfg.OnlineMode = false
	srv, c := servertest.Dial(t, cfg)
	c.Handshake(protocol.NextStateStatus)
	c.Send(protocol.IDStatusRequest, nil)
	r := protocol.NewReader(c.Expect(protocol.IDStatusResponse))
	var status map[string]any
	if err := json.Unmarshal([]byte(r.ReadString(32767)), &status); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"enforcesSecureChat", "preventsChatReports"} {
		if status[key] != true {
			t.Errorf("status %s = %v, want true", key, status[key])
		}
	}
	c.Disconnect()
	player := servertest.DialServer(t, srv)
	player.Login("Testificate")
	player.Configure()
	sawLogin, sessions := false, 0
	player.ReadJoin(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayLogin:
			sawLogin = true
			if len(data) == 0 || data[len(data)-1] != 1 {
				t.Error("Login does not advertise secure chat")
			}
		case protocol.IDPlayPlayerInfoUpdate:
			sessions += checkNoReportSessions(t, data)
		}
	})
	if !sawLogin || sessions != 1 {
		t.Fatalf("login = %v, initialized players = %d", sawLogin, sessions)
	}
	player.MarkLoaded()
	player.Chat("offline chat")
	player.ExpectSystemText(protocol.Text{Translate: "chat.type.text", With: []protocol.Text{servertest.PlayerDisplayComponent("Testificate"), {Text: "offline chat"}}})
}

func TestPreventChatReportsSignedMessages(t *testing.T) {
	t.Parallel()
	profile := protocol.OfflineUUID("Testificate")
	key, session, _ := servertest.SignedSession(t, profile)
	cfg := preventReportsConfig(t)
	cfg.ChatSpamThresholdSeconds = 0
	cfg.CommandSpamThresholdSeconds = 0
	var srv *server.Server
	c := onlineChatClient(t, cfg, servertest.OpsListing(t, "Testificate"), func(s *server.Server) {
		srv = s
		s.SetChatKeyProvider(chatKeysFunc(func(context.Context) (*chatsecurity.KeySet, error) {
			t.Error("prevent-chat-reports fetched service keys")
			return nil, fmt.Errorf("unexpected key fetch")
		}))
	})
	installChatSession(c, session)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayPlayerInfoUpdate {
			checkNoReportSessions(t, data)
		}
	})
	watcher := servertest.DialServer(t, srv)
	initialized := 0
	servertest.CompleteOnlineChatLogin(watcher, "Watcher", func(id int32, data []byte) {
		if id == protocol.IDPlayPlayerInfoUpdate {
			initialized += checkNoReportSessions(t, data)
		}
	})
	if initialized != 2 {
		t.Fatalf("initialized %d players, want 2", initialized)
	}
	c.ExpectPlayerAdded("Watcher")
	c.Settle()
	watcher.Settle()
	servertest.HoldTicks(t, srv)
	c.ChatCommand("team add red")
	c.ChatCommand("team join red Testificate")
	c.ChatCommand("team join red Watcher")
	c.Settle()
	watcher.Settle()
	self := servertest.TeamPlayerDisplayComponent("Testificate", "", "")
	other := servertest.TeamPlayerDisplayComponent("Watcher", "", "")
	team := protocol.Text{Translate: "chat.square_brackets", With: []protocol.Text{{Text: "red"}}, ClickCommand: "/teammsg ", HoverText: &protocol.Text{Translate: "chat.type.team.hover"}}
	peer := &commandChatPeer{c: c}
	// More than twenty signed sends, with empty client acknowledgements: System Chat
	// must never add anything to either recipient's last-seen window.
	for i := range 28 {
		raw := fmt.Sprintf("héllo 世界 %d", i)
		body := chatsecurity.NewBody(raw, time.Now().UnixMilli(), int64(i+13), nil)
		sig := signCommandBody(t, key, profile, session, int32(i), body)
		var incoming, outgoing protocol.Text
		switch i % 7 {
		case 0:
			c.Send(protocol.IDPlayChatMessage, func(b *protocol.Buffer) {
				b.PutString(raw)
				b.PutI64(body.Timestamp())
				b.PutI64(body.Salt())
				b.PutBool(true)
				b.PutRaw(sig[:])
				peer.ack(b)
			})
			incoming = protocol.Text{Translate: "chat.type.text", With: []protocol.Text{self, {Text: raw}}}
			outgoing = incoming
		case 1, 2, 3:
			alias := []string{"msg", "tell", "w"}[i%7-1]
			sendSignedCommand(peer, alias+" Watcher "+raw, body, []chatsecurity.ArgumentSignature{{Name: "message", Signature: sig}})
			incoming = protocol.Text{Translate: "commands.message.display.incoming", With: []protocol.Text{self, {Text: raw}}, Color: protocol.ColorGray, Italic: true}
			outgoing = protocol.Text{Translate: "commands.message.display.outgoing", With: []protocol.Text{other, {Text: raw}}, Color: protocol.ColorGray, Italic: true}
		case 4:
			sendSignedCommand(peer, "me "+raw, body, []chatsecurity.ArgumentSignature{{Name: "action", Signature: sig}})
			incoming = protocol.Text{Translate: "chat.type.emote", With: []protocol.Text{self, {Text: raw}}}
			outgoing = incoming
		case 5:
			// Selector expansion must survive conversion, including the display name's events.
			raw = "hello @s"
			body = chatsecurity.NewBody(raw, body.Timestamp(), body.Salt(), nil)
			sig = signCommandBody(t, key, profile, session, int32(i), body)
			sendSignedCommand(peer, "say "+raw, body, []chatsecurity.ArgumentSignature{{Name: "message", Signature: sig}})
			incoming = protocol.Text{Translate: "chat.type.announcement", With: []protocol.Text{self, {Text: "hello ", Extra: []protocol.Text{self}}}}
			outgoing = incoming
		case 6:
			sendSignedCommand(peer, "teammsg "+raw, body, []chatsecurity.ArgumentSignature{{Name: "message", Signature: sig}})
			incoming = protocol.Text{Translate: "chat.type.team.text", With: []protocol.Text{team, self, {Text: raw}}}
			outgoing = protocol.Text{Translate: "chat.type.team.sent", With: []protocol.Text{team, self, {Text: raw}}}
		}
		expectConvertedChat(t, c, outgoing)
		expectConvertedChat(t, watcher, incoming)
	}
	c.Send(protocol.IDPlayChatAck, func(b *protocol.Buffer) { b.PutVarInt(0) })
	// Neither a missing signature nor an untrusted replacement session may disable chat.
	session.PublicKey, session.Certificate = []byte{1}, []byte{2}
	installChatSession(c, session)
	c.Chat("unsigned after signed")
	expectConvertedChat(t, c, protocol.Text{Translate: "chat.type.text", With: []protocol.Text{self, {Text: "unsigned after signed"}}})
	expectConvertedChat(t, watcher, protocol.Text{Translate: "chat.type.text", With: []protocol.Text{self, {Text: "unsigned after signed"}}})
	sendSignedCommand(peer, "msg Watcher invalid signature", chatsecurity.NewBody("invalid signature", time.Now().UnixMilli(), 0, nil), []chatsecurity.ArgumentSignature{{Name: "wrong", Signature: chatsecurity.Signature{}}})
	expectConvertedChat(t, watcher, protocol.Text{Translate: "commands.message.display.incoming", With: []protocol.Text{self, {Text: "invalid signature"}}, Color: protocol.ColorGray, Italic: true})
	c.ChatCommand("say unsigned command")
	expectConvertedChat(t, watcher, protocol.Text{Translate: "chat.type.announcement", With: []protocol.Text{self, {Text: "unsigned command"}}})
	c.SettleKeeping()
	for _, recipient := range []*servertest.Client{c, watcher} {
		recipient.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayPlayerChat, protocol.IDPlayDisguisedChat))
	}
}

func expectConvertedChat(t *testing.T, c *servertest.Client, want protocol.Text) {
	t.Helper()
	var expected protocol.Buffer
	expected.PutText(want)
	expected.PutBool(false)
	for id, data := range c.Stream() {
		switch id {
		case protocol.IDPlayPlayerChat, protocol.IDPlayDisguisedChat, protocol.IDPlayDisconnect:
			t.Fatalf("got packet 0x%02x, want converted System Chat", id)
		case protocol.IDPlaySystemChat:
			if !bytes.Equal(data, expected.Bytes()) {
				got, _ := protocol.ReadText(protocol.NewReader(data))
				t.Fatalf("system chat = %+v, want %+v", got, want)
			}
			return
		}
	}
}

func TestPreventChatReportsVisibility(t *testing.T) {
	t.Parallel()
	cfg := preventReportsConfig(t)
	cfg.OnlineMode = false
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	alice := connect().Join("Alice")
	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")
	servertest.HoldTicks(t, srv)
	alice.ChatCommand("team add red")
	alice.ChatCommand("team join red Alice")
	alice.ChatCommand("team join red Bob")
	alice.Settle()
	bob.Settle()
	for _, mode := range []int32{1, protocol.ChatModeHidden} {
		bob.SetChatMode(mode)
		bob.Settle()
		alice.Chat("ordinary chat")
		for _, command := range []string{"msg Bob whisper", "tell Bob alias", "w Bob alias", "me action", "say announcement", "teammsg team"} {
			alice.ChatCommand(command)
		}
		alice.Settle()
		bob.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySystemChat, protocol.IDPlayPlayerChat, protocol.IDPlayDisguisedChat))
	}
	// Commands-only still allows real system feedback and sending player chat.
	bob.SetChatMode(1)
	bob.Settle()
	alice.ChatCommand(`tellraw Bob "feedback"`)
	expectConvertedChat(t, bob, protocol.Text{Text: "feedback"})
	bob.Chat("I can speak")
	expectConvertedChat(t, alice, protocol.Text{Translate: "chat.type.text", With: []protocol.Text{servertest.TeamPlayerDisplayComponent("Bob", "", ""), {Text: "I can speak"}}})
	bob.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySystemChat, protocol.IDPlayPlayerChat, protocol.IDPlayDisguisedChat))
	bob.SetChatMode(protocol.ChatModeHidden)
	bob.Chat("refused")
	bob.ExpectSystemText(protocol.Text{Translate: "chat.disabled.options", Color: protocol.ColorRed})
	alice.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySystemChat, protocol.IDPlayPlayerChat, protocol.IDPlayDisguisedChat))
	bob.SetChatMode(protocol.ChatModeFull)
	bob.Settle()
	alice.Chat("visible again")
	expectConvertedChat(t, bob, protocol.Text{Translate: "chat.type.text", With: []protocol.Text{servertest.TeamPlayerDisplayComponent("Alice", "", ""), {Text: "visible again"}}})
}

func TestPreventChatReportsValidationAndSpam(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"chat spam", "command spam", "illegal chat", "invalid ack", "truncated signature", "truncated session", "argument limit"} {
		t.Run(kind, func(t *testing.T) {
			cfg := preventReportsConfig(t)
			cfg.OnlineMode = false
			cfg.ChatSpamThresholdSeconds = 2
			cfg.CommandSpamThresholdSeconds = 2
			srv, c := servertest.Join(t, cfg, "Testificate")
			servertest.HoldTicks(t, srv)
			switch kind {
			case "chat spam", "command spam":
				for i := range 2 {
					if kind == "chat spam" {
						c.Chat(fmt.Sprintf("spam %d", i))
					} else {
						c.ChatCommand("me spam")
					}
				}
				key, text := "chat.type.text", "spam 0"
				if kind == "command spam" {
					key, text = "chat.type.emote", "spam"
				}
				c.ExpectSystemText(protocol.Text{Translate: key, With: []protocol.Text{servertest.PlayerDisplayComponent("Testificate"), {Text: text}}})
				c.ExpectPlay(protocol.IDPlaySystemChat)
				servertest.ExpectDisconnect(t, c, protocol.Text{Translate: "disconnect.spam"})
			case "illegal chat":
				c.Chat("illegal §c color")
				servertest.ExpectDisconnect(t, c, protocol.Text{Text: "Illegal characters in chat"})
			case "invalid ack":
				c.Send(protocol.IDPlayChatAck, func(b *protocol.Buffer) { b.PutVarInt(1) })
				expectChatValidationFailed(t, c)
			default:
				switch kind {
				case "truncated signature":
					c.Send(protocol.IDPlayChatMessage, func(b *protocol.Buffer) { b.PutString("short"); b.PutI64(0); b.PutI64(0); b.PutBool(true); b.PutU8(1) })
				case "truncated session":
					c.Send(protocol.IDPlayPlayerSession, func(b *protocol.Buffer) { b.PutU8(1) })
				case "argument limit":
					c.Send(protocol.IDPlaySignedChatCommand, func(b *protocol.Buffer) { b.PutString("me short"); b.PutI64(0); b.PutI64(0); b.PutVarInt(9) })
				}
				c.AwaitServed()
				if srv.PlayerCount() != 0 {
					t.Fatal("malformed packet did not end the connection")
				}
			}
		})
	}
}

func TestPreventChatReportsOffStatus(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Dial(t, cfg)
	c.Handshake(protocol.NextStateStatus)
	c.Send(protocol.IDStatusRequest, nil)
	// Preserve the existing response, including omitting the mod extension entirely.
	var want protocol.Buffer
	want.PutString(fmt.Sprintf(`{"version":{"name":%q,"protocol":%d},"players":{"max":20,"online":0},"description":{"text":"A Go Minecraft Server"},"enforcesSecureChat":false}`, protocol.GameVersion, protocol.Version))
	if data := c.Expect(protocol.IDStatusResponse); !bytes.Equal(data, want.Bytes()) {
		t.Errorf("disabled status response changed: %s", protocol.NewReader(data).ReadString(32767))
	}
}
