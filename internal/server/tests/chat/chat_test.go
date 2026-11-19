package chat

import (
	"bytes"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// expectGone waits for the server to finish with a connection it ended and
// checks the player is off the list.
func expectGone(t *testing.T, srv *server.Server, c *servertest.Client) {
	t.Helper()
	c.AwaitServed()
	if got := srv.PlayerCount(); got != 0 {
		t.Fatalf("player count = %d after the server ended the connection, want 0", got)
	}
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
