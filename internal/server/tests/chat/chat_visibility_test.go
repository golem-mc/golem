package chat

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// hiddenChatOptionsLine is the red line the server answers a message from a
// hidden-chat client with, instead of broadcasting it.
func hiddenChatOptionsLine() []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{Translate: "chat.disabled.options", Color: protocol.ColorRed})
	b.PutBool(false) // in the chat window, not over the hotbar
	return b.Bytes()
}

// expectNoPlayerChat fails if a player chat message, in either of its two
// forms, reaches c on account of what speaker sent: the check that a client
// whose chat mode must keep chat from them reads none of it. Chat and
// commands are handled on the speaker's connection, so once speaker's
// settle is answered anything relayed is queued to c ahead of c's barrier.
func expectNoPlayerChat(t *testing.T, srv *server.Server, speaker, c *servertest.Client) {
	t.Helper()
	speaker.SettleKeeping()
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayDisguisedChat, protocol.IDPlayPlayerChat))
}

// TestHiddenChatIsRefusedNotBroadcast has a player hide chat and speak
// anyway: the red chat.disabled.options line answers them alone and nothing
// reaches anyone. vanilla: ServerGamePacketListenerImpl.tryHandleChat
func TestHiddenChatIsRefusedNotBroadcast(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)
	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.SetChatMode(protocol.ChatModeHidden)
	alice.Chat("can you hear me?")

	if data := alice.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(data, hiddenChatOptionsLine()) {
		t.Errorf("the hidden chat answer = % X, want the red chat.disabled.options line", data)
	}
	expectNoPlayerChat(t, srv, alice, alice)
	expectNoPlayerChat(t, srv, alice, bob)

	alice.Disconnect()
	bob.Disconnect()
}

// TestCommandsOnlyPlayerChatsButHearsNoPlayerChat has the commands-only
// player speak — vanilla refuses no message of theirs, only a hidden
// client's — while no player chat of any sender reaches them back, their
// own echo included. vanilla: ServerPlayer.sendChatMessage
func TestCommandsOnlyPlayerChatsButHearsNoPlayerChat(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)
	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.SetChatMode(1) // commands only
	alice.Chat("hello")

	if got := expectPlayerChat(t, bob, "minecraft:chat", "Alice",
		protocol.OfflineUUID("Alice"), "hello", "", servertest.ServerStampedChatTimestamp); got != 0 {
		t.Errorf("Bob's copy of Alice's message's index = %d, want 0", got)
	}
	expectNoPlayerChat(t, srv, alice, alice)

	bob.Chat("and to you")
	if got := expectPlayerChat(t, bob, "minecraft:chat", "Bob",
		protocol.OfflineUUID("Bob"), "and to you", "", servertest.ServerStampedChatTimestamp); got != 1 {
		t.Errorf("Bob's own echo's index = %d, want 1 behind Alice's message", got)
	}
	expectNoPlayerChat(t, srv, bob, alice)

	alice.Disconnect()
	bob.Disconnect()
}

// TestSayAndMsgSkipACommandsOnlyPlayer checks the chat broadcasts the
// commands carry over the same player-chat types: /say's world line and the
// two halves of a /msg reach a full client alone, so a commands-only player
// reads neither.
func TestSayAndMsgSkipACommandsOnlyPlayer(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// /say is a gamemaster's command, so the runner is listed as an
	// operator.
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	bob.SetChatMode(1) // commands only
	// Bob's client information travels his own connection, so the wait
	// keeps it from racing the say Alice's connection sends next.
	bob.Settle()

	alice.ChatCommand("say hello team")
	servertest.ExpectDisguisedChat(t, alice, "minecraft:say_command", "Alice", "hello team", "")
	expectNoPlayerChat(t, srv, alice, bob)

	alice.ChatCommand("msg Bob psst")
	servertest.ExpectDisguisedChat(t, alice, "minecraft:msg_command_outgoing", "Alice", "psst", "Bob")
	expectNoPlayerChat(t, srv, alice, bob)

	alice.Disconnect()
	bob.Disconnect()
}

