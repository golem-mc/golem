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

// commandsEvery sends one /list every gap ticks, count times, and reports
// whether the spam kick arrived. The counter wears down once per server tick,
// so the gap, not the wall time, decides it.
// vanilla: ServerGamePacketListenerImpl.tick, TickThrottler.tick
func commandsEvery(t *testing.T, gap, count int) bool {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.CommandSpamThresholdSeconds = 10
	srv, c := servertest.Join(t, cfg, "Testificate")
	servertest.HoldTicks(t, srv)
	defer c.Disconnect()
	kicked := false
	for range count {
		c.ChatCommand("list")
		c.ReadTicks(srv, gap, func(got int32, data []byte) bool {
			if got == protocol.IDPlayDisconnect {
				kicked = true
			}
			return kicked
		})
		if kicked {
			return true
		}
	}
	return false
}

// TestCommandsEveryFewTicksKick sends a command every 10 ticks, as a player
// does on a server running at half speed: 20 added, 10 worn, so the counter
// passes the threshold.
func TestCommandsEveryFewTicksKick(t *testing.T) {
	t.Parallel()
	if !commandsEvery(t, 10, 40) {
		t.Fatal("a command every 10 ticks never brought disconnect.spam")
	}
}

// TestCommandsEverySecondAtFullSpeedStay sends a command every 20 ticks, a
// second at 20 TPS: the counter wears off as fast as it grows.
func TestCommandsEverySecondAtFullSpeedStay(t *testing.T) {
	t.Parallel()
	if commandsEvery(t, 20, 30) {
		t.Fatal("a command every 20 ticks was kicked")
	}
}
