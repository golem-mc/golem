package commands

import (
	"bytes"
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

// TestBossbarJoinReceivesBarsThePlayerIsOn checks that a joining player is
// sent the add of every bar naming them: the bar survives their departure,
// and its identity, its title and the fill they last left it at come with it.
func TestBossbarJoinReceivesBarsThePlayerIsOn(t *testing.T) {
	t.Parallel()
	_, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Alice"))
	alice := connect().Join("Alice")
	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

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

	bob.Disconnect()

	// Bob's second join is sent the bar naming him ahead of the chunk
	// stream: the identity, the title and the fill they last left it at.
	returned := connect()
	returned.Login("Bob")
	returned.Configure()
	var data []byte
	returned.ReadJoin(func(id int32, packet []byte) {
		if id == protocol.IDPlayBossEvent && data == nil {
			data = packet
		}
	})
	if data == nil {
		t.Fatal("the rejoining player was sent no boss event with the join")
	}
	if got := checkBossBarAdd(t, data, "The Test Bar", 0.5); got != bar {
		t.Errorf("the join's add named bar %s, want the one bar %s", got, bar)
	}

	returned.Disconnect()
	alice.Disconnect()
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

// TestBossbarNameIsAComponent checks that the name argument is one text
// component: a quoted or braced component is the name, and bare words after
// a first word are trailing data, not a longer name.
func TestBossbarNameIsAComponent(t *testing.T) {
	t.Parallel()
	_, alice, bob := bossbarTwoPlayers(t)

	input := "bossbar add test:bar The Test Bar"
	alice.ChatCommand(input)
	alice.ExpectCommandError(
		servertest.CommandErrorMessageForTest("command.unknown.argument"),
		servertest.CommandErrorContextForTest(input, strings.Index(input, "Test")),
	)

	alice.ChatCommand(`bossbar add test:bar {text:"Styled",bold:true}`)
	alice.ExpectCommandFeedbackText("commands.bossbar.create.success", protocol.Text{
		Text: "[", Extra: []protocol.Text{boldText("Styled"), {Text: "]"}},
		Color: "white", Insertion: "test:bar", HoverText: &protocol.Text{Text: "test:bar"},
	})

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

// TestBossbarReadsAVanillaFile checks this server against a
// custom_boss_events.dat a vanilla server wrote: its bars load, what
// this server does not model (the maximum, the style, a bold name) is still
// in the file after a save, and a bar's fill is its value against that
// maximum.
func TestBossbarReadsAVanillaFile(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	fixture, err := os.ReadFile(filepath.Join("testdata", "vanilla-custom-boss-events.dat"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(bossBarsPath(cfg.LevelName)), 0o755); err != nil {
		t.Fatalf("create world: %v", err)
	}
	if err := os.WriteFile(bossBarsPath(cfg.LevelName), fixture, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	srv, alice := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	alice.ChatCommand(`bossbar add t:two "again"`)
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.create.failed", "t:two"))

	// The value 5000 stands against a maximum of 200: a bar over-full shows
	// full, and the add names the style the file kept.
	alice.ChatCommand("bossbar set test:bar players Alice")
	data := alice.ExpectPlay(protocol.IDPlayBossEvent)
	r := protocol.NewReader(data)
	r.ReadUUID()
	if action := r.ReadVarInt(); action != protocol.BossEventAdd {
		t.Fatalf("boss event action = %d, want add", action)
	}
	if name, ok := protocol.ReadText(r); !ok || name.Plain() != "New Name" {
		t.Errorf("boss event title = %q, want New Name", name.Plain())
	}
	if got := r.ReadF32(); got != 1 {
		t.Errorf("boss event fill = %v, want 1", got)
	}
	if got := r.ReadVarInt(); got != 6 {
		t.Errorf("boss event colour = %d, want 6 (white)", got)
	}
	if got := r.ReadVarInt(); got != 2 {
		t.Errorf("boss event division = %d, want 2 (notched_10)", got)
	}
	alice.ExpectCommandFeedbackText("commands.bossbar.set.players.success.some",
		protocol.Text{
			Text: "[", Extra: []protocol.Text{{Text: "New Name"}, {Text: "]"}},
			Color: "white", Insertion: "test:bar", HoverText: &protocol.Text{Text: "test:bar"},
		}, servertest.NumberText(1), servertest.PlayerDisplayComponent("Alice"))

	alice.ChatCommand("bossbar set t:two value 7")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.value.success", protocol.Text{
		Text: "[", Extra: []protocol.Text{protocol.Text{Raw: nbt.Compound{"text": nbt.String("Styled"), "bold": nbt.Byte(1), "color": nbt.String("gold")}}, {Text: "]"}},
		Color: "white", Insertion: "t:two", HoverText: &protocol.Text{Text: "t:two"},
	}, servertest.NumberText(7))

	srv.Shutdown()

	saved := readBossBarsFile(t, cfg.LevelName)
	testBar, _ := nbt.Get[nbt.Compound](saved, "test:bar")
	if v, _ := nbt.Get[nbt.Int](testBar, "Max"); v != 200 {
		t.Errorf("saved Max = %d, want 200", v)
	}
	if v, _ := nbt.Get[nbt.String](testBar, "Overlay"); v != "notched_10" {
		t.Errorf("saved Overlay = %q, want notched_10", v)
	}
	if v, _ := nbt.Get[nbt.Int](testBar, "Value"); v != 5000 {
		t.Errorf("saved Value = %d, want 5000", v)
	}
	two, _ := nbt.Get[nbt.Compound](saved, "t:two")
	name, _ := nbt.Get[nbt.Compound](two, "Name")
	if v, _ := nbt.Get[nbt.Byte](name, "bold"); v != 1 {
		t.Errorf("saved name lost its bold: %v", name)
	}
	if v, _ := nbt.Get[nbt.String](name, "color"); v != "gold" {
		t.Errorf("saved name lost its colour: %v", name)
	}
	if v, _ := nbt.Get[nbt.Int](two, "Value"); v != 7 {
		t.Errorf("saved Value = %d, want 7", v)
	}
}

// TestBossbarRefusesAnInvalidName checks that a name the component codec
// would not read is refused where it was typed, whole: one that is accepted
// reaches the client in the Boss Event and takes it down.
func TestBossbarRefusesAnInvalidName(t *testing.T) {
	t.Parallel()
	_, alice, bob := bossbarTwoPlayers(t)

	// Each refusal carries the codec's message, with the cursor at the component's start.
	// vanilla: CommandArgumentParser.withCodec
	const eitherNotString = "Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: "
	for name, shown := range map[string]string{
		`{text:"Bad",color:"nope"}`:                "Invalid color name: nope",
		`{text:"Bad",color:"#zz"}`:                 "Invalid color value: #zz",
		`{text:"Bad",color:"#1000000"}`:            "Color value out of range: #1000000",
		`{text:5}`:                                 eitherNotString + "{text:5}; Second: No matching codec found",
		`{text:"Bad",bold:"yes"}`:                  "Not a number",
		`{text:"Bad",extra:[]}`:                    "List must have contents",
		`{text:"Bad",font:"BAD FONT"}`:             "Not a valid resource location: BAD FONT Non [a-z0-9/._-] character in path of location: minecraft:BAD FONT",
		`{text:"Bad",shadow_color:"x"}`:            `Failed to parse either. First: Not a number; Second: Not a list: "x"`,
		`{text:"Bad",click_event:{action:"nope"}}`: "Unknown element name:nope",
		// Contents the model has no message for take the registry's.
		`{selector:"@bogus"}`: eitherNotString + `{selector:"@bogus"}; Second: No matching codec found`,
		`{nbt:"Health"}`:      eitherNotString + `{nbt:"Health"}; Second: No matching codec found`,
	} {
		input := "bossbar add test:bad " + name
		alice.ChatCommand(input)
		// The codec's diagnostic, with the cursor back at the component.
		// vanilla: CommandArgumentParser.withCodec
		alice.ExpectCommandError(
			servertest.CommandErrorMessageForTest("argument.component.invalid", shown),
			servertest.CommandErrorContextForTest(input, len(input)-len(name)),
		)
	}

	alice.ChatCommand(`bossbar add test:bad {text:5}`)
	alice.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.component.invalid",
			"Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {text:5}; Second: No matching codec found"),
		servertest.CommandErrorContextForTest("bossbar add test:bad {text:5}", len("bossbar add test:bad ")),
	)

	// tellraw reports the same diagnostic at its component.
	alice.ChatCommand(`tellraw @a {text:5}`)
	alice.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.component.invalid",
			"Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {text:5}; Second: No matching codec found"),
		servertest.CommandErrorContextForTest("tellraw @a {text:5}", len("tellraw @a ")),
	)

	// The other commands that read a component report the same at it.
	for _, c := range []struct{ input, prefix string }{
		{`title @s title {text:5}`, "title @s title "},
		{`scoreboard objectives add bad dummy {text:5}`, "scoreboard objectives add bad dummy "},
		{`datapack create bad {text:5}`, "datapack create bad "},
		{`title @s title {text:"Bad",color:"nope"}`, "title @s title "},
	} {
		want := eitherNotString + "{text:5}; Second: No matching codec found"
		if strings.Contains(c.input, "nope") {
			want = "Invalid color name: nope"
		}
		alice.ChatCommand(c.input)
		alice.ExpectCommandError(
			servertest.CommandErrorMessageForTest("argument.component.invalid", want),
			servertest.CommandErrorContextForTest(c.input, len(c.prefix)),
		)
	}

	// A store into a bar that exists is no refusal, one into a missing bar is.
	alice.ChatCommand(`bossbar add test:good "Fine"`)
	alice.ExpectPlay(protocol.IDPlaySystemChat)
	alice.ChatCommand("execute store result bossbar test:good value run bossbar set test:good value 3")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.value.success", protocol.Text{
		Text: "[", Extra: []protocol.Text{{Text: "Fine"}, {Text: "]"}},
		Color: "white", Insertion: "test:good", HoverText: &protocol.Text{Text: "test:good"},
	}, servertest.NumberText(3))
	alice.ChatCommand("execute store result bossbar test:none value run bossbar set test:good value 4")
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.unknown", "test:none"))

	// None of them made the bar.
	alice.ChatCommand(`bossbar add test:bad "Fine"`)
	alice.ExpectCommandFeedbackText("commands.bossbar.create.success", protocol.Text{
		Text: "[", Extra: []protocol.Text{{Text: "Fine"}, {Text: "]"}},
		Color: "white", Insertion: "test:bad", HoverText: &protocol.Text{Text: "test:bad"},
	})

	alice.Disconnect()
	bob.Disconnect()
}

