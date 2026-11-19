package commands

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The tests here put the target selectors through the commands that act on
// what they name: a cow standing in the world is what no selector had to
// find before, and what /kill and /tp have to reach now.

// TestKillSelectorKillsTheCowsItNames checks /kill @e[type=cow]: every cow
// the selector names dies and nothing else does, and the feedback counts
// what died the way the vanilla server counts it, as the number it is.
func TestKillSelectorKillsTheCowsItNames(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(12.5, 64, 8.5, true)
	c.Settle()

	srv.SpawnMob("minecraft:cow", 10.5, 64, 8.5)
	srv.SpawnMob("minecraft:cow", 14.5, 64, 8.5)
	srv.SpawnMob("minecraft:pig", 16.5, 64, 8.5)

	c.ChatCommand("kill @e[type=cow]")
	two := int64(2)
	servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.multiple", protocol.Text{Number: &two})

	// The dead stay in the world as corpses for the twenty ticks a death
	// leaves behind, so the count the check reads is the one after the
	// tick has taken them out.
	servertest.UntilTicks(t, srv, 100, "condition", func() bool { return srv.MobCount() == 1 })
	if got := srv.MobCount(); got != 1 {
		t.Errorf("mobs standing after kill @e[type=cow] = %d, want the pig alone", got)
	}

	c.Disconnect()
}

// TestKillSelectorDistanceKeepsTheFarCow checks the distance option: a
// selector that looks no further than five blocks kills the cow beside the
// runner and leaves the one across the field standing, the way the vanilla
// selector reads its range from where its runner is.
func TestKillSelectorDistanceKeepsTheFarCow(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	near := srv.SpawnMob("minecraft:cow", 2.5, 64, 0.5)
	srv.SpawnMob("minecraft:cow", 20.5, 64, 0.5)
	nearUUID, ok := srv.MobUUID(near)
	if !ok {
		t.Fatal("the near cow has no UUID to be named by")
	}

	c.ChatCommand("kill @e[type=!player,distance=..5]")
	// One cow in range: the single form, naming the one it killed by the
	// display name the vanilla server shows for one.
	servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", mustUUID(t, nearUUID)))

	servertest.UntilTicks(t, srv, 100, "condition", func() bool { return srv.MobCount() == 1 })
	if got := srv.MobCount(); got != 1 {
		t.Errorf("mobs standing after a distance-limited kill = %d, want the far cow alone", got)
	}

	c.Disconnect()
}

// TestTeleportSelectorMovesTheNearestCow checks the limit and sort options
// on a command that moves what it names: of two cows at different
// distances, the nearest one is the one /tp carries, to where the runner
// stands, and the feedback names the cow by the display name the vanilla
// server shows for one.
func TestTeleportSelectorMovesTheNearestCow(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(12.5, 64, 8.5, true)
	c.Settle()

	near := srv.SpawnMob("minecraft:cow", 10.5, 64, 8.5)
	far := srv.SpawnMob("minecraft:cow", 20.5, 64, 8.5)

	c.ChatCommand("tp @e[type=cow,limit=1,sort=nearest] ~ ~ ~")
	nearUUID, ok := srv.MobUUID(near)
	if !ok {
		t.Fatal("the near cow vanished before its teleport could be checked")
	}
	servertest.AwaitCommandFeedbackText(t, c, "commands.teleport.success.location.single",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", mustUUID(t, nearUUID)),
		protocol.Text{Text: "12.500000"}, protocol.Text{Text: "64.000000"}, protocol.Text{Text: "8.500000"})

	x, y, z, ok := srv.MobPos(near)
	if !ok || x != 12.5 || y != 64 || z != 8.5 {
		t.Errorf("nearest cow after tp = (%v, %v, %v), %v, want (12.5, 64, 8.5)", x, y, z, ok)
	}
	x, y, z, ok = srv.MobPos(far)
	if !ok || x != 20.5 || y != 64 || z != 8.5 {
		t.Errorf("far cow after the nearest was taken = (%v, %v, %v), %v, want (20.5, 64, 8.5)", x, y, z, ok)
	}

	c.Disconnect()
}

// TestGamemodeRefusesANonPlayerSelector checks the refusal a players-only
// command gives a selector that can name more than players: the vanilla
// argument's own message, with the context line the vanilla server points
// back at the start of the input with.
func TestGamemodeRefusesANonPlayerSelector(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("gamemode creative @e[type=cow]")
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.player.entities"),
		servertest.CommandErrorContextForTest("gamemode creative @e[type=cow]", 0))

	c.Disconnect()
}

// TestSelectorNamesTheNearestPlayerByName checks the name option on the
// nearest-player selector: of two players, the one whose name the option
// carries is the one /kill reaches, however near the other stands.
func TestSelectorNamesTheNearestPlayerByName(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// The selector the commands walk is a gamemaster's tool, and the
	// commands it rides on are theirs, so the runner is listed as an
	// operator.
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))

	alice := connect().Join("Alice")
	alice.MoveToGround(12.5, 64, 8.5, true)
	alice.Settle()

	bob := connect()
	bobID := bob.JoinAs("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.ChatCommand("kill @p[name=Bob]")
	servertest.AwaitCommandFeedbackText(t, alice, "commands.kill.success.single", servertest.PlayerDisplayComponent("Bob"))

	killMessage := protocol.Text{
		Translate: "death.attack.genericKill",
		With:      []protocol.Text{servertest.PlayerDisplayComponent("Bob")},
	}
	servertest.ExpectDeathSequence(t, bob, bobID, killMessage)

	alice.Disconnect()
	bob.Disconnect()
}

