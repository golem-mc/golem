package chat

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// successFeedbackBytes is the System Chat payload a Feedback carries for a
// translation key with no arguments.
func successFeedbackBytes(key string) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{Translate: key})
	b.PutBool(false) // in the chat window, not over the hotbar
	return b.Bytes()
}

// TestCommandFailureKeepsItsPlaceBeforeASuccess has a player send a failing
// command and then a succeeding one: both ride the mailbox, so the failure
// queued first reaches them first. Written straight, the success could
// overtake it.
func TestCommandFailureKeepsItsPlaceBeforeASuccess(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// Both commands are the counter's to count, so the runner is listed as
	// an operator, whom a kick spares; /banlist needs that level to run at
	// all, and an empty ban list is what answers with the success line.
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	failure := servertest.CommandErrorMessageForTest("command.unknown.command")
	success := successFeedbackBytes("commands.banlist.none")

	for round := range 60 {
		c.ChatCommand("nosuchcommand")
		c.ChatCommand("banlist")

		var failed, succeeded bool
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySystemChat {
				return
			}
			switch {
			case !failed && bytes.Equal(data, failure):
				failed = true
			case !succeeded && bytes.Equal(data, success):
				if !failed {
					t.Fatalf("round %d: the success arrived before the earlier command's failure", round)
				}
				succeeded = true
			}
		})
		if !failed || !succeeded {
			t.Fatalf("round %d: failure and success read = %v, %v, want both", round, failed, succeeded)
		}
	}

	c.Disconnect()
}

// TestChatEchoKeepsItsPlaceBeforeCommandFeedback checks the order of the two
// packets one player's own messages cause in a row: the chat echo of what
// they said, then the failure of the command they sent straight after. Both
// travel by the same road, so the echo, queued first, reaches them first.
func TestChatEchoKeepsItsPlaceBeforeCommandFeedback(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// The command side of the loop is what the spam throttler counts, so
	// the runner is listed as an operator, whom a kick spares.
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	message := servertest.CommandErrorMessageForTest("command.unknown.command")
	context := servertest.CommandErrorContextForTest("nosuchcommand", 0)

	for round := range 40 {
		line := fmt.Sprintf("m%d", round)
		c.Chat(line)
		c.ChatCommand("nosuchcommand")

		var echo, failure, failureContext bool
		c.SettleAndCollect(func(id int32, data []byte) {
			switch id {
			case protocol.IDPlayPlayerChat:
				index := servertest.ParsePlayerChat(t, data, "minecraft:chat", "Testificate",
					protocol.OfflineUUID("Testificate"), line, protocol.Text{Text: line}, "", servertest.ServerStampedChatTimestamp)
				echo = true
				if int(index) != round {
					t.Errorf("round %d: the echo carried global index %d", round, index)
				}
			case protocol.IDPlaySystemChat:
				switch {
				case !failure && bytes.Equal(data, message):
					if !echo {
						t.Fatalf("round %d: the command's failure arrived before the chat echo", round)
					}
					failure = true
				case !failureContext && bytes.Equal(data, context):
					failureContext = true
				}
			}
		})
		if !echo || !failure || !failureContext {
			t.Fatalf("round %d: echo, failure and context read = %v, %v, %v, want all three",
				round, echo, failure, failureContext)
		}
	}

	c.Disconnect()
}
