package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// TestCommandsPacketSentDuringJoin verifies that the Commands packet is sent
// during the join sequence, so the client knows what commands the server offers.
func TestCommandsPacketSentDuringJoin(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Dial(t, cfg)
	c.Login("Testificate")
	c.Configure()

	var sawCommands bool
	for packetID, data := range c.Stream() {
		if packetID == protocol.IDPlayCommands {
			sawCommands = true
			r := protocol.NewReader(data)
			count := r.ReadVarInt()
			if count < 1 {
				t.Errorf("commands packet has %d nodes, want at least 1", count)
			}
			rootIndex := r.ReadVarInt()
			if rootIndex != 0 {
				t.Errorf("root index = %d, want 0", rootIndex)
			}
			if err := r.Err(); err != nil {
				t.Fatalf("parse commands packet: %v", err)
			}
			break
		}
		if packetID == protocol.IDPlayChunkBatchFinished {
			t.Fatal("chunk batch finished before commands packet was sent")
		}
	}
	if !sawCommands {
		t.Fatal("commands packet was never sent")
	}

	_ = srv
	c.Disconnect()
}

// TestListCommand shows the players who are online. With one player, the output
// is "There are 1 of a max of N players online: Name", sent as a system chat
// message carrying the commands.list.players translation key.
func TestListCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate")

	c.ChatCommand("list")
	c.ExpectListOutput(srv.Config.MaxPlayers, []player.Profile{
		{Name: "Testificate", UUID: protocol.OfflineUUID("Testificate")},
	}, false)

	c.Disconnect()
}

// TestListCommandWithMultiplePlayers shows all online players separated by a
// gray ", ", the way ComponentUtils.formatList does in vanilla.
func TestListCommandWithMultiplePlayers(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")

	// Alice sees Bob arrive before she can run a command.
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	// Alice runs /list and sees both players, in the order they joined.
	alice.ChatCommand("list")
	alice.ExpectListOutput(srv.Config.MaxPlayers, []player.Profile{
		{Name: "Alice", UUID: protocol.OfflineUUID("Alice")},
		{Name: "Bob", UUID: protocol.OfflineUUID("Bob")},
	}, false)

	alice.Disconnect()
	bob.Disconnect()
}

// TestListCommandUUIDs shows each player's name followed by their UUID in
// parentheses, the way /list uuids does in vanilla.
func TestListCommandUUIDs(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate")

	c.ChatCommand("list uuids")
	c.ExpectListOutput(srv.Config.MaxPlayers, []player.Profile{
		{Name: "Testificate", UUID: protocol.OfflineUUID("Testificate")},
	}, true)

	c.Disconnect()
}

// TestCommandSuggestions completes the command the player is typing. The client
// normally completes literals itself from the Commands packet, but the server
// answers a request anyway, so that the system works whether the client asks
// or not.
func TestCommandSuggestions(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate")

	for _, tc := range []struct {
		name    string
		text    string
		start   int32
		length  int32
		matches []string
	}{
		{"partial command", "/l", 1, 1, []string{"list"}},
		{"full command", "/list", 1, 4, []string{"list"}},
		{"command with space", "/list ", 6, 0, []string{"uuids"}},
		{"partial subcommand", "/list u", 6, 1, []string{"uuids"}},
		{"full subcommand", "/list uuids", 6, 5, []string{"uuids"}},
		{"nothing matches", "/xyz", 0, 0, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.CommandSuggestion(1, tc.text)
			id, start, length, matches := c.ExpectCommandSuggestions()
			if id != 1 {
				t.Errorf("transaction ID = %d, want 1", id)
			}
			if start != tc.start {
				t.Errorf("start = %d, want %d", start, tc.start)
			}
			if length != tc.length {
				t.Errorf("length = %d, want %d", length, tc.length)
			}
			if !slices.Equal(matches, tc.matches) {
				t.Errorf("matches = %v, want %v", matches, tc.matches)
			}
		})
	}

	c.Disconnect()
}

// TestCommandParseError makes the server reject a command that does not parse:
// a red error message, then a gray line showing the input and a red italic
// "<--[HERE]" pointing at where the parser stopped.
// The cursor is at the end of the input for an incomplete command, at the start
// of the input for an unknown one, and at the start of the offending word for a
// command that matches part of the tree and then fails.
func TestCommandParseError(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		command  string   // the command text, without the leading slash
		errorKey string   // the translation key the first message carries
		with     []string // the values the error message's %s take
		cursor   int      // where the "<--[HERE]" marker points
	}{
		{"incomplete command", "time", "command.unknown.command", nil, 4},
		{"incomplete subcommand", "time set", "command.unknown.command", nil, 8},
		{"unknown command", "xyz", "command.unknown.command", nil, 0},
		{"unknown argument", "time foo", "command.unknown.argument", nil, 5},
		{"unknown argument under list", "list xyz", "command.unknown.argument", nil, 5},
		// The timeline argument is a minecraft:resource, so a name the
		// timeline registry does not hold stops the reading itself, where
		// the word ends.
		{"unknown timeline past cursor 10", "time query timeline foo", "argument.resource.not_found",
			[]string{"minecraft:timeline", "minecraft:timeline"}, 19},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// A config of the subtest's own: start keeps a non-default
			// LevelName, so one shared config would be one shared world.
			cfg := config.Default()
			cfg.OnlineMode = false
			// The /time cases walk a command's own subtree, which needs
			// the runner to be an operator; a listed Testificate offers
			// the whole tree.
			_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

			c.ChatCommand(tc.command)
			c.ExpectCommandError(
				servertest.CommandErrorMessageForTest(tc.errorKey, tc.with...),
				servertest.CommandErrorContextForTest(tc.command, tc.cursor),
			)

			c.Disconnect()
		})
	}
}
