package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/google/uuid"
)

// bossBarDisplay is how feedback calls a bar with this name, id and colour.
// vanilla: CustomBossEvent.getDisplayName
func bossBarDisplay(name, id, color string) protocol.Text {
	return protocol.Text{
		Text: "[", Extra: []protocol.Text{{Text: name}, {Text: "]"}},
		Color: color, Insertion: id, HoverText: &protocol.Text{Text: id},
	}
}

// expectBossBarName reads the Boss Event that renames a bar.
func expectBossBarName(t *testing.T, c *servertest.Client, bar uuid.UUID, name string) {
	t.Helper()
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayBossEvent))
	if got := r.ReadUUID(); got != bar {
		t.Errorf("boss event names bar %s, want %s", got, bar)
	}
	if action := r.ReadVarInt(); action != protocol.BossEventName {
		t.Errorf("boss event action = %d, want name (%d)", action, protocol.BossEventName)
	}
	text, ok := protocol.ReadText(r)
	if !ok || text.Plain() != name {
		t.Errorf("boss event name = %q (%v), want %q", text.Plain(), ok, name)
	}
	if r.Err() != nil || r.Remaining() != 0 {
		t.Errorf("boss event name: err %v, %d bytes left", r.Err(), r.Remaining())
	}
}

// expectBossBarStyle reads the Boss Event that restyles a bar.
func expectBossBarStyle(t *testing.T, c *servertest.Client, bar uuid.UUID, color, overlay int32) {
	t.Helper()
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayBossEvent))
	if got := r.ReadUUID(); got != bar {
		t.Errorf("boss event names bar %s, want %s", got, bar)
	}
	if action := r.ReadVarInt(); action != protocol.BossEventStyle {
		t.Errorf("boss event action = %d, want style (%d)", action, protocol.BossEventStyle)
	}
	if got := r.ReadVarInt(); got != color {
		t.Errorf("boss event colour = %d, want %d", got, color)
	}
	if got := r.ReadVarInt(); got != overlay {
		t.Errorf("boss event overlay = %d, want %d", got, overlay)
	}
	if r.Err() != nil || r.Remaining() != 0 {
		t.Errorf("boss event style: err %v, %d bytes left", r.Err(), r.Remaining())
	}
}

// bossbarWithBob makes test:bar and puts Bob on it, answering the bar's wire
// identity.
func bossbarWithBob(t *testing.T, alice, bob *servertest.Client) uuid.UUID {
	t.Helper()
	alice.ChatCommand(`bossbar add test:bar "The Test Bar"`)
	alice.ExpectCommandFeedbackText("commands.bossbar.create.success", testBarDisplay)
	alice.ChatCommand("bossbar set test:bar players Bob")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.players.success.some",
		testBarDisplay, servertest.NumberText(1), servertest.PlayerDisplayComponent("Bob"))
	return expectBossBarAdd(t, bob, "The Test Bar", 0)
}

// TestBossbarRemove checks that removing a bar takes it off the players
// who see it, that it is gone afterwards, and that a hidden bar sends nothing.
// vanilla: BossBarCommands.removeBar
func TestBossbarRemove(t *testing.T) {
	t.Parallel()
	srv, alice, bob := bossbarTwoPlayers(t)
	bar := bossbarWithBob(t, alice, bob)

	alice.ChatCommand("bossbar remove test:bar")
	expectBossBarRemove(t, bob, bar)
	alice.ExpectCommandFeedbackText("commands.bossbar.remove.success", testBarDisplay)

	alice.ChatCommand("bossbar remove test:bar")
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.unknown", "test:bar"))
	alice.ChatCommand("bossbar get test:bar value")
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.unknown", "test:bar"))

	// A hidden bar is not on anyone's screen, so there is nothing to remove.
	bar = bossbarWithBob(t, alice, bob)
	alice.ChatCommand("bossbar set test:bar visible false")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.visible.success.hidden", testBarDisplay)
	expectBossBarRemove(t, bob, bar)
	alice.ChatCommand("bossbar remove test:bar")
	alice.ExpectCommandFeedbackText("commands.bossbar.remove.success", testBarDisplay)
	expectNoBossEvent(t, srv, bob)

	alice.Disconnect()
	bob.Disconnect()
}

