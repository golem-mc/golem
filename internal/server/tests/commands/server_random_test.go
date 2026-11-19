package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// The /random command, black-box: the replies, the broadcasts and the
// gates a vanilla server gives the same words. The messages and the
// tree were captured from a vanilla reference server's console and its
// saved random_sequences.dat; the named draws it makes are held against
// the random source port in internal/server/world fed the world's seed, which is
// what vanilla seeds its own by. vanilla: RandomCommand

// readFeedback reads one System Chat message a command's reply is sent as,
// failing unless the whole packet read back and it stayed off the action
// bar.
func readFeedback(t *testing.T, data []byte) protocol.Text {
	t.Helper()
	r := protocol.NewReader(data)
	text, ok := protocol.ReadText(r)
	if !ok || r.Err() != nil {
		t.Fatalf("feedback = % X, which is no text component", data)
	}
	if overlay := r.ReadBool(); overlay {
		t.Fatalf("feedback = % X, sent to the action bar", data)
	}
	return text
}

// expectSample reads the reply /random value sends back and hands its
// drawn value over.
func expectSample(t *testing.T, c *servertest.Client) int64 {
	t.Helper()
	text := readFeedback(t, c.ExpectPlay(protocol.IDPlaySystemChat))
	if text.Translate != "commands.random.sample.success" || len(text.With) != 1 || text.With[0].Number == nil {
		t.Fatalf("sample reply = %+v, want commands.random.sample.success with the value", text)
	}
	return *text.With[0].Number
}

// expectBareFailure reads the one red line a command's own refusal arrives
// as, without a context line.
func expectBareFailure(t *testing.T, c *servertest.Client, key string) {
	t.Helper()
	want := servertest.CommandErrorMessageForTest(key)
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, want) {
		t.Errorf("failure = % X, want % X", got, want)
	}
}

// expectResetReply reads the reply a /random reset names its sequence with.
func expectResetReply(t *testing.T, c *servertest.Client, id string) {
	t.Helper()
	text := readFeedback(t, c.ExpectPlay(protocol.IDPlaySystemChat))
	if text.Translate != "commands.random.reset.success" ||
		len(text.With) != 1 || text.With[0].Text != id {
		t.Errorf("reset reply = %+v, want commands.random.reset.success with %s", text, id)
	}
}

// TestRandomValueDrawsFromTheWorldsDice checks the form of /random every
// player may run: a bare /random value answers the drawn number as the
// vanilla sample message. vanilla: RandomCommand.randomSample
func TestRandomValueDrawsFromTheWorldsDice(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, connect := servertest.Start(t, cfg)

	draw := func(name string, command string, lo, hi int64) {
		t.Run(name, func(t *testing.T) {
			c := connect().Join("Testificate")
			defer c.Disconnect()

			c.ChatCommand(command)
			v := expectSample(t, c)
			if v < lo || v > hi {
				t.Errorf("%s drew %d, want one in [%d, %d]", command, v, lo, hi)
			}
		})
	}
	draw("both ends", "random value 1..2", 1, 2)
	draw("negative numbers", "random value -3..-1", -3, -1)
	// An open end keeps its bound: a range missing its maximum draws from
	// everything above the minimum.
	draw("open maximum", "random value 5..", 5, 2147483647)
	draw("open minimum under the cap", "random value -3..2147483643", -3, 2147483643)
}

// TestRandomRefusesDegenerateRanges checks the ranges the vanilla sampler
// refuses before it draws: a range of one value, and a span at or past the
// int cap. The refusals are the command's own, without a parse context.
func TestRandomRefusesDegenerateRanges(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, connect := servertest.Start(t, cfg)

	refuse := func(name string, command string, key string) {
		t.Run(name, func(t *testing.T) {
			c := connect().Join("Testificate")
			defer c.Disconnect()

			c.ChatCommand(command)
			expectBareFailure(t, c, key)
		})
	}
	// A range holding one value, written either way, is too small.
	refuse("a lone number", "random value 5", "commands.random.error.range_too_small")
	// The span — the largest value less the smallest — must stay under the
	// int cap, which an open minimum or a full-width range runs past.
	refuse("the span at the cap", "random value 0..2147483647", "commands.random.error.range_too_large")
}