// TestSelectorRefusesAnUnknownOption checks the refusal a selector gives an
// option it does not know: the vanilla message naming the option, with the
// context line pointing back at the option's own word, where the vanilla
// parser rewinds to before it gives up.
func TestSelectorRefusesAnUnknownOption(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("kill @e[colour=red]")
	c.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.entity.options.unknown", "colour"),
		servertest.CommandErrorContextForTest("kill @e[colour=red]", 8))

	c.Disconnect()
}

// TestSelectorRangeErrorsPointAtTheValue checks where a range that does not
// hold together is refused: at the start of the range, past the option's
// name, which is where the vanilla server's context line was seen to point
// for "kill @e[level=5..1]" and "kill @e[distance=5..1]".
func TestSelectorRangeErrorsPointAtTheValue(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	for _, input := range []string{"kill @e[level=5..1]", "kill @e[distance=5..1]"} {
		c.ChatCommand(input)
		c.ExpectCommandError(
			servertest.CommandErrorMessageForTest("argument.range.swapped"),
			servertest.CommandErrorContextForTest(input, strings.Index(input, "=")+1))
	}

	c.Disconnect()
}

// TestKillSelectorTakesAUUID checks the bare-UUID target: the one entity
// whose UUID the command carries is the one it reaches, and the feedback
// names the cow by the translation the client renders it by.
func TestKillSelectorTakesAUUID(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	cow := srv.SpawnMob("minecraft:cow", 10.5, 64, 8.5)
	uuid, ok := srv.MobUUID(cow)
	if !ok {
		t.Fatal("the cow has no UUID to be named by")
	}

	c.ChatCommand("kill " + uuid)
	servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", mustUUID(t, uuid)))

	servertest.UntilTicks(t, srv, 100, "condition", func() bool { return srv.MobCount() == 0 })
	if got := srv.MobCount(); got != 0 {
		t.Errorf("mobs standing after a kill by UUID = %d, want none", got)
	}

	c.Disconnect()
}

// TestTheRandomPlayerSelectorPicksByTheSeededShuffle checks @r: the pick
// goes through the source's Random, which is the level dice the server's
// root seed seeded, so the one seed picks the one player. Four players
// stand in the world and the runner kills @r; the feedback names the one
// the shuffle landed on, which the same seed lands the same every run, and
// which is nobody the unshuffled first candidate would have been.
func TestTheRandomPlayerSelectorPicksByTheSeededShuffle(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Ruler", "Alice", "Bob", "Charlie"))

	ruler := connect().Join("Ruler")

	alice := connect()
	alice.JoinAs("Alice")

	bob := connect()
	bob.JoinAs("Bob")

	charlie := connect()
	charlie.JoinAs("Charlie")

	// The joins' announcements reach the runner late, with no order the
	// reads could lean on; the feedback read steps over whatever of them
	// is still in flight.
	ruler.ChatCommand("kill @r")
	servertest.AwaitCommandFeedbackText(t, ruler, "commands.kill.success.single",
		servertest.PlayerDisplayComponent("Charlie"))

	for _, c := range []*servertest.Client{ruler, alice, bob, charlie} {
		c.Disconnect()
	}
}

// TestSelectorSuggestsTheRegistrysTypes checks the type option's values:
// inside the brackets the client is offered the entity types the registry
// holds and the tags it groups them by, replacing from after the = they are
// typed behind, and a value that is chosen hands the word back with the
// comma or bracket that may follow it.
func TestSelectorSuggestsTheRegistrysTypes(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.CommandSuggestion(1, "/kill @e[type=cow")
	id, start, length, matches := c.ExpectCommandSuggestions()
	if id != 1 {
		t.Errorf("suggestion id = %d, want 1", id)
	}
	if start != 17 {
		t.Errorf("start = %d, want 17", start)
	}
	if length != 0 {
		t.Errorf("length = %d, want 0", length)
	}
	if want := []string{",", "]"}; !servertest.EqualStrings(matches, want) {
		t.Errorf("after a whole type = %v, want %v", matches, want)
	}

	c.CommandSuggestion(2, "/kill @e[type=")
	_, _, _, matches = c.ExpectCommandSuggestions()
	var hasTag, hasNegated, hasPlain bool
	for _, match := range matches {
		switch match {
		case "#minecraft:frog_food":
			hasTag = true
		case "!minecraft:cow", "minecraft:cow":
			if match == "!minecraft:cow" {
				hasNegated = true
			} else {
				hasPlain = true
			}
		}
	}
	if !hasTag || !hasNegated || !hasPlain {
		t.Errorf("type values = %v, want tags, negated types and plain types among them", matches)
	}

	c.Disconnect()
}

