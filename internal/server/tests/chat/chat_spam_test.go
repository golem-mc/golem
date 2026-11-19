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
