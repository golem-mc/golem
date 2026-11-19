package chat

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// expectChatPastHeldTick reads the next player chat and checks it is the
// sender's message and that it crossed with no tick finishing on the way.
func expectChatPastHeldTick(t *testing.T, srv *server.Server, c *servertest.Client, frozen uint64, sender, message string) {
	t.Helper()
	data := c.AwaitPacket(protocol.IDPlayPlayerChat)
	servertest.ParsePlayerChat(t, data, "minecraft:chat", sender, protocol.OfflineUUID(sender),
		message, protocol.Text{Text: message}, "", servertest.ServerStampedChatTimestamp)
	if got := srv.TicksRun(); got != frozen {
		t.Fatalf("%q arrived after %d more ticks, want it past the held tick", message, got-frozen)
	}
}

// TestChatDoesNotWaitForTheTick holds the tick still in the middle of a
// survival pass — the vitals seam, installed before the loop starts, parks
// it there — and checks that chat carries on anyway: a message reaches
// another client with no tick finished on the way, and two messages one
// player sends arrive in their order. A broadcast queued to the tick would
// arrive only once the tick was let go.
func TestChatDoesNotWaitForTheTick(t *testing.T) {
	t.Parallel()

	// release closes to let the parked tick go; entered is signalled by the
	// seam the first time it holds the tick, so the test knows the hold is
	// up rather than guessing at the count of finished ticks.
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	srv, connect := servertest.Start(t, servertest.SurvivalConfig(), func(s *server.Server) {
		s.BarsBetween("Bob", func() {
			select {
			case entered <- struct{}{}:
			default:
			}
			<-release
		})
	})
	// The release has to run before the harness's StopTick, whose wait for
	// the loop must not sit on a tick still parked in the seam.
	t.Cleanup(func() { close(release) })

	a := connect().Join("Alice")

	b := connect().Join("Bob")

	// The loop parks itself in Bob's vitals once he has joined; while it
	// sits there no further tick finishes.
	// The bound is only the safety net against a seam never reached.
	select {
	case <-entered:
	case <-time.After(servertest.ExpectTimeout):
		t.Fatal("the tick never reached the vitals seam to be held")
	}
	frozen := srv.TicksRun()

	a.Chat("first past a held tick")
	expectChatPastHeldTick(t, srv, b, frozen, "Alice", "first past a held tick")
	a.Chat("second past a held tick")
	expectChatPastHeldTick(t, srv, b, frozen, "Alice", "second past a held tick")
}

// expectGone waits for the server to finish with a connection it ended and
// checks the player is off the list.
func expectGone(t *testing.T, srv *server.Server, c *servertest.Client) {
	t.Helper()
	c.AwaitServed()
	if got := srv.PlayerCount(); got != 0 {
		t.Fatalf("player count = %d after the server ended the connection, want 0", got)
	}
}

func TestJoinAnnouncementReachesOnlyExistingPlayers(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Bob"))
	alice := connect().Join("Alice")
	bob := connect()
	bob.Login("Bob")
	bob.Configure()
	checkAnnouncement := func(id int32, data []byte) {
		if id == protocol.IDPlaySystemChat && bytes.Contains(data, []byte("multiplayer.player.joined")) {
			t.Error("Bob received a join announcement on his own join")
		}
	}
	// Read the join explicitly: EnterWorld consumes chat along with the chunks.
	bob.ReadJoin(checkAnnouncement)
	bob.MarkLoaded()
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	// A command sent after loading bounds the absence check on Bob's stream.
	bob.ChatCommand("say joined")
	for id, data := range bob.Packets(servertest.ExpectTimeout) {
		checkAnnouncement(id, data)
		if id == protocol.IDPlayDisguisedChat {
			bob.Pushback = append(bob.Pushback, servertest.PushedPacket{Id: id, Data: data})
			servertest.ExpectDisguisedChat(t, bob, "minecraft:say_command", "Bob", "joined", "")
			return
		}
	}
	t.Fatal("Bob did not receive his /say marker")
}