// TestSelectorSuggestionsReplaceFromTheCursor checks where the selector's
// suggestions say the replacement starts: the option keys from the cursor
// inside the brackets, behind the [ or the comma that came before them, and
// an option's values from after its =, so a Tab never swaps the whole
// selector for the key it offers. The keys and values on offer are the ones
// the fragment typed so far begins, and the comma and bracket that close an
// option off answer from the cursor too.
func TestSelectorSuggestionsReplaceFromTheCursor(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// The merged list rides in Brigadier's own order, sorted without regard
	// to case. Vanilla offers its scores and advancements beside these; the
	// scoreboard they are read against is what this server keeps none of.
	keys := []string{"advancements=", "distance=", "dx=", "dy=", "dz=", "gamemode=", "level=", "limit=", "name=",
		"nbt=", "predicate=", "scores=", "sort=", "tag=", "team=", "type=", "x=", "x_rotation=", "y=", "y_rotation=", "z="}

	c.CommandSuggestion(1, "/kill @e[")
	id, start, length, matches := c.ExpectCommandSuggestions()
	if id != 1 {
		t.Errorf("suggestion id = %d, want 1", id)
	}
	if start != 9 {
		t.Errorf("start = %d, want 9", start)
	}
	if length != 0 {
		t.Errorf("length = %d, want 0", length)
	}
	if !servertest.EqualStrings(matches, keys) {
		t.Errorf("option keys = %v, want %v", matches, keys)
	}

	c.CommandSuggestion(2, "/kill @e[name=foo,")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 18 {
		t.Errorf("start = %d, want 18", start)
	}
	if length != 0 {
		t.Errorf("length = %d, want 0", length)
	}
	if want := []string{"advancements=", "distance=", "dx=", "dy=", "dz=", "gamemode=", "level=", "limit=",
		"nbt=", "predicate=", "scores=", "sort=", "tag=", "team=", "type=", "x=", "x_rotation=", "y=", "y_rotation=", "z="}; !servertest.EqualStrings(matches, want) {
		t.Errorf("option keys after a used name = %v, want %v", matches, want)
	}

	c.CommandSuggestion(3, "/kill @e[type=")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 14 {
		t.Errorf("start = %d, want 14", start)
	}
	if length != 0 {
		t.Errorf("length = %d, want 0", length)
	}
	var hasTag, hasNegated, hasPlain, hasPrefixed bool
	for _, match := range matches {
		switch match {
		case "#minecraft:frog_food":
			hasTag = true
		case "!minecraft:cow":
			hasNegated = true
		case "minecraft:cow":
			hasPlain = true
		}
		if strings.HasPrefix(match, "@e[type=") {
			hasPrefixed = true
		}
	}
	if !hasTag || !hasNegated || !hasPlain {
		t.Errorf("type values = %v, want tags, negated types and plain types among them", matches)
	}
	if hasPrefixed {
		t.Errorf("type values carry the word typed so far: %v", matches)
	}

	// A typed key fragment narrows the keys to the ones it begins.
	c.CommandSuggestion(4, "/kill @e[t")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 9 {
		t.Errorf("start = %d, want 9", start)
	}
	if length != 1 {
		t.Errorf("length = %d, want 1", length)
	}
	if want := []string{"tag=", "team=", "type="}; !servertest.EqualStrings(matches, want) {
		t.Errorf("option keys behind t = %v, want %v", matches, want)
	}

	// A typed value fragment keeps the forms whose own shape matches it: a
	// plain fragment offers the plain names alone.
	c.CommandSuggestion(5, "/kill @e[type=c")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 14 {
		t.Errorf("start = %d, want 14", start)
	}
	if length != 1 {
		t.Errorf("length = %d, want 1", length)
	}
	if len(matches) != 31 {
		t.Errorf("type values behind c = %d matches, want 31", len(matches))
	}
	if !hasString(matches, "minecraft:cow") {
		t.Errorf("type values behind c = %v, want minecraft:cow among them", matches)
	}
	for _, match := range matches {
		if match[0] == '!' || match[0] == '#' {
			t.Errorf("type values behind c carry a mark: %v", matches)
			break
		}
	}

	// A negated fragment shares its ! with both the negated entity types and
	// the negated tags, so both are offered, each carrying its own marks.
	c.CommandSuggestion(6, "/kill @e[type=!c")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 14 {
		t.Errorf("start = %d, want 14", start)
	}
	if length != 2 {
		t.Errorf("length = %d, want 2", length)
	}
	if len(matches) != 44 {
		t.Errorf("type values behind !c = %d matches, want 44", len(matches))
	}
	if !hasString(matches, "!minecraft:cow") {
		t.Errorf("type values behind !c = %v, want !minecraft:cow among them", matches)
	}
	if !hasString(matches, "!#minecraft:can_breathe_under_water") {
		t.Errorf("type values behind !c = %v, want the negated can_* tag among them", matches)
	}
	for _, match := range matches {
		if strings.HasPrefix(match, "!!") {
			t.Errorf("type values behind !c double the mark: %v", matches)
			break
		}
	}

	// A fragment carrying the namespace keeps the names it begins whole.
	c.CommandSuggestion(7, "/kill @e[type=minecraft:c")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 14 {
		t.Errorf("start = %d, want 14", start)
	}
	if length != 11 {
		t.Errorf("length = %d, want 11", length)
	}
	if len(matches) != 15 {
		t.Errorf("type values behind minecraft:c = %d matches, want 15", len(matches))
	}
	if !hasString(matches, "minecraft:cow") {
		t.Errorf("type values behind minecraft:c = %v, want minecraft:cow among them", matches)
	}

	// A fragment behind a comma narrows the keys the same way.
	c.CommandSuggestion(8, "/kill @e[type=cow,l")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 18 {
		t.Errorf("start = %d, want 18", start)
	}
	if length != 1 {
		t.Errorf("length = %d, want 1", length)
	}
	if want := []string{"level=", "limit="}; !servertest.EqualStrings(matches, want) {
		t.Errorf("option keys behind l = %v, want %v", matches, want)
	}

	// An option whose value is still empty hands back the comma and bracket
	// from where its value would begin, not the whole word.
	c.CommandSuggestion(9, "/kill @e[name=")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 14 {
		t.Errorf("start = %d, want 14", start)
	}
	if length != 0 {
		t.Errorf("length = %d, want 0", length)
	}
	if want := []string{",", "]"}; !servertest.EqualStrings(matches, want) {
		t.Errorf("after an empty name value = %v, want %v", matches, want)
	}

	// A tag value is read whole wherever it stops, so the parser moves past
	// it and offers the comma and bracket even while the tag is still being
	// spelled out.
	c.CommandSuggestion(10, "/kill @e[type=#minecraft:ar")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 27 {
		t.Errorf("start = %d, want 27", start)
	}
	if length != 0 {
		t.Errorf("length = %d, want 0", length)
	}
	if want := []string{",", "]"}; !servertest.EqualStrings(matches, want) {
		t.Errorf("behind a half-typed tag = %v, want %v", matches, want)
	}

	// The same ranges hold for a selector a nested command reads, where the
	// word does not begin the input.
	c.CommandSuggestion(11, "/execute run kill @e[type=c")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 26 {
		t.Errorf("start = %d, want 26", start)
	}
	if length != 1 {
		t.Errorf("length = %d, want 1", length)
	}
	if len(matches) != 31 {
		t.Errorf("nested type values behind c = %d matches, want 31", len(matches))
	}
	if !hasString(matches, "minecraft:cow") {
		t.Errorf("nested type values behind c = %v, want minecraft:cow among them", matches)
	}

	// A bare # reads as the empty tag id, which parses, so the parser has
	// moved past the value and offers the comma and bracket from there.
	c.CommandSuggestion(12, "/kill @e[type=#")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 15 {
		t.Errorf("start = %d, want 15", start)
	}
	if length != 0 {
		t.Errorf("length = %d, want 0", length)
	}
	if want := []string{",", "]"}; !servertest.EqualStrings(matches, want) {
		t.Errorf("behind a bare # = %v, want %v", matches, want)
	}

	c.CommandSuggestion(13, "/kill @e[type=#minecraft:arrows,type=#")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 38 {
		t.Errorf("start = %d, want 38", start)
	}
	if length != 0 {
		t.Errorf("length = %d, want 0", length)
	}
	if want := []string{",", "]"}; !servertest.EqualStrings(matches, want) {
		t.Errorf("behind a bare # after a tag = %v, want %v", matches, want)
	}

	// A tag already read is no longer offered, in either polarity, since a
	// second read of it is refused.
	c.CommandSuggestion(14, "/kill @e[type=#minecraft:arrows,type=")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 37 {
		t.Errorf("start = %d, want 37", start)
	}
	if length != 0 {
		t.Errorf("length = %d, want 0", length)
	}
	if hasString(matches, "#minecraft:arrows") || hasString(matches, "!#minecraft:arrows") {
		t.Errorf("a used tag is still offered: %v", matches)
	}
	if !hasString(matches, "#minecraft:aquatic") || !hasString(matches, "!minecraft:cow") {
		t.Errorf("type values behind a used tag = %v, want the other tags and types among them", matches)
	}

	c.Disconnect()
}