// TestBossbarResolvesTheName checks that the name is resolved when the bar
// is made: a selector becomes the names it finds and a list becomes its
// first element with the rest as siblings.
func TestBossbarResolvesTheName(t *testing.T) {
	t.Parallel()
	_, alice, bob := bossbarTwoPlayers(t)

	alice.ChatCommand(`bossbar add test:sel {selector:"Bob"}`)
	alice.ExpectCommandFeedbackText("commands.bossbar.create.success", protocol.Text{
		Text: "[", Extra: []protocol.Text{
			{Text: "", Extra: []protocol.Text{servertest.PlayerDisplayComponent("Bob")}},
			{Text: "]"},
		},
		Color: "white", Insertion: "test:sel", HoverText: &protocol.Text{Text: "test:sel"},
	})
	alice.ChatCommand("bossbar set test:sel players Bob")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.players.success.some", protocol.Text{
		Text: "[", Extra: []protocol.Text{
			{Text: "", Extra: []protocol.Text{servertest.PlayerDisplayComponent("Bob")}},
			{Text: "]"},
		},
		Color: "white", Insertion: "test:sel", HoverText: &protocol.Text{Text: "test:sel"},
	}, servertest.NumberText(1), servertest.PlayerDisplayComponent("Bob"))
	checkBossBarAdd(t, bob.ExpectPlay(protocol.IDPlayBossEvent), "Bob", 0)

	alice.ChatCommand(`bossbar add test:list ["A","B"]`)
	alice.ExpectCommandFeedbackText("commands.bossbar.create.success", protocol.Text{
		Text: "[", Extra: []protocol.Text{
			{Text: "A", Extra: []protocol.Text{{Text: "B"}}},
			{Text: "]"},
		},
		Color: "white", Insertion: "test:list", HoverText: &protocol.Text{Text: "test:list"},
	})
	alice.ChatCommand("bossbar set test:list players Bob")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.players.success.some", protocol.Text{
		Text: "[", Extra: []protocol.Text{
			{Text: "A", Extra: []protocol.Text{{Text: "B"}}},
			{Text: "]"},
		},
		Color: "white", Insertion: "test:list", HoverText: &protocol.Text{Text: "test:list"},
	}, servertest.NumberText(1), servertest.PlayerDisplayComponent("Bob"))
	checkBossBarAdd(t, bob.ExpectPlay(protocol.IDPlayBossEvent), "AB", 0)

	alice.Disconnect()
	bob.Disconnect()
}

