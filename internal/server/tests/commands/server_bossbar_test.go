package commands

import (
	"compress/gzip"
	"github.com/golem-mc/golem/internal/server/servertest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// The Boss Bar packet's payloads, as this server builds them: the add
// carries the bar's identity, its title, its fill and the three style fields
// this server holds fixed.

// expectBossBarAdd reads the Boss Event that adds a bar to this client and
// checks its fields, answering the bar's identity for the packets after it.
func expectBossBarAdd(t *testing.T, c *servertest.Client, title string, fill float32) uuid.UUID {
	t.Helper()
	return checkBossBarAdd(t, c.ExpectPlay(protocol.IDPlayBossEvent), title, fill)
}

// checkBossBarAdd checks the payload of an add, answering the bar's identity.
func checkBossBarAdd(t *testing.T, data []byte, title string, fill float32) uuid.UUID {
	t.Helper()
	r := protocol.NewReader(data)
	bar := r.ReadUUID()
	if action := r.ReadVarInt(); action != protocol.BossEventAdd {
		t.Errorf("boss event action = %d, want add (%d)", action, protocol.BossEventAdd)
	}
	name, ok := protocol.ReadText(r)
	if !ok {
		t.Fatalf("boss event title did not read back")
	}
	if got := name.Plain(); got != title {
		t.Errorf("boss event title = %q, want %q", got, title)
	}
	if got := r.ReadF32(); got != fill {
		t.Errorf("boss event fill = %v, want %v", got, fill)
	}
	if got := r.ReadVarInt(); got != 6 {
		t.Errorf("boss event colour = %d, want 6 (white)", got)
	}
	if got := r.ReadVarInt(); got != 0 {
		t.Errorf("boss event division = %d, want 0 (undivided)", got)
	}
	if got := r.ReadU8(); got != 0 {
		t.Errorf("boss event flags = %#02x, want none", got)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("boss event add: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("boss event add carries %d bytes past its fields", r.Remaining())
	}
	return bar
}

// expectBossBarProgress reads the Boss Event that moves a bar's fill and
// checks it names the bar and lands on the fill given.
func expectBossBarProgress(t *testing.T, c *servertest.Client, bar uuid.UUID, fill float32) {
	t.Helper()
	data := c.ExpectPlay(protocol.IDPlayBossEvent)
	r := protocol.NewReader(data)
	if got := r.ReadUUID(); got != bar {
		t.Errorf("boss event names bar %s, want %s", got, bar)
	}
	if action := r.ReadVarInt(); action != protocol.BossEventProgress {
		t.Errorf("boss event action = %d, want progress (%d)", action, protocol.BossEventProgress)
	}
	if got := r.ReadF32(); got != fill {
		t.Errorf("boss event fill = %v, want %v", got, fill)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("boss event progress: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("boss event progress carries %d bytes past its fields", r.Remaining())
	}
}

// expectBossBarRemove reads the Boss Event that takes a bar off this client
// and checks it names the bar.
func expectBossBarRemove(t *testing.T, c *servertest.Client, bar uuid.UUID) {
	t.Helper()
	data := c.ExpectPlay(protocol.IDPlayBossEvent)
	r := protocol.NewReader(data)
	if got := r.ReadUUID(); got != bar {
		t.Errorf("boss event names bar %s, want %s", got, bar)
	}
	if action := r.ReadVarInt(); action != protocol.BossEventRemove {
		t.Errorf("boss event action = %d, want remove (%d)", action, protocol.BossEventRemove)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("boss event remove: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("boss event remove carries %d bytes past its fields", r.Remaining())
	}
}

// expectNoBossEvent reads for a short while and fails should any Boss Event
// arrive, for the turns a command takes that send nothing.
func expectNoBossEvent(t *testing.T, srv *server.Server, c *servertest.Client) {
	t.Helper()
	c.ExpectNoneWithinTicks(srv, 5, servertest.PacketIs(protocol.IDPlayBossEvent))
}

// bossbarTwoPlayers joins the operator that runs the /bossbar lines and a
// second player the bar is put on, and answers each client once the two can
// see each other.
func bossbarTwoPlayers(t *testing.T) (srv *server.Server, alice, bob *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Alice"))
	alice = connect()
	alice.Join("Alice")
	bob = connect()
	bob.Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")
	return srv, alice, bob
}

// testBarDisplay is how feedback calls the bar the tests make: its name in
// brackets, drawn in the bar's colour with the id as the insertion.
// vanilla: CustomBossEvent.getDisplayName
var testBarDisplay = protocol.Text{
	Text: "[", Extra: []protocol.Text{{Text: "The Test Bar"}, {Text: "]"}},
	Color: "white", Insertion: "test:bar", HoverText: &protocol.Text{Text: "test:bar"},
}

// numberText is shared with the admin tests (server_admin_test.go): a
// number as a feedback argument.

// TestBossbarAddAndSetPlayers runs /bossbar add and /bossbar set players: the
// players the command names are sent the add of the bar, whoever it takes
// them off the remove, and the one bar keeps its identity through both.
func TestBossbarAddAndSetPlayers(t *testing.T) {
	t.Parallel()
	srv, alice, bob := bossbarTwoPlayers(t)

	alice.ChatCommand(`bossbar add test:bar "The Test Bar"`)
	alice.ExpectCommandFeedbackText("commands.bossbar.create.success", testBarDisplay)
	expectNoBossEvent(t, srv, alice)

	alice.ChatCommand("bossbar set test:bar players Bob")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.players.success.some",
		testBarDisplay, servertest.NumberText(1), servertest.PlayerDisplayComponent("Bob"))
	bar := expectBossBarAdd(t, bob, "The Test Bar", 0)

	// Moving the bar onto another player sends them the same bar, identity
	// and title included, and takes the old player off it.
	alice.ChatCommand("bossbar set test:bar players Alice")
	// The add reaches the player ahead of the command's own feedback.
	if got := expectBossBarAdd(t, alice, "The Test Bar", 0); got != bar {
		t.Errorf("the re-add named bar %s, want the one bar %s", got, bar)
	}
	alice.ExpectCommandFeedbackText("commands.bossbar.set.players.success.some",
		testBarDisplay, servertest.NumberText(1), servertest.PlayerDisplayComponent("Alice"))
	expectBossBarRemove(t, bob, bar)

	// Naming the same players again changes nothing, and sends nothing.
	alice.ChatCommand("bossbar set test:bar players Alice")
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.set.players.unchanged"))
	expectNoBossEvent(t, srv, alice)
	expectNoBossEvent(t, srv, bob)

	alice.Disconnect()
	bob.Disconnect()
}

// TestBossbarSetValue runs /bossbar set value: the fill the value puts the
// bar at reaches the players on it, and a value the bar is already at sends
// nothing and answers with its unchanged message.
func TestBossbarSetValue(t *testing.T) {
	t.Parallel()
	srv, alice, bob := bossbarTwoPlayers(t)

	alice.ChatCommand(`bossbar add test:bar "The Test Bar"`)
	alice.ExpectCommandFeedbackText("commands.bossbar.create.success", testBarDisplay)
	alice.ChatCommand("bossbar set test:bar players Bob")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.players.success.some",
		testBarDisplay, servertest.NumberText(1), servertest.PlayerDisplayComponent("Bob"))
	bar := expectBossBarAdd(t, bob, "The Test Bar", 0)

	alice.ChatCommand("bossbar set test:bar value 50")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.value.success",
		testBarDisplay, servertest.NumberText(50))
	expectBossBarProgress(t, bob, bar, 0.5)

	alice.ChatCommand("bossbar set test:bar value 50")
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.set.value.unchanged"))
	expectNoBossEvent(t, srv, bob)

	// The value's bound is the walk's: a value below the zero the argument
	// starts at is refused where it was typed.
	input := "bossbar set test:bar value -7"
	alice.ChatCommand(input)
	alice.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.integer.low", "0", "-7"),
		servertest.CommandErrorContextForTest(input, strings.Index(input, "-7")),
	)

	alice.Disconnect()
	bob.Disconnect()
}