// TestKillSelectorWithSpacesInItsBracketsKillsOne checks a selector whose
// options are parted by spaces: the whole selector reaches the command, so a
// limit of one kills one of the two pigs and leaves the other.
func TestKillSelectorWithSpacesInItsBracketsKillsOne(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(12.5, 64, 8.5, true)
	c.Settle()

	srv.SpawnMob("minecraft:pig", 10.5, 64, 8.5)
	srv.SpawnMob("minecraft:pig", 14.5, 64, 8.5)

	c.ChatCommand("kill @e[type=pig, limit=1]")
	servertest.UntilTicks(t, srv, 100, "condition", func() bool { return srv.MobCount() == 1 })
	if got := srv.MobCount(); got != 1 {
		t.Errorf("pigs standing after kill @e[type=pig, limit=1] = %d, want one", got)
	}

	c.Disconnect()
}

// TestSelectorSuggestsThroughWhitespace checks the selector suggestions
// hold across the whitespace inside the brackets: an option's value, the
// comma and bracket that close it, and the next option's key all answer
// across the spaces.
// vanilla: EntitySelectorParser.parseOptions
func TestSelectorSuggestsThroughWhitespace(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	keys := []string{"advancements=", "distance=", "dx=", "dy=", "dz=", "gamemode=", "level=", "limit=", "name=",
		"nbt=", "predicate=", "scores=", "sort=", "tag=", "team=", "type=", "x=", "x_rotation=", "y=", "y_rotation=", "z="}

	// The whitespace behind the = is skipped and the type values answer from
	// where the value begins.
	c.CommandSuggestion(1, "/kill @e[type= c")
	_, start, length, matches := c.ExpectCommandSuggestions()
	if start != 15 {
		t.Errorf("start = %d, want 15", start)
	}
	if length != 1 {
		t.Errorf("length = %d, want 1", length)
	}
	if len(matches) != 31 || !hasString(matches, "minecraft:cow") {
		t.Errorf("type values behind a space = %v, want the 31 names behind c", matches)
	}

	// A value read whole leaves the comma and bracket offered at the cursor.
	c.CommandSuggestion(2, "/kill @e[type=cow ")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 18 {
		t.Errorf("start = %d, want 18", start)
	}
	if length != 0 {
		t.Errorf("length = %d, want 0", length)
	}
	if want := []string{",", "]"}; !servertest.EqualStrings(matches, want) {
		t.Errorf("after a whole type and a space = %v, want %v", matches, want)
	}

	// The whitespace behind the bracket is skipped before the option's key.
	c.CommandSuggestion(3, "/kill @e[ type=c")
	_, start, _, matches = c.ExpectCommandSuggestions()
	if start != 15 {
		t.Errorf("start = %d, want 15", start)
	}
	if len(matches) != 31 || !hasString(matches, "minecraft:cow") {
		t.Errorf("type values behind a space after [ = %v, want the 31 names behind c", matches)
	}

	// After a comma, a space, the option keys are offered from the cursor,
	// with the type already used left out.
	c.CommandSuggestion(4, "/kill @e[type=pig, ")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 19 {
		t.Errorf("start = %d, want 19", start)
	}
	if length != 0 {
		t.Errorf("length = %d, want 0", length)
	}
	if want := keysWithout(keys, "type="); !servertest.EqualStrings(matches, want) {
		t.Errorf("keys after a comma and a space = %v, want %v", matches, want)
	}

	// The same holds under /tp, where the target selector is one of several
	// argument branches: the selector's own words are read as one argument.
	c.CommandSuggestion(5, "/tp @e[ ")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 8 {
		t.Errorf("start = %d, want 8", start)
	}
	if length != 0 {
		t.Errorf("length = %d, want 0", length)
	}
	if !servertest.EqualStrings(matches, keys) {
		t.Errorf("keys behind /tp @e[ = %v, want %v", matches, keys)
	}

	c.CommandSuggestion(6, "/tp @e[type=cow, l")
	_, start, length, matches = c.ExpectCommandSuggestions()
	if start != 17 {
		t.Errorf("start = %d, want 17", start)
	}
	if length != 1 {
		t.Errorf("length = %d, want 1", length)
	}
	if want := []string{"level=", "limit="}; !servertest.EqualStrings(matches, want) {
		t.Errorf("keys behind /tp @e[type=cow, l = %v, want %v", matches, want)
	}

	c.Disconnect()
}