// TestBossbarList checks the list of bars, whose feedback is not echoed.
// vanilla: BossBarCommands.listBars
func TestBossbarList(t *testing.T) {
	t.Parallel()
	_, alice, bob := bossbarTwoPlayers(t)

	alice.ChatCommand("bossbar list")
	alice.ExpectCommandFeedback("commands.bossbar.list.bars.none")

	alice.ChatCommand(`bossbar add a:one "One"`)
	alice.ExpectCommandFeedbackText("commands.bossbar.create.success", bossBarDisplay("One", "a:one", "white"))
	alice.ChatCommand("bossbar list")
	alice.ExpectCommandFeedbackText("commands.bossbar.list.bars.some", servertest.NumberText(1), bossBarDisplay("One", "a:one", "white"))

	alice.ChatCommand(`bossbar add b:two "Two"`)
	alice.ExpectCommandFeedbackText("commands.bossbar.create.success", bossBarDisplay("Two", "b:two", "white"))
	alice.ChatCommand("bossbar list")
	alice.ExpectCommandFeedbackText("commands.bossbar.list.bars.some", servertest.NumberText(2), protocol.Text{
		Text: "", Extra: []protocol.Text{
			bossBarDisplay("One", "a:one", "white"),
			{Text: ", ", Color: protocol.ColorGray},
			bossBarDisplay("Two", "b:two", "white"),
		},
	})

	alice.Disconnect()
	bob.Disconnect()
}

// TestBossbarGet checks each of the four reads.
// vanilla: BossBarCommands.getValue, getMax, getVisible, getPlayers
func TestBossbarGet(t *testing.T) {
	t.Parallel()
	_, alice, bob := bossbarTwoPlayers(t)

	alice.ChatCommand(`bossbar add test:bar "The Test Bar"`)
	alice.ExpectCommandFeedbackText("commands.bossbar.create.success", testBarDisplay)

	alice.ChatCommand("bossbar get test:bar value")
	alice.ExpectCommandFeedbackText("commands.bossbar.get.value", testBarDisplay, servertest.NumberText(0))
	alice.ChatCommand("bossbar get test:bar max")
	alice.ExpectCommandFeedbackText("commands.bossbar.get.max", testBarDisplay, servertest.NumberText(100))
	alice.ChatCommand("bossbar get test:bar visible")
	alice.ExpectCommandFeedbackText("commands.bossbar.get.visible.visible", testBarDisplay)
	alice.ChatCommand("bossbar get test:bar players")
	alice.ExpectCommandFeedbackText("commands.bossbar.get.players.none", testBarDisplay)

	alice.ChatCommand("bossbar set test:bar players Bob")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.players.success.some",
		testBarDisplay, servertest.NumberText(1), servertest.PlayerDisplayComponent("Bob"))
	expectBossBarAdd(t, bob, "The Test Bar", 0)
	alice.ChatCommand("bossbar get test:bar players")
	alice.ExpectCommandFeedbackText("commands.bossbar.get.players.some",
		testBarDisplay, servertest.NumberText(1), servertest.PlayerDisplayComponent("Bob"))

	alice.ChatCommand("bossbar set test:bar visible false")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.visible.success.hidden", testBarDisplay)
	_ = bob.ExpectPlay(protocol.IDPlayBossEvent)
	alice.ChatCommand("bossbar get test:bar visible")
	alice.ExpectCommandFeedbackText("commands.bossbar.get.visible.hidden", testBarDisplay)

	alice.ChatCommand("bossbar get nosuch max")
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.unknown", "minecraft:nosuch"))

	alice.Disconnect()
	bob.Disconnect()
}