// TestRandomRangeParseErrors checks the typed errors the range argument's
// own reading refuses a word with: nothing readable at all, a minimum above
// the maximum, and the tail the reading stopped at.
// vanilla: MinMaxBounds.fromReader
func TestRandomRangeParseErrors(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	for _, tc := range []struct {
		name    string
		command string
		key     string
		cursor  int
	}{
		{"a word that names no number", "random value a..b", "argument.range.empty",
			strings.Index("random value a..b", "a..b")},
		{"no ends at all", "random value ..", "argument.range.empty",
			strings.Index("random value ..", "..")},
		{"a minimum above the maximum", "random value 6..1", "argument.range.swapped",
			strings.Index("random value 6..1", "6..1")},
		{"a tail the reading stops at", "random value 1..x", "command.expected.separator",
			strings.Index("random value 1..x", "x")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c.ChatCommand(tc.command)
			c.ExpectCommandError(
				servertest.CommandErrorMessageForTest(tc.key),
				servertest.CommandErrorContextForTest(tc.command, tc.cursor))
		})
	}
	c.Disconnect()
}

// TestRandomSequences checks the value and roll overloads that name a
// sequence: the draws a named sequence makes match the port of vanilla's
// random source fed the world's seed, a reset restarts the same stream, the
// unqualified name names the minecraft one, and a rejected range still
// leaves the sequence behind — vanilla fetches before it reads the range.
func TestRandomSequences(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	// The world's seed — which is what vanilla seeds a named sequence's
	// stream from — read off the /seed feedback rather than assumed.
	c.ChatCommand("seed")
	seedText := readFeedback(t, c.ExpectPlay(protocol.IDPlaySystemChat))
	seed := servertest.SeedFromFeedback(t, seedText)
	sequence := world.NewRandomSequence(seed, "minecraft:kdice")
	const lo, hi = int32(0), int32(1 << 24)
	const span = "0..16777216"

	first := sequence.NextIntBetween(lo, hi)
	c.ChatCommand("random value " + span + " minecraft:kdice")
	if got := expectSample(t, c); got != int64(first) {
		t.Errorf("the first named draw was %d, want %d", got, first)
	}
	second := sequence.NextIntBetween(lo, hi)
	c.ChatCommand("random value " + span + " minecraft:kdice")
	if got := expectSample(t, c); got != int64(second) {
		t.Errorf("the second named draw was %d, want %d", got, second)
	}

	// A roll from the same sequence draws the same stream on.
	third := sequence.NextIntBetween(lo, hi)
	c.ChatCommand("random roll " + span + " minecraft:kdice")
	expectRollBroadcast(t, c, servertest.PlayerDisplayComponent("Alice"), int64(third), int64(lo), int64(hi))

	// The unqualified name is the minecraft one: the same stream draws on.
	fourth := sequence.NextIntBetween(lo, hi)
	c.ChatCommand("random value " + span + " kdice")
	if got := expectSample(t, c); got != int64(fourth) {
		t.Errorf("the unqualified draw was %d, want %d", got, fourth)
	}

	// A reset with a salt restarts the stream from the seed and the salt
	// the reset named, which is what a new sequence would have been seeded
	// with, and the reply names the sequence back.
	restarted := world.NewRandomSequence(seed^42, "minecraft:kdice")
	afterReset := restarted.NextIntBetween(lo, hi)
	c.ChatCommand("random reset minecraft:kdice 42")
	expectResetReply(t, c, "minecraft:kdice")
	c.ChatCommand("random value " + span + " minecraft:kdice")
	if got := expectSample(t, c); got != int64(afterReset) {
		t.Errorf("the draw after the reset was %d, want %d", got, afterReset)
	}

	// A rejected range fetches the sequence first — vanilla's own order —
	// so the sequence exists although nothing was drawn from it.
	c.ChatCommand("random value 1..1 minecraft:halfformed")
	expectBareFailure(t, c, "commands.random.error.range_too_small")

	// The star reset clears every sequence, and its answer counts them.
	c.ChatCommand("random reset *")
	text := readFeedback(t, c.ExpectPlay(protocol.IDPlaySystemChat))
	if text.Translate != "commands.random.reset.all.success" ||
		len(text.With) != 1 || text.With[0].Number == nil || *text.With[0].Number != 2 {
		t.Errorf("reset-all reply = %+v, want commands.random.reset.all.success with 2", text)
	}

	// A reset of a sequence that never existed is the same answer as one
	// that did: a fresh stream under the name.
	c.ChatCommand("random reset minecraft:neverdrawn")
	expectResetReply(t, c, "minecraft:neverdrawn")
	c.ChatCommand("random reset *")
	text = readFeedback(t, c.ExpectPlay(protocol.IDPlaySystemChat))
	if text.Translate != "commands.random.reset.all.success" || *text.With[0].Number != 1 {
		t.Errorf("second reset-all reply = %+v, want commands.random.reset.all.success with 1", text)
	}
	c.Disconnect()
}