// TestKillSelectorNBTTakesTheNoAICowAlone checks the nbt option: of two cows
// the one whose saved compound carries NoAI is the one /kill takes, and the
// one without the flag stands on. The compound the option matches against is
// the entity's saved one, so a NoAI that a summon's tag or /data put there is
// what the option reads.
func TestKillSelectorNBTTakesTheNoAICowAlone(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(12.5, 64, 8.5, true)
	c.Settle()

	asleep := srv.SpawnMob("minecraft:cow", 10.5, 64, 8.5)
	if !srv.SetMobNoAI(asleep) {
		t.Fatal("the sleeping cow could not be flagged")
	}
	srv.SpawnMob("minecraft:cow", 16.5, 64, 8.5)
	asleepUUID, ok := srv.MobUUID(asleep)
	if !ok {
		t.Fatal("the sleeping cow has no UUID to be named by")
	}

	c.ChatCommand("kill @e[type=minecraft:cow,nbt={NoAI:1b}]")
	servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", mustUUID(t, asleepUUID)))

	servertest.UntilTicks(t, srv, 100, "condition", func() bool { return srv.MobCount() == 1 })
	if got := srv.MobCount(); got != 1 {
		t.Errorf("cows standing after kill @e[nbt={NoAI:1b}] = %d, want the unflagged one alone", got)
	}

	c.Disconnect()
}

// TestKillSelectorNBTInvertedTakesTheCowWithoutTheFlag checks the nbt
// option's !: of the same two cows the inverted compound takes the one whose
// saved compound carries no NoAI, and the flagged one is refused by nothing.
func TestKillSelectorNBTInvertedTakesTheCowWithoutTheFlag(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(12.5, 64, 8.5, true)
	c.Settle()

	asleep := srv.SpawnMob("minecraft:cow", 10.5, 64, 8.5)
	if !srv.SetMobNoAI(asleep) {
		t.Fatal("the sleeping cow could not be flagged")
	}
	plain := srv.SpawnMob("minecraft:cow", 16.5, 64, 8.5)
	plainUUID, ok := srv.MobUUID(plain)
	if !ok {
		t.Fatal("the plain cow has no UUID to be named by")
	}

	c.ChatCommand("kill @e[type=minecraft:cow,nbt=!{NoAI:1b}]")
	servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", mustUUID(t, plainUUID)))

	servertest.UntilTicks(t, srv, 100, "condition", func() bool { return srv.MobCount() == 1 })
	if got := srv.MobCount(); got != 1 {
		t.Errorf("cows standing after kill @e[nbt=!{NoAI:1b}] = %d, want the sleeping one alone", got)
	}

	c.Disconnect()
}

// TestSelectorEmptyNBTMatchesEverything checks the nbt option's empty
// compound: it demands nothing of the entities it names, so the runner alone
// in the world is what it answers with.
// vanilla: EntitySelectorOptions' nbt option, an empty compound as subset
func TestSelectorEmptyNBTMatchesEverything(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("execute if entity @e[nbt={}]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the empty compound answered % X, want the runner alone counted", got)
	}

	c.Disconnect()
}

