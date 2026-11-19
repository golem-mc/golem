package admin

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// expectTransfer fails unless the next packet the client reads is the
// Transfer packet for the host and port, the reconnect a /transfer hands it.
func expectTransfer(t *testing.T, c *servertest.Client, host string, port int32) {
	t.Helper()
	data := c.ExpectPlay(protocol.IDPlayTransfer)
	r := protocol.NewReader(data)
	gotHost := r.ReadString(32767)
	gotPort := r.ReadVarInt()
	if err := r.Err(); err != nil {
		t.Fatalf("decode transfer packet: %v", err)
	}
	if gotHost != host || gotPort != port {
		t.Errorf("transfer packet = %s:%d, want %s:%d", gotHost, gotPort, host, port)
	}
}

// TestTransferCommand runs /transfer with a host and a port: the operator
// running it is handed the Transfer packet for that host and port, then the
// feedback naming them.
func TestTransferCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, _, alice, _ := servertest.AdminPair(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	alice.ChatCommand("transfer 127.0.0.1 25852")
	expectTransfer(t, alice, "127.0.0.1", 25852)

	feedback := servertest.FeedbackComponent(t, alice)
	if feedback.Translate != "commands.transfer.success.single" {
		t.Fatalf("transfer feedback key = %q, want commands.transfer.success.single", feedback.Translate)
	}
	if len(feedback.With) != 3 || feedback.With[0].Text != "Alice" ||
		feedback.With[1].Text != "127.0.0.1" ||
		feedback.With[2].Number == nil || *feedback.With[2].Number != 25852 {
		t.Errorf("transfer feedback args = %+v, want Alice, 127.0.0.1 and 25852", feedback.With)
	}

	alice.Disconnect()
}

// TestTransferTargetsAnotherPlayer runs /transfer naming a player: the
// target receives the Transfer packet and the runner the feedback naming
// the target, who alone is moved.
func TestTransferTargetsAnotherPlayer(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, _, alice, bob := servertest.AdminPair(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	alice.ChatCommand("transfer example.org 25852 Bob")
	expectTransfer(t, bob, "example.org", 25852)

	feedback := servertest.FeedbackComponent(t, alice)
	if feedback.Translate != "commands.transfer.success.single" {
		t.Fatalf("transfer feedback key = %q, want commands.transfer.success.single", feedback.Translate)
	}
	if len(feedback.With) != 3 || feedback.With[0].Text != "Bob" ||
		feedback.With[1].Text != "example.org" ||
		feedback.With[2].Number == nil || *feedback.With[2].Number != 25852 {
		t.Errorf("transfer feedback args = %+v, want Bob, example.org and 25852", feedback.With)
	}

	bob.Disconnect()
	alice.Disconnect()
}

// TestTransferNeedsAnOperator checks the refusal a non-op meets: vanilla's
// tree never offers them the command, so the server answers it as an
// unknown one at the first word.
func TestTransferNeedsAnOperator(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, _, _, bob := servertest.AdminPair(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	bob.ChatCommand("transfer 127.0.0.1 25852")
	bob.ExpectCommandError(
		servertest.CommandErrorMessageForTest("command.unknown.command"),
		servertest.CommandErrorContextForTest("transfer 127.0.0.1 25852", 0),
	)

	bob.Disconnect()
}

// TestTransferRefusals checks the refusals the parser raises, each with the
// error and the context line the vanilla server answers it with: a
// port outside the 1..65535 range, and a quoted hostname whose quote never
// closes.
func TestTransferRefusals(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, _, alice, _ := servertest.AdminPair(t, cfg, servertest.WriteAdminFiles(t, nil, nil, nil))

	for _, tc := range []struct {
		input  string
		key    string
		with   []string
		cursor int
	}{
		{"transfer 127.0.0.1 0", "argument.integer.low", []string{"1", "0"}, 19},
		{"transfer 127.0.0.1 65536", "argument.integer.big", []string{"65535", "65536"}, 19},
		{`transfer "unterminated 25565`, "parsing.quote.expected.end", nil, len(`transfer "unterminated 25565`)},
	} {
		alice.ChatCommand(tc.input)
		alice.ExpectCommandError(
			servertest.CommandErrorMessageForTest(tc.key, tc.with...),
			servertest.CommandErrorContextForTest(tc.input, tc.cursor),
		)
		alice.Settle()
	}

	alice.Disconnect()
}