// TestBossbarSetVisible runs /bossbar set visible: hiding a bar removes it
// from the players on it without taking them off it, and showing it again
// adds it back with the fill and title they last saw.
func TestBossbarSetVisible(t *testing.T) {
	t.Parallel()
	srv, alice, bob := bossbarTwoPlayers(t)

	alice.ChatCommand(`bossbar add test:bar "The Test Bar"`)
	alice.ExpectCommandFeedbackText("commands.bossbar.create.success", testBarDisplay)
	alice.ChatCommand("bossbar set test:bar players Bob")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.players.success.some",
		testBarDisplay, servertest.NumberText(1), servertest.PlayerDisplayComponent("Bob"))
	bar := expectBossBarAdd(t, bob, "The Test Bar", 0)

	alice.ChatCommand("bossbar set test:bar visible false")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.visible.success.hidden", testBarDisplay)
	expectBossBarRemove(t, bob, bar)

	alice.ChatCommand("bossbar set test:bar visible false")
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.set.visibility.unchanged.hidden"))
	expectNoBossEvent(t, srv, bob)

	// The players the bar still names are who it comes back to.
	alice.ChatCommand("bossbar set test:bar visible true")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.visible.success.visible", testBarDisplay)
	if got := expectBossBarAdd(t, bob, "The Test Bar", 0); got != bar {
		t.Errorf("the re-add named bar %s, want the one bar %s", got, bar)
	}

	alice.Disconnect()
	bob.Disconnect()
}