// TestSelectorNBTKeepsEachTagType checks the nbt option compares a tag by
// its type and not its value: a cow's Health is a float, so the compound that
// spells it one matches and the one that spells the same number an int
// matches nothing. The trailing comma the vanilla grammar allows is carried
// on the matching value.
// vanilla: NbtUtils.compareNbt, SnbtGrammar's trailing comma
func TestSelectorNBTKeepsEachTagType(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	asleep := srv.SpawnMob("minecraft:cow", 10.5, 64, 8.5)
	if !srv.SetMobNoAI(asleep) {
		t.Fatal("the sleeping cow could not be flagged")
	}

	c.ChatCommand("execute if entity @e[type=minecraft:cow,nbt={Health:10f}]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the float Health answered % X, want the cow counted", got)
	}

	c.ChatCommand("execute if entity @e[type=minecraft:cow,nbt={Health:10}]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the int Health answered % X, want the conditional failure", got)
	}

	c.ChatCommand("execute if entity @e[type=minecraft:cow,nbt={Health:10f,}]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the trailing comma answered % X, want the compound still read", got)
	}

	c.Disconnect()
}

// TestSelectorNBTExcludesTheEntityID checks the compound the nbt option
// compares: it is the saveWithoutId form, so the "id" an entity file carries
// is not among the tags a cow answers to, and a pattern naming it matches
// nothing.
// vanilla: NbtPredicate.getEntityTagToCompare, Entity.saveWithoutId
func TestSelectorNBTExcludesTheEntityID(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	srv.SpawnMob("minecraft:cow", 10.5, 64, 8.5)

	c.ChatCommand(`execute if entity @e[type=minecraft:cow,nbt={id:"minecraft:cow"}]`)
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the id pattern answered % X, want the conditional failure", got)
	}

	c.Disconnect()
}

// TestSelectorNBTReadsThePlayersHeldItem checks the player's side of the
// compound: saveWithoutId carries SelectedItem, the held stack, so a player
// holding stone answers a pattern naming the stone and not one naming
// another item.
// vanilla: NbtPredicate.getEntityTagToCompare
func TestSelectorNBTReadsThePlayersHeldItem(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the registry has no stone")
	}
	srv.GiveSlot("Testificate", player.HotbarSlot, protocol.ItemStack{ItemID: stone, Count: 1})

	c.ChatCommand(`execute if entity @s[nbt={SelectedItem:{id:"minecraft:stone"}}]`)
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the held stone answered % X, want the player counted", got)
	}

	c.ChatCommand(`execute if entity @s[nbt={SelectedItem:{id:"minecraft:dirt"}}]`)
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the other item answered % X, want the conditional failure", got)
	}

	c.Disconnect()
}

// TestSelectorNBTMatchesKindsWithNoMobForm checks the nbt option against the
// kinds whose saved form is not a mob's: an armor stand and a thrown
// snowball both answer the empty compound, and both answer an inverted
// pattern they do not carry, which their own saveWithoutId form alone makes
// possible.
// vanilla: ArmorStand.addAdditionalSaveData, ThrowableItemProjectile.addAdditionalSaveData
func TestSelectorNBTMatchesKindsWithNoMobForm(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// The stand goes to the corner of the chunks the client's render distance
	// holds: inside the held square it stays loaded, and past the round
	// tracking range no spawn or equipment notice reaches the client to stand
	// in front of the command answers.
	c.ChatCommand("summon minecraft:armor_stand 72.5 64 72.5")
	c.ExpectSystemChatPastMobs() // the summon's own feedback

	c.ChatCommand("execute if entity @e[type=minecraft:armor_stand,nbt={}]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the empty compound against the stand answered % X, want it counted", got)
	}

	c.ChatCommand("execute if entity @e[type=minecraft:armor_stand,nbt=!{NoAI:1b}]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the inverted pattern against the stand answered % X, want it counted", got)
	}

	if ball := srv.ThrowThrown(entity.ThrownSnowball, 10.5, 100, 8.5, 0, 0, 0, 7); ball == 0 {
		t.Fatal("the snowball never flew")
	}

	c.ChatCommand("execute if entity @e[type=minecraft:snowball,nbt={}]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the empty compound against the snowball answered % X, want it counted", got)
	}

	c.ChatCommand("execute if entity @e[type=minecraft:snowball,nbt=!{NoAI:1b}]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the inverted pattern against the snowball answered % X, want it counted", got)
	}

	c.Disconnect()
}

// TestSelectorNBTMatchesPartialLists checks the list half of the comparison:
// an expected list matches when every element of it appears in the tag's own,
// and an empty expected list only matches an empty list. A cow's Pos carries
// the 10.5 it was spawned at, so a one-element list finds it and an empty
// list finds nothing.
// vanilla: NbtUtils.compareNbt, partialListMatches
func TestSelectorNBTMatchesPartialLists(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	srv.SpawnMob("minecraft:cow", 10.5, 64, 8.5)

	c.ChatCommand("execute if entity @e[type=minecraft:cow,nbt={Pos:[10.5d]}]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the partial list answered % X, want the cow counted", got)
	}

	c.ChatCommand("execute if entity @e[type=minecraft:cow,nbt={Pos:[]}]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the empty list against a full one answered % X, want the conditional failure", got)
	}

	c.Disconnect()
}