// TestRandomRollBroadcastsToEveryPlayer checks the roll form, which vanilla
// does not answer with: it tells every player what the runner threw, over
// the system chat, the runner among them. A non-operator may throw the bare
// form — it is not gated — and the range the vanilla checker refuses
// answers the refusal before any throw is announced.
func TestRandomRollBroadcastsToEveryPlayer(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))

	alice := connect().Join("Alice")
	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	// The sequence the roll names is created on the spot, so its first
	// draw is the one the seeded port makes.
	alice.ChatCommand("seed")
	seedText := readFeedback(t, alice.ExpectPlay(protocol.IDPlaySystemChat))
	seed := servertest.SeedFromFeedback(t, seedText)
	first := world.NewRandomSequence(seed, "minecraft:poker").NextIntBetween(1, 6)

	alice.ChatCommand("random roll 1..6 minecraft:poker")
	expectRollBroadcast(t, alice, servertest.PlayerDisplayComponent("Alice"), int64(first), 1, 6)
	expectRollBroadcast(t, bob, servertest.PlayerDisplayComponent("Alice"), int64(first), 1, 6)

	// The refusal answers the runner alone, before anything is announced.
	bob.ChatCommand("random roll 20..20")
	expectBareFailure(t, bob, "commands.random.error.range_too_small")

	// A bare roll is every player's to throw, on the world's own dice, and
	// the broadcast names the thrower the same way.
	bob.ChatCommand("random roll 1..6")
	expectRollBroadcast(t, bob, servertest.PlayerDisplayComponent("Bob"), 0, 1, 6)
	expectRollBroadcast(t, alice, servertest.PlayerDisplayComponent("Bob"), 0, 1, 6)

	alice.Disconnect()
	bob.Disconnect()
}

// expectRollBroadcast reads the line a /random roll sends every player and
// checks it is the thrown value between the range's ends. wantValue zero
// leaves the value to be checked against the bounds alone, for the draws
// that come off the world's own dice.
func expectRollBroadcast(t *testing.T, c *servertest.Client, thrower protocol.Text, wantValue, lo, hi int64) {
	t.Helper()
	text := readFeedback(t, c.ExpectPlay(protocol.IDPlaySystemChat))
	if text.Translate != "commands.random.roll" || len(text.With) != 4 {
		t.Fatalf("roll broadcast = %+v, want commands.random.roll with thrower and bounds", text)
	}
	if !textMatches(text.With[0], thrower) {
		t.Errorf("roll broadcast names %s, want %s", renderText(text.With[0]), renderText(thrower))
	}
	if text.With[1].Number == nil {
		t.Fatalf("roll broadcast value = %+v, want a whole number", text.With[1])
	}
	v := *text.With[1].Number
	if wantValue != 0 {
		if v != wantValue {
			t.Errorf("roll broadcast value = %d, want %d", v, wantValue)
		}
	} else if v < lo || v > hi {
		t.Errorf("roll broadcast value = %d, want one in [%d, %d]", v, lo, hi)
	}
	if !textMatches(text.With[2], servertest.NumberText(lo)) || !textMatches(text.With[3], servertest.NumberText(hi)) {
		t.Errorf("roll broadcast bounds = %s..%s, want %d..%d",
			renderText(text.With[2]), renderText(text.With[3]), lo, hi)
	}
}

// textMatches holds two components against each other by the wire form they
// encode to, pointees included.
func textMatches(a, b protocol.Text) bool {
	var wa, wb protocol.Buffer
	wa.PutText(a)
	wb.PutText(b)
	return bytes.Equal(wa.Bytes(), wb.Bytes())
}

// renderText is the text of a component as a failure names it by.
func renderText(t protocol.Text) string {
	return t.Plain()
}

// TestRandomGatesItsSequencesBehindOps checks who the command tree offers
// /random to: the bare draws are every player's, while the sequence and
// reset forms are an operator's, so a player without the level meets
// vanilla's unknown-argument refusal where the gated nodes would stand.
func TestRandomGatesItsSequencesBehindOps(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := servertest.Join(t, cfg, "Testificate")

	// The bare draw is not gated.
	c.ChatCommand("random value 1..2")
	if v := expectSample(t, c); v != 1 && v != 2 {
		t.Errorf("the bare draw was %d, want the one the die threw", v)
	}

	// A gated node is not in the server-built tree, so the walk stops at
	// the word that would have named it — vanilla's unknown-argument
	// refusal, with the context line pointing there.
	c.ChatCommand("random reset *")
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("command.unknown.argument"),
		servertest.CommandErrorContextForTest("random reset *", strings.Index("random reset *", "reset")))
	c.ChatCommand("random value 1..2 minecraft:sneaky")
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("command.unknown.argument"),
		servertest.CommandErrorContextForTest("random value 1..2 minecraft:sneaky",
			strings.Index("random value 1..2 minecraft:sneaky", "minecraft:sneaky")))
	c.Disconnect()
}