// TestHiddenChatStillRunsCommands checks execution and suppressed feedback separately.
func TestHiddenChatStillRunsCommands(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Bob"))
	alice := connect().Join("Alice")
	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")
	servertest.HoldTicks(t, srv)

	bob.SetChatMode(protocol.ChatModeHidden)
	bob.ChatCommand(`tellraw Alice "executed"`)
	var want protocol.Buffer
	want.PutText(protocol.Text{Text: "executed"})
	want.PutBool(false)
	if got := alice.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, want.Bytes()) {
		t.Errorf("command result = % X, want % X", got, want.Bytes())
	}
	bob.ChatCommand("msg Nobody hi")
	bob.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlaySystemChat))

	alice.Disconnect()
	bob.Disconnect()
}

// TestHiddenChatFiltersSystemMessages covers joins, tellraw, titles and mode changes.
func TestHiddenChatFiltersSystemMessages(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	alice := connect().Join("Alice")
	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")
	bob.SetChatMode(protocol.ChatModeHidden)
	bob.Settle()

	carol := connect().Join("Carol")
	alice.ExpectPlayerAdded("Carol")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Carol")
	servertest.HoldTicks(t, srv)
	bob.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlaySystemChat))

	alice.ChatCommand(`title Bob actionbar "x"`)
	var want protocol.Buffer
	want.PutText(protocol.Text{Text: "x"})
	if got := bob.ExpectPlay(protocol.IDPlayActionBarText); !bytes.Equal(got, want.Bytes()) {
		t.Errorf("action bar = % X, want % X", got, want.Bytes())
	}
	alice.ChatCommand(`tellraw Bob "hi"`)
	alice.SettleKeeping()
	bob.ExpectNoneWithinTicks(srv, 10, servertest.PacketIs(protocol.IDPlaySystemChat))

	bob.Chat("hidden")
	if got := bob.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, hiddenChatOptionsLine()) {
		t.Errorf("hidden chat answer = % X, want % X", got, hiddenChatOptionsLine())
	}

	bob.SetChatMode(1) // commands only
	bob.Settle()
	servertest.ReleaseTicks(t, srv)
	dave := connect().Join("Dave")
	bob.ExpectPlayerAdded("Dave")
	bob.ExpectAnnouncement("multiplayer.player.joined", "Dave")

	alice.Disconnect()
	bob.Disconnect()
	carol.Disconnect()
	dave.Disconnect()
}

// TestHiddenChatIsNotCounted floods from a hidden-chat player: the red line
// answers every message and none of them steps the counter, so no kick.
// vanilla: ServerGamePacketListenerImpl.tryHandleChat
func TestHiddenChatIsNotCounted(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate")

	c.SetChatMode(protocol.ChatModeHidden)
	for round := range 30 {
		c.Chat(fmt.Sprintf("spam %d", round))
	}

	answers := 0
	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySystemChat:
			if bytes.Equal(data, hiddenChatOptionsLine()) {
				answers++
			}
		case protocol.IDPlayDisconnect:
			t.Fatalf("hidden chat was counted and kicked the player: % X", data)
		}
		return false
	})
	if answers != 30 {
		t.Errorf("the hidden client read %d red answers, want 30", answers)
	}

	c.Disconnect()
}

// TestChatModeReturnsToFull has a player hide chat and then switch back to
// full: a message after the switch reaches the others again.
func TestChatModeReturnsToFull(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg)
	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.SetChatMode(protocol.ChatModeHidden)
	alice.Chat("hidden")
	// The red answer rides Alice's own connection, so waiting for it keeps
	// the switch back from racing the message Bob is about to hear.
	if data := alice.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(data, hiddenChatOptionsLine()) {
		t.Errorf("the hidden chat answer = % X, want the red chat.disabled.options line", data)
	}

	alice.SetChatMode(protocol.ChatModeFull)
	alice.Settle()
	alice.Chat("visible")

	if got := expectPlayerChat(t, bob, "minecraft:chat", "Alice",
		protocol.OfflineUUID("Alice"), "visible", "", servertest.ServerStampedChatTimestamp); got != 0 {
		t.Errorf("Bob's copy of Alice's message's index = %d, want 0", got)
	}
	if got := expectPlayerChat(t, alice, "minecraft:chat", "Alice",
		protocol.OfflineUUID("Alice"), "visible", "", servertest.ServerStampedChatTimestamp); got != 0 {
		t.Errorf("Alice's own echo's index = %d, want 0: the hidden mode dropped hers", got)
	}

	alice.Disconnect()
	bob.Disconnect()
}
