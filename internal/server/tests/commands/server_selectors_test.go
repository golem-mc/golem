package commands

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
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

func TestExecuteEntitySelectorAcrossChunkBorder(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance = config.MinDistance
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	for _, line := range []string{
		`summon item 15.5 80 0 {Item:{id:"minecraft:stone",count:1},PickupDelay:32767s}`,
		`summon item 16.0625 80 0 {Item:{id:"minecraft:stone",count:1},PickupDelay:32767s,Tags:["edge"]}`,
		`summon cow 15.75 80 0 {NoAI:1b}`,
	} {
		if out := srv.RunLine(line); len(out) != 1 || out[0].Translate != "commands.summon.success" {
			t.Fatalf("%s: %v", line, out)
		}
		c.ExpectPlay(protocol.IDPlaySystemChat) // Console feedback is copied to operators.
	}

	for _, tc := range []struct {
		query string
		count int64
	}{
		{"17 80 0 if entity @e[type=item,distance=..2]", 2},
		{"14.95 80 0 if entity @e[type=item,dx=0,tag=edge,limit=1]", 1},
		{"16.0 80 0 if entity @e[type=cow,dx=0]", 1},
	} {
		c.Run(t, tc.query, func(t *testing.T) {
			c.ChatCommand("execute positioned " + tc.query)
			c.ExpectCommandFeedbackText("commands.execute.conditional.pass_count", protocol.Text{Number: &tc.count})
		})
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

func TestExecuteAsArmorStandByUUID(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	const id = "00000001-0000-0002-0000-000300000004"
	name := namedDisplay("minecraft:armor_stand", "Indexed", 4)
	c.ChatCommand(`summon armor_stand 3.5 64 0.5 {UUID:[I;1,2,3,4],CustomName:"Indexed",NoGravity:1b}`)
	servertest.AwaitCommandFeedbackText(t, c, "commands.summon.success", name)

	c.ChatCommand("execute as " + id + " run tag @s add indexed")
	servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single", protocol.Text{Text: "indexed"}, name)
	c.ChatCommand("kill " + id)
	servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single", name)
	c.ChatCommand("kill " + id)
	if got := c.ExpectSystemChatPastMobs(); !bytes.Equal(got, servertest.CommandErrorMessageForTest("argument.entity.notfound.entity")) {
		t.Errorf("kill after removal = % X, want no entity found", got)
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