// TestBossbarResolvesAnNbtName resolves a stored value on both creation and rename.
// vanilla: BossBarCommands.createBar, BossBarCommands.setName
func TestBossbarResolvesAnNbtName(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))
	c.ChatCommand(`data modify storage t:s v set value "Hi"`)
	c.ExpectPlay(protocol.IDPlaySystemChat)
	c.ChatCommand(`bossbar add t:n {nbt:"v",storage:"t:s"}`)
	c.ExpectPlay(protocol.IDPlaySystemChat)
	c.ChatCommand(`bossbar set t:n players @s`)
	bar := checkBossBarAdd(t, c.ExpectPlay(protocol.IDPlayBossEvent), `"Hi"`, 0)
	c.ExpectPlay(protocol.IDPlaySystemChat)
	srv.RunLine(`data modify storage t:s v set value "Bye"`)
	c.Settle()
	c.ChatCommand(`bossbar set t:n name {nbt:"v",storage:"t:s"}`)
	expectBossBarName(t, c, bar, `"Bye"`)
}

// TestBossbarWildcardScoreUsesExecutor resolves * against the command runner.
// vanilla: BossBarCommands.createBar, BossBarCommands.setName
func TestBossbarWildcardScoreUsesExecutor(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalTestConfig(), "Actor", servertest.OpsListing(t, "Actor"))
	srv.RunLine(`scoreboard objectives add o dummy`)
	srv.RunLine(`scoreboard players set Actor o 42`)
	c.Settle()
	c.ChatCommand(`bossbar add t:n ["Score: ",{score:{name:"*",objective:"o"}}]`)
	c.ExpectPlay(protocol.IDPlaySystemChat)
	c.ChatCommand(`bossbar set t:n players @s`)
	bar := checkBossBarAdd(t, c.ExpectPlay(protocol.IDPlayBossEvent), "Score: 42", 0)
	c.ExpectPlay(protocol.IDPlaySystemChat)
	c.ChatCommand(`bossbar set t:n name ["Again: ",{score:{name:"*",objective:"o"}}]`)
	expectBossBarName(t, c, bar, "Again: 42")
}

