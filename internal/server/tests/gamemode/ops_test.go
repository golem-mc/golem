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

// Opping a player while their own connection runs commands changes their
// level from another goroutine; under -race this must not race.
func TestOpWhileTheTargetRunsCommands(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg, servertest.WriteOpsFile(t,
		servertest.OpRecord{UUID: protocol.OfflineUUID("Alice").String(), Name: "Alice", Level: 4},
	))

	alice := connect().Join("Alice")
	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	for range 20 {
		bob.ChatCommand("time query daytime")
	}
	alice.ChatCommand("op Bob")
	for range 20 {
		bob.ChatCommand("time query daytime")
	}
	alice.ExpectCommandFeedback("commands.op.success", "Bob")
}

// TestOpSelectorWithSpacesInItsBrackets checks the targets argument read
// whole: a selector whose options are parted by spaces promotes the player it
// names, where reading only its first word would refuse the command.
func TestOpSelectorWithSpacesInItsBrackets(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, servertest.WriteOpsFile(t,
		servertest.OpRecord{UUID: protocol.OfflineUUID("Alice").String(), Name: "Alice", Level: 4},
	))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.ChatCommand("op @a[ name=Bob ]")
	alice.ExpectCommandFeedback("commands.op.success", "Bob")

	records := servertest.ReadOpsFile(t, srv)
	found := false
	for _, record := range records {
		if record.Name == "Bob" {
			found = true
		}
	}
	if !found {
		t.Errorf("ops.json = %+v, want Bob among them", records)
	}

	alice.Disconnect()
	bob.Disconnect()
}

// TestDeopCommand takes the permission level away again: the demoted
// player's tree loses the commands and their level event drops to nothing.
func TestDeopCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, servertest.WriteOpsFile(t,
		servertest.OpRecord{UUID: protocol.OfflineUUID("Alice").String(), Name: "Alice", Level: 4},
		servertest.OpRecord{UUID: protocol.OfflineUUID("Bob").String(), Name: "Bob", Level: 2},
	))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.ChatCommand("deop Bob")
	alice.ExpectCommandFeedback("commands.deop.success", "Bob")

	names := rootCommandNames(t, servertest.DecodeCommands(t, bob.ExpectPlay(protocol.IDPlayCommands)))
	for _, absent := range []string{"time", "gamemode", "op"} {
		if slices.Contains(names, absent) {
			t.Errorf("commands offered to a deopped player include %q: %v", absent, names)
		}
	}
	event := protocol.NewReader(bob.ExpectPlay(protocol.IDPlayEntityEvent))
	event.ReadI32()
	if got := event.ReadU8(); got != 24 {
		t.Errorf("entity event status = %d, want %d (permission level 0)", got, 24)
	}

	// The demotion is on disk as well.
	records := servertest.ReadOpsFile(t, srv)
	for _, record := range records {
		if record.Name == "Bob" {
			t.Errorf("ops.json still lists %s: %+v", record.Name, records)
		}
	}

	alice.Disconnect()
	bob.Disconnect()
}

// TestNoOpsFileMeansNoOperators: a server whose operator list has never
// been written has no operators at all. A vanilla server's missing ops.json
// reads as an empty list, and op-permission-level is only the level a /op
// grants, not a level anyone holds by default - so the commands tree the
// joining player is sent carries only what a vanilla server offers a
// non-op, and every command above that level is refused with the
// unknown-command error any tree the client should never have walked
// answers with.
func TestNoOpsFileMeansNoOperators(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg, servertest.WriteOpsFile(t))

	c := connect()
	c.Login("Visitor")
	c.Configure()

	// The tree is the level-0 commands, and nothing above them. This is
	// the same list the vanilla reference server sends a player it has
	// never made an operator of (probed as Visitor).
	names := rootCommandNames(t, servertest.DecodeCommands(t, c.AwaitPlay(protocol.IDPlayCommands)))
	sorted := slices.Clone(names)
	slices.Sort(sorted)
	if !slices.Equal(sorted, []string{"?", "bukkit:help", "help", "list", "me", "msg", "random", "teammsg", "tell", "tm", "trigger", "w"}) {
		t.Errorf("commands offered without an ops.json = %v, want ?, bukkit:help, help, list, me, msg, random, teammsg, tell, tm, trigger and w", names)
	}
	c.SkipJoin()

	// The refusals on the wire, the way the vanilla server refuses a
	// command it never offered: as an unknown command, the context
	// pointing at the first word.
	c.ChatCommand("time set 1000")
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("command.unknown.command"),
		servertest.CommandErrorContextForTest("time set 1000", 0),
	)
	c.ChatCommand("op Bob")
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("command.unknown.command"),
		servertest.CommandErrorContextForTest("op Bob", 0),
	)
	c.Disconnect()
}