func TestJoinAndChatReachTheOtherPlayers(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	watcher := connect().Join("Watcher")

	joiner := connect()
	joiner.Login("Joiner")
	joiner.Configure()
	joiner.EnterWorld("Joiner")

	if got := srv.PlayerCount(); got != 2 {
		t.Fatalf("player count = %d, want 2", got)
	}

	// The player who was already there sees the newcomer, and is told about
	// them.
	joinerEntity := watcher.ExpectPlayerAdded("Joiner")
	watcher.ExpectAnnouncement("multiplayer.player.joined", "Joiner")

	// What the newcomer says reaches everyone, itself included, each copy
	// with its own recipient's index.
	joiner.Chat("hello everyone")
	if got := expectPlayerChat(t, joiner, "minecraft:chat", "Joiner",
		protocol.OfflineUUID("Joiner"), "hello everyone", "", servertest.ServerStampedChatTimestamp); got != 0 {
		t.Errorf("the joiner's own echo's global index = %d, want 0", got)
	}
	if got := expectPlayerChat(t, watcher, "minecraft:chat", "Joiner",
		protocol.OfflineUUID("Joiner"), "hello everyone", "", servertest.ServerStampedChatTimestamp); got != 0 {
		t.Errorf("the watcher's copy's global index = %d, want 0", got)
	}

	// Leaving takes the body away and is announced to whoever is left.
	joiner.Conn.Close()
	watcher.ExpectPlayerRemoved(joinerEntity, "Joiner")
	watcher.ExpectAnnouncement("multiplayer.player.left", "Joiner")
	joiner.AwaitServed()
	if got := srv.PlayerCount(); got != 1 {
		t.Fatalf("player count = %d after the joiner left, want 1", got)
	}
}

func TestChatRejectsIllegalCharacters(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate")

	// The section sign starts a legacy formatting code. A vanilla server
	// disconnects a client that sends one rather than relaying it, which was
	// checked against a real vanilla server.
	c.Chat("illegal §c colour")

	reason := c.ExpectPlay(protocol.IDPlayDisconnect)
	if len(reason) == 0 {
		t.Error("disconnect carried no reason")
	}
	expectGone(t, srv, c)
}

// TestChatOverTheLimitDisconnects sends a message past the 256-code-unit
// limit in both shapes it can be crossed — ASCII one code unit each, and
// emoji two. The read fails and the client is disconnected rather than
// ignored.
func TestChatOverTheLimitDisconnects(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		message string
	}{
		// 257 ASCII characters, one past the 256-character limit.
		{"ASCII", strings.Repeat("a", 257)},
		// 129 emoji are 258 code units, one pair past the limit.
		{"emoji", strings.Repeat("\U0001F600", 129)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A config per subtest: start binds the default level name to
			// its t's TempDir, so a shared one would put every sibling in
			// the first subtest's directory.
			cfg := config.Default()
			cfg.OnlineMode = false
			srv, c := servertest.Join(t, cfg, "Testificate")

			c.Chat(tc.message)

			var want protocol.Buffer
			want.PutText(protocol.Text{
				Translate: "disconnect.genericReason",
				With:      []protocol.Text{{Text: "Internal Exception: io.netty.handler.codec.DecoderException: Failed to decode packet 'serverbound/minecraft:chat'"}},
			})
			if reason := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(reason, want.Bytes()) {
				t.Errorf("disconnect reason % X, want % X", reason, want.Bytes())
			}
			expectGone(t, srv, c)
		})
	}
}

// TestChatCommandRejectsIllegalCharacters sends a command carrying terminal
// escapes: like a chat message, it disconnects the player rather than
// running. vanilla: ServerGamePacketListenerImpl.tryHandleChat
func TestChatCommandRejectsIllegalCharacters(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Nobody")

	c.ChatCommand("me \x1b]0;pwned\x07\x1b[2K\rFAKE LINE")
	var want protocol.Buffer
	want.PutTextComponent("Illegal characters in chat")
	if reason := c.ExpectPlay(protocol.IDPlayDisconnect); !bytes.Equal(reason, want.Bytes()) {
		t.Errorf("disconnect reason % X, want % X", reason, want.Bytes())
	}
	expectGone(t, srv, c)
}

// TestSignedChatCommandRuns sends /me as a Signed Chat Command, which a
// client with a chat session sends: it runs, and because the source carries
// the signing context the action goes out as a Player Chat Message naming the
// player by profile.
// vanilla: ServerGamePacketListenerImpl.handleSignedChatCommand, MeCommand
func TestSignedChatCommandRuns(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate")

	c.Send(protocol.IDPlaySignedChatCommand, func(b *protocol.Buffer) {
		b.PutString("me waves")
		b.PutI64(0)    // timestamp
		b.PutI64(0)    // salt
		b.PutVarInt(0) // no argument signatures
		b.PutVarInt(0) // none of the server's messages acknowledged
		b.PutRaw([]byte{0, 0, 0})
		b.PutU8(1) // the checksum of an empty acknowledgement set
	})
	if got := expectPlayerChat(t, c, "minecraft:emote_command", "Testificate",
		protocol.OfflineUUID("Testificate"), "waves", "", servertest.ServerStampedChatTimestamp); got != 0 {
		t.Errorf("the signed emote's global index = %d, want 0", got)
	}
	c.Disconnect()
}
