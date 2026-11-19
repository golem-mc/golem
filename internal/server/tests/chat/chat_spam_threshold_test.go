package chat

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// spamClient joins a held-tick server whose spam thresholds are chat and
// command seconds. Held ticks keep the counters from wearing down mid-burst.
func spamClient(t *testing.T, chat, command int) (*servertest.Client, func(id int32) (kicked bool, seen int)) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ChatSpamThresholdSeconds = chat
	cfg.CommandSpamThresholdSeconds = command

	srv, c := servertest.Join(t, cfg, "Testificate")
	servertest.HoldTicks(t, srv)

	// drain reads up to the barrier answer behind everything the burst
	// owed the client, counting packets of id and noting a spam kick.
	drain := func(id int32) (bool, int) {
		kicked, seen := false, 0
		c.ReadTicks(srv, 0, func(got int32, data []byte) bool {
			switch got {
			case id:
				seen++
			case protocol.IDPlayDisconnect:
				if string(data) != string(spamKickText()) {
					t.Errorf("disconnect = % X, want the disconnect.spam text", data)
				}
				kicked = true
			}
			return false
		})
		return kicked, seen
	}
	return c, drain
}

// TestChatSpamThresholdOneKicksAtTheFirstMessage sets chat-spam-threshold-seconds
// to 1: one message steps the counter to 20, the threshold, so the sender is
// kicked where the default threshold would let three messages pass.
// vanilla: ServerGamePacketListenerImpl.detectRateSpam
func TestChatSpamThresholdOneKicksAtTheFirstMessage(t *testing.T) {
	t.Parallel()
	c, _ := spamClient(t, 1, 10)
	if err := c.WriteChat("one"); err != nil {
		t.Fatal(err)
	}
	if got := readSpamKick(t, c, protocol.IDPlayPlayerChat); got != 1 {
		t.Errorf("echoes before the kick = %d, want 1", got)
	}
	c.Disconnect()
}

// TestChatSpamDefaultThresholdAllowsThreeMessages is the counterpart at the
// default of 10 seconds.
func TestChatSpamDefaultThresholdAllowsThreeMessages(t *testing.T) {
	t.Parallel()
	c, drain := spamClient(t, 10, 10)
	for i := range 3 {
		c.Chat(fmt.Sprintf("m%d", i))
	}
	kicked, echoes := drain(protocol.IDPlayPlayerChat)
	if kicked || echoes != 3 {
		t.Errorf("kicked = %v, echoes = %d; want no kick and 3 echoes", kicked, echoes)
	}
	c.Disconnect()
}

// TestCommandSpamThresholdIsSeparateFromChat floods commands with a command
// threshold of 1 and a chat threshold of 1000: the commands kick. Chat at the
// same settings, in the next test, does not.
func TestCommandSpamThresholdIsSeparateFromChat(t *testing.T) {
	t.Parallel()
	c, _ := spamClient(t, 1000, 1)
	if err := c.WriteChatCommand("nosuchcommand"); err != nil {
		t.Fatal(err)
	}
	if got := readSpamKick(t, c, protocol.IDPlaySystemChat); got < 1 {
		t.Errorf("command failures before the kick = %d, want at least 1", got)
	}
	c.Disconnect()
}

// TestChatIsNotCountedByTheCommandThreshold sends chat with a command
// threshold of 1: the chat counter is the one with the roomy threshold.
func TestChatIsNotCountedByTheCommandThreshold(t *testing.T) {
	t.Parallel()
	c, drain := spamClient(t, 1000, 1)
	for i := range 30 {
		c.Chat(fmt.Sprintf("m%d", i))
	}
	kicked, echoes := drain(protocol.IDPlayPlayerChat)
	if kicked || echoes != 30 {
		t.Errorf("kicked = %v, echoes = %d; want no kick and 30 echoes", kicked, echoes)
	}
	c.Disconnect()
}

// TestCommandsAreNotCountedByTheChatThreshold is the other direction: a chat
// threshold of 1 leaves a command flood alone.
func TestCommandsAreNotCountedByTheChatThreshold(t *testing.T) {
	t.Parallel()
	c, drain := spamClient(t, 1, 1000)
	for range 30 {
		c.ChatCommand("nosuchcommand")
	}
	kicked, failures := drain(protocol.IDPlaySystemChat)
	if kicked || failures < 30 {
		t.Errorf("kicked = %v, failures = %d; want no kick and at least 30 failures", kicked, failures)
	}
	c.Disconnect()
}

// TestSpamThresholdZeroNeverKicks sets both thresholds to 0: a flood passes.
// vanilla: TickThrottler.isUnderThreshold
func TestSpamThresholdZeroNeverKicks(t *testing.T) {
	t.Parallel()
	c, drain := spamClient(t, 0, 0)
	for i := range 30 {
		c.Chat(fmt.Sprintf("m%d", i))
	}
	kicked, echoes := drain(protocol.IDPlayPlayerChat)
	if kicked || echoes != 30 {
		t.Errorf("kicked = %v, echoes = %d; want no kick and 30 echoes", kicked, echoes)
	}
	c.Disconnect()
}