// TestBossbarRefusals checks the words the command answers with when it
// will not run: a name that names no bar, an id already taken, a player the
// targets cannot resolve, and a non-op offered no tree to run it through.
func TestBossbarRefusals(t *testing.T) {
	t.Parallel()
	_, alice, bob := bossbarTwoPlayers(t)

	alice.ChatCommand("bossbar set nosuch value 3")
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.unknown", "minecraft:nosuch"))

	alice.ChatCommand(`bossbar add test:bar "The Test Bar"`)
	alice.ExpectCommandFeedbackText("commands.bossbar.create.success", testBarDisplay)
	alice.ChatCommand(`bossbar add test:bar "Another Bar"`)
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.create.failed", "test:bar"))

	// Targets that name nobody are no refusal: the bar names nobody either
	// way, which is no change.
	alice.ChatCommand("bossbar set test:bar players Nobody")
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.set.players.unchanged"))

	// A player without the permission the command asks is offered no
	// /bossbar at all, and the server answers the command as unknown.
	input := "bossbar add test:bar2 Bob's Bar"
	bob.ChatCommand(input)
	bob.ExpectCommandError(
		servertest.CommandErrorMessageForTest("command.unknown.command"),
		servertest.CommandErrorContextForTest(input, 0),
	)

	alice.Disconnect()
	bob.Disconnect()
}

// bossBarsPath is where the world under root keeps its custom boss bars.
func bossBarsPath(root string) string {
	return filepath.Join(root, "data", "minecraft", "custom_boss_events.dat")
}

// readBossBarsFile reads the bars the world under root saved, the data
// compound of custom_boss_events.dat.
func readBossBarsFile(t *testing.T, root string) nbt.Compound {
	t.Helper()
	f, err := os.Open(bossBarsPath(root))
	if err != nil {
		t.Fatalf("open the boss bars file: %v", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gunzip the boss bars file: %v", err)
	}
	defer gz.Close()
	var raw []byte
	buf := make([]byte, 4096)
	for {
		n, err := gz.Read(buf)
		raw = append(raw, buf[:n]...)
		if err != nil {
			break
		}
	}
	_, tag, err := nbt.Decode(raw)
	if err != nil {
		t.Fatalf("decode the boss bars file: %v", err)
	}
	data, ok := nbt.Get[nbt.Compound](tag.(nbt.Compound), "data")
	if !ok {
		t.Fatal("the boss bars file has no data compound")
	}
	return data
}

// TestBossbarSurvivesARestart checks that a custom bar is saved with the
// world, as vanilla saves it to data/minecraft/custom_boss_events.dat, and
// is back after a restart: the id is taken, and a player the bar names is
// sent it at their next join with the value it was left at.
func TestBossbarSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	alice := connect().Join("Alice")
	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.ChatCommand(`bossbar add test:bar "The Test Bar"`)
	alice.ExpectCommandFeedbackText("commands.bossbar.create.success", testBarDisplay)
	alice.ChatCommand("bossbar set test:bar players Bob")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.players.success.some",
		testBarDisplay, servertest.NumberText(1), servertest.PlayerDisplayComponent("Bob"))
	expectBossBarAdd(t, bob, "The Test Bar", 0)
	alice.ChatCommand("bossbar set test:bar value 25")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.value.success", testBarDisplay, servertest.NumberText(25))

	srv.Shutdown()

	// Vanilla's packed form: the fields at their default are left out, so
	// this bar (white, progress, max 100, no flags) names its value, its
	// visibility, its name and the players.
	bar, ok := nbt.Get[nbt.Compound](readBossBarsFile(t, cfg.LevelName), "test:bar")
	if !ok {
		t.Fatal("the saved world has no test:bar")
	}
	if v, _ := nbt.Get[nbt.Int](bar, "Value"); v != 25 {
		t.Errorf("saved Value = %d, want 25", v)
	}
	if v, _ := nbt.Get[nbt.Byte](bar, "Visible"); v != 1 {
		t.Errorf("saved Visible = %d, want 1", v)
	}
	if v, _ := nbt.Get[nbt.String](bar, "Name"); v != "The Test Bar" {
		t.Errorf("saved Name = %q, want The Test Bar", v)
	}
	if players, _ := nbt.Get[nbt.List](bar, "Players"); len(players.Items) != 1 {
		t.Errorf("saved %d players, want 1", len(players.Items))
	}
	for _, field := range []string{"Max", "Color", "Overlay"} {
		if _, ok := bar[field]; ok {
			t.Errorf("saved %s at its default", field)
		}
	}

	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted, servertest.OpsListing(t, "Alice"))
	again := reconnect().Join("Alice")
	again.ChatCommand(`bossbar add test:bar "Another Bar"`)
	again.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.create.failed", "test:bar"))

	returned := reconnect()
	returned.Login("Bob")
	returned.Configure()
	var data []byte
	returned.ReadJoin(func(id int32, packet []byte) {
		if id == protocol.IDPlayBossEvent && data == nil {
			data = packet
		}
	})
	if data == nil {
		t.Fatal("the rejoining player was sent no boss event after the restart")
	}
	checkBossBarAdd(t, data, "The Test Bar", 0.25)
}