// TestSelectorNBTParseErrors checks the refusals the two new options make,
// with the key and the cursor each answer carries: the nbt rows were read off
// a vanilla server over the wire, answer for answer, and the predicate
// rows follow the identifier and valueless conventions the other options
// already read.
func TestSelectorNBTParseErrors(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	for _, tc := range []struct {
		name    string
		command string
		key     string
		with    []string
		cursor  int
	}{
		{"a value that is no compound", "kill @e[nbt=5]", "argument.nbt.expected.compound", nil,
			strings.Index("kill @e[nbt=5]", "]")},
		{"an inverted value that is no compound", "kill @e[nbt=!5]", "argument.nbt.expected.compound", nil,
			strings.Index("kill @e[nbt=!5]", "]")},
		{"a compound entry without its colon", "kill @e[nbt={a}]", "argument.literal.incorrect", []string{":"},
			strings.Index("kill @e[nbt={a}]", "}")},
		{"a compound that spells no value", "kill @e[nbt=]", "snbt.parser.expected_unquoted_string", nil,
			strings.Index("kill @e[nbt=]", "]")},
		{"a compound the option list runs on from", "kill @e[nbt={a:1}limit=1]", "argument.entity.options.unterminated", nil,
			strings.Index("kill @e[nbt={a:1}limit=1]", "limit")},
		{"a predicate with no value", "kill @e[predicate]", "argument.entity.options.valueless", []string{"predicate"},
			strings.Index("kill @e[predicate]", "predicate")},
		{"a predicate id that parses nowhere", "kill @e[predicate=a:b:c]", "argument.id.invalid", nil,
			strings.Index("kill @e[predicate=a:b:c]", "a:b:c")},
		// An uppercase letter is no identifier character, so the id reads
		// empty and the option list refuses the character it left behind.
		{"a predicate whose id reads empty", "kill @e[predicate=Foo]", "argument.entity.options.unterminated", nil,
			strings.Index("kill @e[predicate=Foo]", "Foo")},
	} {
		c.ChatCommand(tc.command)
		c.ExpectCommandError(
			servertest.CommandErrorMessageForTest(tc.key, tc.with...),
			servertest.CommandErrorContextForTest(tc.command, tc.cursor))
	}

	c.Disconnect()
}

// TestSelectorPredicateMatchesNothing checks the predicate option against a
// standing cow: this server keeps no predicates, so every id answers to
// none — and an id that answers to none matches nothing, in either
// polarity. The kills find nobody, and the cow stands through them.
func TestSelectorPredicateMatchesNothing(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	asleep := srv.SpawnMob("minecraft:cow", 10.5, 64, 8.5)
	if !srv.SetMobNoAI(asleep) {
		t.Fatal("the sleeping cow could not be flagged")
	}

	c.ChatCommand("execute if entity @e[type=minecraft:cow,predicate=whatever:test]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the unknown predicate answered % X, want the conditional failure", got)
	}

	c.ChatCommand("execute if entity @e[type=minecraft:cow,predicate=!whatever:test]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the inverted unknown predicate answered % X, want the conditional failure", got)
	}

	// An empty id is no id this server registers, but it parses all the
	// same and matches nothing. vanilla: Identifier.read
	c.ChatCommand("execute if entity @e[type=minecraft:cow,predicate=]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")) {
		t.Errorf("the empty predicate id answered % X, want the conditional failure", got)
	}

	c.ChatCommand("kill @e[type=minecraft:cow,predicate=a,predicate=b]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, servertest.CommandErrorMessageForTest("argument.entity.notfound.entity")) {
		t.Errorf("the repeated predicates answered % X, want nobody found", got)
	}

	c.ChatCommand("execute if entity @e[type=minecraft:cow]")
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, conditionalPassCountForTest(1)) {
		t.Errorf("the cow after the predicates answered % X, want it still standing", got)
	}

	c.Disconnect()
}

// keysWithout is the key list with the named ones left out, which is the set
// a selector that has already used them may still offer.
func keysWithout(keys []string, drop ...string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		if hasString(drop, key) {
			continue
		}
		out = append(out, key)
	}
	return out
}

// hasString reports whether the list carries the one string.
func hasString(list []string, want string) bool {
	return slices.Contains(list, want)
}

// mustUUID reads the UUID a mob's identity travels as, which the display
// component of the tests hangs its hover on.
func mustUUID(t *testing.T, text string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(text)
	if err != nil {
		t.Fatalf("a mob's UUID %q does not parse: %v", text, err)
	}
	return id
}

// TestSelectorQuotedNameKeepsItsRunOfSpaces checks a selector parted into
// several words by a quoted value with two spaces in it: it reaches /kill
// (splitTargets) and /execute as one text, so only the cow named "a  b" dies
// and not the one named "a b".
func TestSelectorQuotedNameKeepsItsRunOfSpaces(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(12.5, 64, 8.5, true)
	c.Settle()

	c.ChatCommand(`summon minecraft:cow 10.5 64 8.5 {CustomName:"a  b"}`)
	c.ChatCommand(`summon minecraft:cow 14.5 64 8.5 {CustomName:"a b"}`)
	c.ChatCommand(`summon minecraft:cow 12.5 64 4.5 {CustomName:"a  b"}`)
	servertest.UntilTicks(t, srv, 100, "three cows", func() bool { return srv.MobCount() == 3 })

	c.ChatCommand(`kill @e[type=cow, name="a  b", limit=1]`)
	servertest.UntilTicks(t, srv, 100, "one dead", func() bool { return srv.MobCount() == 2 })

	c.ChatCommand(`execute as @e[name="a  b"] run kill @s`)
	servertest.UntilTicks(t, srv, 100, "execute kill", func() bool { return srv.MobCount() == 1 })
	if got := srv.MobCount(); got != 1 {
		t.Errorf("cows standing = %d, want only the one named \"a b\"", got)
	}

	c.Disconnect()
}

