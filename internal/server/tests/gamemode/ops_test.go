package gamemode

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
)

// rootCommandNames lists the literal names at the top of a decoded command
// tree, which is the list of commands the packet offers.
func rootCommandNames(t *testing.T, nodes []servertest.TestCommandNode) []string {
	t.Helper()
	if len(nodes) == 0 || nodes[0].Typ != 0 {
		t.Fatal("commands packet has no root node")
	}
	var names []string
	for _, index := range nodes[0].Children {
		node := nodes[index]
		if node.Typ == 1 {
			names = append(names, node.Name)
		}
	}
	return names
}

// TestOpsFileGatesTheCommands checks that a player listed in ops.json is
// offered the commands their level allows, and an unlisted player none of
// the ones that change the world. The tree a non-op is sent is what keeps
// them from tab-completing their way to a command the server would refuse.
func TestOpsFileGatesTheCommands(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg, servertest.WriteOpsFile(t,
		servertest.OpRecord{UUID: protocol.OfflineUUID("Alice").String(), Name: "Alice", Level: 2},
	))

	alice := connect()
	alice.Login("Alice")
	alice.Configure()
	// The Commands packet is the sixth packet of the join, and the tree it
	// carries is the one to judge.
	names := rootCommandNames(t, servertest.DecodeCommands(t, alice.AwaitPlay(protocol.IDPlayCommands)))
	for _, want := range []string{"list", "time", "gamemode", "give", "kill", "clear", "difficulty", "weather", "setblock", "seed", "say", "spawnpoint", "setworldspawn", "teleport", "tp"} {
		if !slices.Contains(names, want) {
			t.Errorf("commands offered to a gamemaster lack %q: %v", want, names)
		}
	}
	for _, absent := range []string{"op", "deop"} {
		if slices.Contains(names, absent) {
			t.Errorf("commands offered to a gamemaster include %q: %v", absent, names)
		}
	}
	alice.SkipJoin()
	alice.Disconnect()

	// Nobody the file does not name holds anything, and the tree they are
	// sent says so.
	bob := connect()
	bob.Login("Bob")
	bob.Configure()
	names = rootCommandNames(t, servertest.DecodeCommands(t, bob.AwaitPlay(protocol.IDPlayCommands)))
	for _, absent := range []string{"time", "gamemode", "give", "kill", "clear", "difficulty", "weather", "op", "deop", "setblock", "say", "seed", "teleport", "tp"} {
		if slices.Contains(names, absent) {
			t.Errorf("commands offered to a non-op include %q: %v", absent, names)
		}
	}
	if !slices.Contains(names, "list") {
		t.Errorf("commands offered to a non-op lack list: %v", names)
	}
	bob.SkipJoin()

	// And the server refuses the command on the wire, the way it would
	// refuse one it has never heard of.
	bob.ChatCommand("time set 1000")
	bob.ExpectCommandError(
		servertest.CommandErrorMessageForTest("command.unknown.command"),
		servertest.CommandErrorContextForTest("time set 1000", 0),
	)
	bob.Disconnect()
}

// TestOpCommand makes another player an operator: their level rises, their
// client is told in the entity event that carries it, their command tree
// grows the commands that level allows, and the list on disk gains them.
func TestOpCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, servertest.WriteOpsFile(t,
		servertest.OpRecord{UUID: protocol.OfflineUUID("Alice").String(), Name: "Alice", Level: 4},
	))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")

	// Alice sees Bob arrive before she can promote him.
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.ChatCommand("op Bob")
	alice.ExpectCommandFeedback("commands.op.success", "Bob")

	// Bob's client learns its new level in the entity event, and the tree
	// that goes with it carries the commands the level unlocks. They are
	// queued in that order.
	names := rootCommandNames(t, servertest.DecodeCommands(t, bob.ExpectPlay(protocol.IDPlayCommands)))
	if !slices.Contains(names, "op") || !slices.Contains(names, "time") {
		t.Errorf("commands offered to a fresh operator = %v, want op and time among them", names)
	}
	event := protocol.NewReader(bob.ExpectPlay(protocol.IDPlayEntityEvent))
	event.ReadI32() // Bob's own entity, which the event is played on
	if got := event.ReadU8(); got != 24+4 {
		t.Errorf("entity event status = %d, want %d (permission level 4)", got, 24+4)
	}

	// The promotion is on disk, where a restart finds it.
	records := servertest.ReadOpsFile(t, srv)
	if len(records) != 2 {
		t.Fatalf("ops.json holds %d entries, want 2", len(records))
	}
	found := map[string]int{}
	for _, record := range records {
		found[record.Name] = record.Level
	}
	if found["Bob"] != 4 || found["Alice"] != 4 {
		t.Errorf("ops.json = %+v, want Alice and Bob at level 4", records)
	}

	alice.Disconnect()
	bob.Disconnect()
}
