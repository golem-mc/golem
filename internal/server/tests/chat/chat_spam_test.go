package chat

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// spamKickText is the Disconnect payload a spam kick carries.
func spamKickText() []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{Translate: "disconnect.spam"})
	return b.Bytes()
}

// readSpamKick reads until the spam kick and returns how many packets of id
// came before it. The server closes with the burst's tail unread, and the
// resulting reset can discard the Disconnect still in flight, so a reset after
// at least one counted packet stands for the kick; a connection that goes
// quiet instead fails.
func readSpamKick(t *testing.T, c *servertest.Client, id int32) int {
	t.Helper()
	counted := 0
	for {
		got, data, err := c.ReadBy(time.Now().Add(servertest.ExpectTimeout))
		if err != nil {
			if errors.Is(err, servertest.ErrWaitOver) || counted == 0 {
				t.Fatalf("the burst brought no disconnect.spam (%d counted before: %v)", counted, err)
			}
			return counted
		}
		switch got {
		case id:
			counted++
		case protocol.IDPlayKeepAlive:
			c.AnswerKeepAlive(data)
		case protocol.IDPlayDisconnect:
			if !bytes.Equal(data, spamKickText()) {
				t.Errorf("the kick's disconnect = % X, want the disconnect.spam text", data)
			}
			return counted
		}
	}
}

// TestChatSpamKicks has a player burst past the spam threshold: the
// connection ends with the disconnect.spam the counter's kick carries,
// behind the echo of the very message that crossed the threshold.
func TestChatSpamKicks(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate")

	// A message's worth of headroom short of a kick each, twenty in the
	// burst cross the counter whatever the clock wore off on the way.
	// The kick may reset the connection with messages still to send, so the
	// burst stops at the first failed write.
	for round := range 20 {
		if err := c.WriteChat(fmt.Sprintf("spam %d", round)); err != nil {
			break
		}
	}

	echoes := readSpamKick(t, c, protocol.IDPlayPlayerChat)
	if echoes == 0 {
		t.Fatal("no chat echo arrived before the kick")
	}

	c.Disconnect()
}

// TestChatBelowTheSpamThresholdStays has a player chat one message short of
// the burst the counter kicks for: every echo comes back and the
// connection holds.
func TestChatBelowTheSpamThresholdStays(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate")

	for round := range 9 {
		c.Chat(fmt.Sprintf("spam %d", round))
	}

	// The chat is handled on the connection, so the barrier's answer comes
	// behind every echo and any kick.
	echoes := 0
	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayPlayerChat:
			echoes++
		case protocol.IDPlayDisconnect:
			t.Fatalf("nine messages kicked the player: % X", data)
		}
		return false
	})
	if echoes != 9 {
		t.Errorf("the player read %d of their own echoes, want 9", echoes)
	}

	c.Disconnect()
}

// TestCommandSpamKicks counts the commands the throttler kicks for the same
// way: the refusals of what would not parse count no less, and the kick
// follows the failure of the command that crossed the threshold.
func TestCommandSpamKicks(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate")

	// The kick may reset the connection with commands still to send, so the
	// burst stops at the first failed write.
	for range 20 {
		if err := c.WriteChatCommand("nosuchcommand"); err != nil {
			break
		}
	}

	failures := readSpamKick(t, c, protocol.IDPlaySystemChat)
	if failures == 0 {
		t.Fatal("no command failure arrived before the kick")
	}

	c.Disconnect()
}

// TestSpamKickSparesAListedOperatorOfLevelZero floods from a player on the
// ops list at level 0, who holds no permission level: vanilla's exemption is
// the list itself, so every echo comes back.
// vanilla: PlayerList.isOp, ServerGamePacketListenerImpl.detectRateSpam
func TestSpamKickSparesAListedOperatorOfLevelZero(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	record := servertest.OpRecord{
		UUID:  protocol.OfflineUUID("Testificate").String(),
		Name:  "Testificate",
		Level: 0,
	}
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.WriteOpsFile(t, record))

	for round := range 20 {
		c.Chat(fmt.Sprintf("spam %d", round))
	}

	echoes := 0
	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayPlayerChat:
			echoes++
		case protocol.IDPlayDisconnect:
			t.Fatalf("a level-0 listed operator was kicked for spamming: % X", data)
		}
		return false
	})
	if echoes != 20 {
		t.Errorf("the operator read %d of their own echoes, want 20", echoes)
	}

	c.Disconnect()
}