// TestBossbarFeedbackColours checks the colour the bracketed name is drawn
// in for each bar colour a file can give.
// vanilla: BossEvent.BossBarColor
func TestBossbarFeedbackColours(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	want := map[string]string{
		"pink": "red", "blue": "blue", "red": "dark_red", "green": "green",
		"yellow": "yellow", "purple": "dark_blue", "white": "white",
	}
	bars := nbt.Compound{}
	for color := range want {
		bars["c:"+color] = nbt.Compound{"Name": nbt.String("N"), "Color": nbt.String(color)}
	}
	writeBossBarsFile(t, cfg.LevelName, bars)
	_, alice := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	for _, color := range []string{"pink", "blue", "red", "green", "yellow", "purple", "white"} {
		id := "c:" + color
		alice.ChatCommand("bossbar set " + id + " value 1")
		alice.ExpectCommandFeedbackText("commands.bossbar.set.value.success", protocol.Text{
			Text: "[", Extra: []protocol.Text{{Text: "N"}, {Text: "]"}},
			Color: want[color], Insertion: id, HoverText: &protocol.Text{Text: id},
		}, servertest.NumberText(1))
	}
	alice.Disconnect()
}

// TestBossbarFileWithAnInvalidNameIsSkipped checks that a saved bar whose
// name the codec would refuse is not loaded, where sending it would take the
// client down at every join.
func TestBossbarFileWithAnInvalidNameIsSkipped(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	bars := nbt.Compound{"b:bad": nbt.Compound{"Name": nbt.Compound{"text": nbt.String("x"), "color": nbt.String("nope")}}}
	writeBossBarsFile(t, cfg.LevelName, bars)
	_, alice := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	alice.ChatCommand("bossbar set b:bad value 1")
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.unknown", "b:bad"))
	alice.Disconnect()
}

// writeBossBarsFile writes the bars as the custom_boss_events.dat of the
// world under root.
func writeBossBarsFile(t *testing.T, root string, bars nbt.Compound) {
	t.Helper()
	encoded, err := nbt.Encode("", nbt.Compound{"data": bars, "DataVersion": nbt.Int(0)})
	if err != nil {
		t.Fatal(err)
	}
	var packed bytes.Buffer
	gz := gzip.NewWriter(&packed)
	if _, err := gz.Write(encoded); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(bossBarsPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bossBarsPath(root), packed.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