// TestOpAndDeopResolveAnOfflineName: in offline mode a name that never joined
// still resolves, as vanilla's GameProfileArgument resolves it against the
// offline name cache, so the console can list a player before their first
// join. The grant writes the offline UUID to ops.json, a repeat says nothing
// changed, and /deop takes the same offline name back off.
func TestOpAndDeopResolveAnOfflineName(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, _ := servertest.Start(t, cfg, servertest.WriteOpsFile(t))

	// With no list yet the first grant writes one holding the target alone;
	// the console is never listed.
	reply := srv.RunLine("op Nobody")
	if len(reply) != 1 || reply[0].Plain() != "Made Nobody a server operator" {
		t.Fatalf("op Nobody answered %v, want Made Nobody a server operator", reply)
	}
	nobody := protocol.OfflineUUID("Nobody").String()
	records := servertest.ReadOpsFile(t, srv)
	if len(records) != 1 {
		t.Fatalf("ops.json = %+v, want exactly the Nobody entry", records)
	}
	listed := false
	for _, record := range records {
		if record.Name == "Nobody" {
			listed = true
			if record.UUID != nobody {
				t.Errorf("Nobody's uuid = %q, want the offline UUID %q", record.UUID, nobody)
			}
			if record.Level != cfg.OpPermissionLevel {
				t.Errorf("Nobody's level = %d, want %d", record.Level, cfg.OpPermissionLevel)
			}
		}
	}
	if !listed {
		t.Fatalf("ops.json = %+v, want an entry for Nobody", records)
	}

	// A repeat is a no-op: the list already names the offline player.
	reply = srv.RunLine("op Nobody")
	if len(reply) != 1 || reply[0].Plain() != "Nothing changed. The player is already an operator" {
		t.Errorf("repeat op Nobody answered %v, want the nothing-changed failure", reply)
	}

	reply = srv.RunLine("deop Nobody")
	if len(reply) != 1 || reply[0].Plain() != "Made Nobody no longer a server operator" {
		t.Errorf("deop Nobody answered %v, want Made Nobody no longer a server operator", reply)
	}
	for _, record := range servertest.ReadOpsFile(t, srv) {
		if record.Name == "Nobody" {
			t.Errorf("ops.json still lists Nobody: %+v", record)
		}
	}
}

// TestOpOnAListedPlayerKeepsItsLevel: the list naming a player is what makes
// them an operator, whatever level their entry holds, so /op on a listed
// player nothing changes for is refused instead of raising the entry to the
// default. vanilla's OpCommand skips every target PlayerList.isOp accepts.
func TestOpOnAListedPlayerKeepsItsLevel(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, _ := servertest.Start(t, cfg, servertest.WriteOpsFile(t,
		servertest.OpRecord{UUID: protocol.OfflineUUID("Alice").String(), Name: "Alice", Level: 4},
		servertest.OpRecord{UUID: protocol.OfflineUUID("Bob").String(), Name: "Bob", Level: 1},
	))

	reply := srv.RunLine("op Bob")
	if len(reply) != 1 || reply[0].Plain() != "Nothing changed. The player is already an operator" {
		t.Fatalf("op on a listed player answered %v, want the nothing-changed failure", reply)
	}

	var bob *servertest.OpRecord
	records := servertest.ReadOpsFile(t, srv)
	for i := range records {
		if records[i].Name == "Bob" {
			bob = &records[i]
		}
	}
	if bob == nil {
		t.Fatalf("ops.json no longer lists Bob: %+v", records)
	}
	if bob.Level != 1 {
		t.Errorf("Bob's level = %d, want the listed 1", bob.Level)
	}
}

// TestDeopRemovesALevelZeroEntry: a listed entry is an operator for membership
// whatever level it holds, so /deop takes a level-0 entry off. vanilla's
// OpCommand tests isOp, which an entry at level 0 answers.
func TestDeopRemovesALevelZeroEntry(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, _ := servertest.Start(t, cfg, servertest.WriteOpsFile(t,
		servertest.OpRecord{UUID: protocol.OfflineUUID("Alice").String(), Name: "Alice", Level: 4},
		servertest.OpRecord{UUID: protocol.OfflineUUID("Bob").String(), Name: "Bob", Level: 0},
	))

	reply := srv.RunLine("deop Bob")
	if len(reply) != 1 || reply[0].Plain() != "Made Bob no longer a server operator" {
		t.Fatalf("deop of a level-0 entry answered %v, want the success", reply)
	}
	for _, record := range servertest.ReadOpsFile(t, srv) {
		if record.Name == "Bob" {
			t.Errorf("ops.json still lists Bob: %+v", record)
		}
	}
}

// TestOpAndDeopSwitchTheTreeACommandRunsAgainst: a player's commands are
// matched against the tree of the level they hold now, through a /op and a
// /deop in one session, after the lower level's tree has already served them.
func TestOpAndDeopSwitchTheTreeACommandRunsAgainst(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, bob := servertest.Join(t, cfg, "Bob", servertest.WriteOpsFile(t))

	refused := func() {
		t.Helper()
		bob.ChatCommand("setblock 1 63 1 minecraft:glass")
		bob.ExpectCommandError(
			servertest.CommandErrorMessageForTest("command.unknown.command"),
			servertest.CommandErrorContextForTest("setblock 1 63 1 minecraft:glass", 0),
		)
	}
	refused()

	// The tree and the entity event announcing the new level come first,
	// then the grey admin line Bob now reads as an operator.
	srv.RunRconLine("op Bob")
	bob.ExpectPlay(protocol.IDPlayCommands)
	bob.ExpectPlay(protocol.IDPlayEntityEvent)
	bob.ExpectPlay(protocol.IDPlaySystemChat)
	bob.ChatCommand("setblock 1 63 1 minecraft:glass")
	bob.ExpectCommandFeedback("commands.setblock.success", "1", "63", "1")

	srv.RunRconLine("deop Bob")
	bob.ExpectPlay(protocol.IDPlayCommands)
	bob.ExpectPlay(protocol.IDPlayEntityEvent)
	refused()

	bob.Disconnect()
}