// TestEmptyCommandCounts floods with command packets that name no command:
// vanilla counts every command packet, so the empty lines kick as unknown
// ones do.
func TestEmptyCommandCounts(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate")

	// The kick may reset the connection with commands still to send, so the
	// burst stops at the first failed write.
	for range 20 {
		if err := c.WriteChatCommand(""); err != nil {
			break
		}
	}

	kicked := false
	for id, data := range c.Stream() {
		if id == protocol.IDPlayDisconnect {
			if !bytes.Equal(data, spamKickText()) {
				t.Errorf("the kick's disconnect = % X, want the disconnect.spam text", data)
			}
			kicked = true
			break
		}
	}
	if !kicked {
		t.Fatal("twenty empty commands in one burst brought no disconnect.spam")
	}

	c.Disconnect()
}

// TestSpamKickStopsTheRestOfABurst floods far past the counter in one burst:
// the messages the read loop had already taken off the socket when the
// counter crossed are not relayed, so the others hear only the count up to
// the kick, and the kicked connection reads a single Disconnect.
func TestSpamKickStopsTheRestOfABurst(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)
	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	// The counter wears down by the loop's clock: held with no ticks run,
	// ten steps reach the threshold however long a loaded machine takes to
	// read the burst.
	servertest.HoldTicks(t, srv)

	// Thirty sent without waiting between them, the whole burst in the
	// server's socket when the counter crosses. The server resets the
	// connection with the tail still unread, so the writes after the kick
	// are best effort.
	for round := range 30 {
		alice.ChatBestEffort(fmt.Sprintf("flood %d", round))
	}

	// One Disconnect, however many messages crossed the counter: read up to
	// the server's close.
	disconnects := 0
	for {
		id, data, err := alice.ReadBy(time.Now().Add(servertest.ExpectTimeout))
		if errors.Is(err, servertest.ErrWaitOver) {
			t.Fatal("the kicked connection was never closed")
		}
		if err != nil {
			break
		}
		if id == protocol.IDPlayDisconnect {
			disconnects++
			if !bytes.Equal(data, spamKickText()) {
				t.Errorf("the kick's disconnect = % X, want the disconnect.spam text", data)
			}
		}
	}
	if disconnects != 1 {
		t.Fatalf("the kicked connection read %d Disconnects, want exactly one", disconnects)
	}

	// Alice's connection is done with, so every relay it made is queued to
	// Bob ahead of his barrier's answer. Ten steps reach the threshold; the
	// tail is dropped rather than relayed.
	alice.AwaitServed()
	echoes := 0
	bob.ReadTicks(srv, 0, func(id int32, _ []byte) bool {
		if id == protocol.IDPlayPlayerChat {
			echoes++
		}
		return false
	})
	if echoes > 10 {
		t.Fatalf("Bob heard %d messages of the burst, want the 10 the counter relays", echoes)
	}

	alice.Disconnect()
	bob.Disconnect()
}

// TestSpamKickSparesOperators has an operator burst past the threshold:
// vanilla's kick exempts operators, so every echo comes back.
func TestSpamKickSparesOperators(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	for round := range 20 {
		c.Chat(fmt.Sprintf("spam %d", round))
	}

	echoes := 0
	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayPlayerChat:
			echoes++
		case protocol.IDPlayDisconnect:
			t.Fatalf("an operator was kicked for spamming: % X", data)
		}
		return false
	})
	if echoes != 20 {
		t.Errorf("the operator read %d of their own echoes, want 20", echoes)
	}

	c.Disconnect()
}