// TestDataPathQuotedKeyKeepsItsRunOfSpaces checks a /data path whose quoted
// key holds two spaces: the path is read as typed, so it names the key with
// two spaces and not the one with one.
func TestDataPathQuotedKeyKeepsItsRunOfSpaces(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.Settle()

	c.ChatCommand(`data merge storage test:q {"a  b":5,"a b":9}`)
	c.ExpectCommandFeedback("commands.data.storage.modified", "test:q")
	c.ChatCommand(`data get storage test:q "a  b"`)
	c.ExpectCommandFeedback("commands.data.storage.query", "test:q", "5")

	c.Disconnect()
}

// TestSelectorNegativeDeltaBoxEndsAtTheAnchorBlock checks that a negative dx
// spans the delta to one block past the anchor, not to the anchor itself.
func TestSelectorNegativeDeltaBoxEndsAtTheAnchorBlock(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(12.5, 64, 8.5, true)
	c.Settle()

	// Box 9.2..13.2 holds x=12.5.
	c.ChatCommand("tag @a[x=12.2,y=64,z=8.5,dx=-3] add neg")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
		protocol.Text{Text: "neg"}, servertest.PlayerDisplayComponent("Testificate"))

	// Box 5.0..9.0 does not.
	c.ChatCommand("tag @a[x=8.0,y=64,z=8.5,dx=-3] add neg2")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("argument.entity.notfound.entity"))

	c.Disconnect()
}

// TestSelectorNameMatchesWithoutRegardToCase checks a bare player name in a
// target argument finds the player whatever the case it is typed in.
func TestSelectorNameMatchesWithoutRegardToCase(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.Settle()

	c.ChatCommand("tag testificate add lower")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
		protocol.Text{Text: "lower"}, servertest.PlayerDisplayComponent("Testificate"))

	c.Disconnect()
}

func TestSelectorRotationRanges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		selector string
		yaw      float32
		matches  bool
	}{
		{"yaw wraps", "@a[y_rotation=170..-170]", 180, true},
		{"pitch open upper", "@a[x_rotation=10..]", 180, false},
		{"pitch open lower", "@a[x_rotation=..10]", 180, true},
		{"pitch wraps", "@a[x_rotation=10..-10]", 180, false},
		{"yaw open upper", "@a[y_rotation=10..]", 0, false},
		{"float endpoints", "@a[y_rotation=179.999999..179.999999]", 180, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, c := servertest.Join(t, servertest.MobTestConfig(), "Testificate", servertest.OpsListing(t, "Testificate"))
			c.SendRotation(tc.yaw, 0, true)
			c.ChatCommand("tag " + tc.selector + " add rot")
			if tc.matches {
				servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
					protocol.Text{Text: "rot"}, servertest.PlayerDisplayComponent("Testificate"))
			} else {
				c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("argument.entity.notfound.entity"))
			}
			c.Disconnect()
		})
	}
}

// TestSelectorsFindEndCrystals checks that a selector reaches an end crystal:
// /data merge changes it and /kill removes it without an explosion.
func TestSelectorsFindEndCrystals(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))
	servertest.HoldTicks(t, srv)

	c.ChatCommand("summon minecraft:end_crystal 5.5 64 5.5 {Tags:[\"core\"]}")
	spawn := c.AwaitSpawn(protocol.EntityTypeEndCrystal)

	c.ChatCommand("data merge entity @e[type=minecraft:end_crystal,tag=core,limit=1] {ShowBottom:0b}")
	show := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexEndCrystalShowBottom)
	if show == nil || show.FlagVal {
		t.Fatalf("ShowBottom metadata = %+v, want false", show)
	}

	c.ChatCommand("kill @e[type=minecraft:end_crystal]")
	servertest.AwaitSystemChatSaying(t, c, "commands.kill.success.single")
	if n := srv.EndCrystalCount(); n != 0 {
		t.Fatalf("end crystals after /kill = %d, want 0", n)
	}
}

// TestTagSelectorCountsTheTaggedBodies checks the tag option across a
// player, a fixture, a mob and an untagged item: a named tag, its inversion,
// the bare tag= that asks for no tags, its inversion and two options at once.
func TestTagSelectorCountsTheTaggedBodies(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(12.5, 64, 8.5, true)
	c.Settle()

	c.ChatCommand("scoreboard objectives add x dummy")
	c.ChatCommand(`summon marker ~ ~ ~ {Tags:["a"]}`)
	c.ChatCommand(`summon cow ~-2 ~ ~ {Tags:["a","b"]}`)
	// Five blocks off and never picked up, so it stays the untagged body.
	c.ChatCommand(`summon item ~5 ~ ~ {Item:{id:"minecraft:stone",count:1},PickupDelay:32767s}`)
	c.ChatCommand("tag @s add a")
	cases := []struct {
		holder, selector string
		want             int32
	}{
		{"#a", "@e[tag=a]", 3},
		{"#b", "@e[tag=!a]", 1},
		{"#c", "@e[tag=]", 1},
		{"#d", "@e[tag=!]", 3},
		{"#e", "@e[tag=a,tag=!b]", 2},
	}
	for _, tc := range cases {
		c.ChatCommand("execute store result score " + tc.holder + " x if entity " + tc.selector)
	}
	servertest.UntilTicks(t, srv, 100, "every count stored", func() bool {
		_, ok := srv.ScoreOf("#e", "x")
		return ok
	})
	for _, tc := range cases {
		if got, ok := srv.ScoreOf(tc.holder, "x"); !ok || got != tc.want {
			t.Errorf("%s counted %d (stored %t), want %d", tc.selector, got, ok, tc.want)
		}
	}

	c.Disconnect()
}
