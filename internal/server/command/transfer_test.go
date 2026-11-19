package command

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// TestTransferTreeMatchesVanilla holds /transfer's nodes to the vanilla
// tree: an executable quotable-phrase hostname, a port bounded to the port
// range, and a players-only entity argument, none of them asking the client
// for suggestions.
// vanilla: TransferCommand.register
func TestTransferTreeMatchesVanilla(t *testing.T) {
	if Transfer.Level != PermLevelAdmins {
		t.Errorf("transfer permission = %d, want the admins level", Transfer.Level)
	}
	hostname := Transfer.Build().Children[0]
	if !hostname.Executable || hostname.Parser != ParserString || !hostname.Quotable {
		t.Errorf("hostname node = %+v, want an executable quotable string", hostname)
	}
	if hostname.Properties == nil {
		t.Fatal("hostname node carries no string-mode properties")
	}
	var b protocol.Buffer
	hostname.Properties(&b)
	if got := protocol.NewReader(b.Bytes()).ReadVarInt(); got != 1 {
		t.Errorf("hostname string mode = %d, want the quotable phrase 1", got)
	}
	port := hostname.Children[0]
	if !port.Executable || port.Int == nil ||
		!port.Int.HasMin || port.Int.Min != 1 ||
		!port.Int.HasMax || port.Int.Max != 65535 {
		t.Errorf("port node = %+v, want an executable integer bounded to 1..65535", port)
	}
	players := port.Children[0]
	if !players.Executable || players.EntityArg == nil || !players.EntityArg.PlayersOnly ||
		players.Suggests != "" {
		t.Errorf("players node = %+v, want an executable players-only argument without suggestions", players)
	}
}

// TestTransferWalk checks the walk of whole /transfer inputs against the
// answers of the vanilla server: which it accepts, and the error and
// cursor each refusal carries.
func TestTransferWalk(t *testing.T) {
	tree := ForLevel(PermLevelOwners)

	for _, input := range []string{
		"transfer example.org",
		"transfer example.org 25565",
		"transfer example.org 25565 Alice",
		`transfer "a b" 25565`,
		`transfer 'a b' 25565`,
		`transfer "" 25565`,
		"transfer host 1 @a",
	} {
		if node, _ := Match(tree, input, Tokenize(input), true); node == nil {
			t.Errorf("%q: refused, want accepted", input)
		}
	}

	for _, tc := range []struct {
		input  string
		key    string
		with   []string
		cursor int
	}{
		{"transfer", "command.unknown.command", nil, len("transfer")},
		{"transfer 127.0.0.1 0", keyIntegerLow, []string{"1", "0"}, 19},
		{"transfer 127.0.0.1 65536", keyIntegerBig, []string{"65535", "65536"}, 19},
		{"transfer 127.0.0.1 70000", keyIntegerBig, []string{"65535", "70000"}, 19},
		{`transfer "host`, keyQuoteExpectedEnd, nil, len(`transfer "host`)},
		{`transfer "host 25565`, keyQuoteExpectedEnd, nil, len(`transfer "host 25565`)},
		{`transfer "a b"c 25565`, keyExpectedSeparator, nil, 14},
		{"transfer host 25565 @e", keyOnlyPlayers, nil, 0},
	} {
		node, fail := Match(tree, tc.input, Tokenize(tc.input), true)
		if node != nil {
			t.Errorf("%q: accepted, want refused", tc.input)
			continue
		}
		key, with, cursor := failureAnswer(tc.input, fail)
		if key != tc.key || !slices.Equal(with, tc.with) || cursor != tc.cursor {
			t.Errorf("%q: got %s %q at %d, want %s %q at %d", tc.input, key, with, cursor, tc.key, tc.with, tc.cursor)
		}
	}
}