// TestBossbarSetNameColorStyleMax checks the four setters: each tells the
// players who see the bar, and a repeat is refused with its own message.
// vanilla: BossBarCommands.setName, setColor, setStyle, setMax
func TestBossbarSetNameColorStyleMax(t *testing.T) {
	t.Parallel()
	srv, alice, bob := bossbarTwoPlayers(t)
	bar := bossbarWithBob(t, alice, bob)

	alice.ChatCommand(`bossbar set test:bar name "Renamed"`)
	alice.ExpectCommandFeedbackText("commands.bossbar.set.name.success", bossBarDisplay("Renamed", "test:bar", "white"))
	expectBossBarName(t, bob, bar, "Renamed")
	alice.ChatCommand(`bossbar set test:bar name "Renamed"`)
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.set.name.unchanged"))

	alice.ChatCommand(`bossbar set test:bar name {text:"Renamed"}`)
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.set.name.unchanged"))

	alice.ChatCommand("bossbar set test:bar color red")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.color.success", bossBarDisplay("Renamed", "test:bar", "dark_red"))
	expectBossBarStyle(t, bob, bar, 2, 0)
	alice.ChatCommand("bossbar set test:bar color red")
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.set.color.unchanged"))

	alice.ChatCommand("bossbar set test:bar style notched_10")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.style.success", bossBarDisplay("Renamed", "test:bar", "dark_red"))
	expectBossBarStyle(t, bob, bar, 2, 2)
	alice.ChatCommand("bossbar set test:bar style notched_10")
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.set.style.unchanged"))

	alice.ChatCommand("bossbar set test:bar value 50")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.value.success", bossBarDisplay("Renamed", "test:bar", "dark_red"), servertest.NumberText(50))
	expectBossBarProgress(t, bob, bar, 0.5)
	alice.ChatCommand("bossbar set test:bar max 200")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.max.success", bossBarDisplay("Renamed", "test:bar", "dark_red"), servertest.NumberText(200))
	expectBossBarProgress(t, bob, bar, 0.25)
	alice.ChatCommand("bossbar set test:bar max 200")
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.bossbar.set.max.unchanged"))

	// The maximum starts at 1, as the argument says.
	input := "bossbar set test:bar max 0"
	alice.ChatCommand(input)
	alice.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.integer.low", "1", "0"),
		servertest.CommandErrorContextForTest(input, len(input)-1),
	)

	// A hidden bar is restyled without telling anyone.
	alice.ChatCommand("bossbar set test:bar visible false")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.visible.success.hidden", bossBarDisplay("Renamed", "test:bar", "dark_red"))
	expectBossBarRemove(t, bob, bar)
	alice.ChatCommand("bossbar set test:bar color blue")
	alice.ExpectCommandFeedbackText("commands.bossbar.set.color.success", bossBarDisplay("Renamed", "test:bar", "blue"))
	expectNoBossEvent(t, srv, bob)

	alice.Disconnect()
	bob.Disconnect()
}

// TestBossbarResultValues checks the value /execute store reads off each form.
// vanilla: BossBarCommands
func TestBossbarResultValues(t *testing.T) {
	t.Parallel()
	_, alice, bob := bossbarTwoPlayers(t)
	bar := bossbarWithBob(t, alice, bob)

	stored := func(command string, want string, feedbackKey string, with ...protocol.Text) {
		t.Helper()
		alice.ChatCommand("execute store result storage test:r x int 1 run " + command)
		alice.ExpectCommandFeedbackText(feedbackKey, with...)
		alice.ChatCommand("data get storage test:r x")
		alice.ExpectCommandFeedback("commands.data.storage.query", "test:r", want)
	}
	stored("bossbar list", "1", "commands.bossbar.list.bars.some", servertest.NumberText(1), testBarDisplay)
	stored("bossbar get test:bar max", "100", "commands.bossbar.get.max", testBarDisplay, servertest.NumberText(100))
	stored("bossbar get test:bar visible", "1", "commands.bossbar.get.visible.visible", testBarDisplay)
	stored("bossbar get test:bar players", "1", "commands.bossbar.get.players.some",
		testBarDisplay, servertest.NumberText(1), servertest.PlayerDisplayComponent("Bob"))
	stored("bossbar set test:bar max 40", "40", "commands.bossbar.set.max.success", testBarDisplay, servertest.NumberText(40))

	// A setter with no value of its own stores 0.
	stored("bossbar set test:bar color pink", "0", "commands.bossbar.set.color.success",
		bossBarDisplay("The Test Bar", "test:bar", "red"))
	expectBossBarStyle(t, bob, bar, 0, 0)

	stored("bossbar remove test:bar", "0", "commands.bossbar.remove.success",
		bossBarDisplay("The Test Bar", "test:bar", "red"))
	expectBossBarRemove(t, bob, bar)

	alice.Disconnect()
	bob.Disconnect()
}
