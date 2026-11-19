package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// macroFeedback is the chat a say line inside a function broadcasts.
func macroFeedback(c *servertest.Client, message string) {
	c.T.Helper()
	servertest.ExpectDisguisedChat(c.T, c, "minecraft:say_command", "Testificate", message, "")
}

// macroInstantiationFailure builds the refusal a function that cannot be
// instantiated fails with: the id, then the translated reason.
func macroInstantiationFailure(id string, reason protocol.Text) []byte {
	return servertest.CommandErrorMessageTextForTest(
		"commands.function.instantiationFailure", protocol.Text{Text: id}, reason)
}

// TestFunctionMacroRunsTheSubstitutedLines checks a macro function under
// /function: its plain lines run in order around the substituted ones,
// each type of argument at its own text, a spaced compound read whole, a
// plain function ignoring the arguments it was handed, the cache
// answering the repeated call, a /return from a substituted line, and
// the with clause reading a block entity's whole tag.
// vanilla: FunctionCommand.register (with sources),
// MacroFunction.instantiate, MacroFunction.stringify
func TestFunctionMacroRunsTheSubstitutedLines(t *testing.T) {
	t.Parallel()
	c := functionWorld(t, map[string]string{
		"t/function/echo.mcfunction":     "say first\n$say a:$(a) b:$(b)\nsay last\n",
		"t/function/plain.mcfunction":    "say plain one\n",
		"t/function/quote.mcfunction":    "$say \"$(s)\" end\n",
		"t/function/retmacro.mcfunction": "$data merge storage t:o {v:$(x)}\nreturn 5\n",
		"t/function/types.mcfunction":    "$say by:$(by) sh:$(sh) l:$(l) f:$(f) s:$(s) c:$(c)\n",
		"t/function/arrays.mcfunction":   "$say i:$(i) b:$(b) l:$(l) e:$(e) u:$(u)\n",
		"t/function/atblock.mcfunction":  "$say at:$(x) of:$(id)\n",
		"t/tags/function/all.json":       `{"values":["t:echo","t:retmacro"]}`,
	})

	c.ChatCommand("data merge storage t:in {a:0.5,b:-2.25,x:hello}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "t:in")

	// The with clause reads the storage's whole compound.
	c.ChatCommand("function t:echo with storage t:in")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:echo")
	macroFeedback(c, "first")
	macroFeedback(c, "a:.5 b:-2.25")
	macroFeedback(c, "last")
	// The repeated call answers from the instantiation cache.
	c.ChatCommand("function t:echo with storage t:in")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:echo")
	macroFeedback(c, "first")
	macroFeedback(c, "a:.5 b:-2.25")
	macroFeedback(c, "last")

	// A compound written behind the name may carry its spaces.
	c.ChatCommand("function t:echo {a:1, b:-2}")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:echo")
	macroFeedback(c, "first")
	macroFeedback(c, "a:1 b:-2")
	macroFeedback(c, "last")

	// A plain function runs untouched by the arguments it was handed.
	c.ChatCommand("function t:plain with storage t:in")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:plain")
	macroFeedback(c, "plain one")

	// A string's own text substitutes, quoted by the line around it.
	c.ChatCommand("data merge storage t:q {s:\"a b\"}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "t:q")
	c.ChatCommand("function t:quote with storage t:q")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:quote")
	macroFeedback(c, "\"a b\" end")

	// Each type at its own text: the integrals without their suffixes,
	// the decimals at its own digits, a string raw, a compound as SNBT.
	c.ChatCommand("function t:types {by:3b,sh:-5s,l:9223372036854775807L,f:2.675d,s:hello,c:{x:1}}")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:types")
	macroFeedback(c, "by:3 sh:-5 l:9223372036854775807 f:2.675 s:hello c:{x:1}")

	// The compound's own arrays read, an empty typed one among them: the
	// writer's spellings, byte elements with their B.
	c.ChatCommand("function t:arrays {i:[I;1,2],b:[B;7b],l:[L;4],e:[I;],u:[3]}")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:arrays")
	macroFeedback(c, "i:[I;1,2] b:[B;7B] l:[L;4L] e:[I;] u:[3]")

	// A return from a substituted line ends the function; its own line
	// writes what the value read, behind an execute store as well.
	c.ChatCommand("execute store result storage t:r r int 1 run function t:retmacro with storage t:in")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:retmacro")
	expectFunctionResult(c, "t:retmacro", 5)
	c.ChatCommand("data get storage t:o v")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:o", "\"hello\"")
	c.ChatCommand("data get storage t:r r")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:r", "5")

	// A tag's macro members announce together, then run in order.
	c.ChatCommand("function #t:all with storage t:in")
	with := expectScoreboardFeedback(t, c, "commands.function.scheduled.multiple")
	if len(with) != 1 || len(with[0].Extra) != 3 ||
		with[0].Extra[0].Text != "t:echo" || with[0].Extra[2].Text != "t:retmacro" {
		t.Errorf("/function #t:all announced %+v, want t:echo and t:retmacro", with)
	}
	macroFeedback(c, "first")
	macroFeedback(c, "a:.5 b:-2.25")
	macroFeedback(c, "last")
	expectFunctionResult(c, "t:retmacro", 5)

	// The with clause reads a block entity's whole tag as well: the kind's
	// own keys, its place an int and its id a string.
	c.ChatCommand("setblock 1 63 1 minecraft:chest")
	c.ExpectCommandFeedback("commands.setblock.success", "1", "63", "1")
	c.ChatCommand("function t:atblock with block 1 63 1")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:atblock")
	macroFeedback(c, "at:1 of:minecraft:chest")
	c.Disconnect()
}

// TestFunctionMacroRefusesWhatVanillaRefuses checks the refusals of a
// macro call: a source that keeps nothing or a path that reads no
// compound fails before the functions are announced, and a call that
// misses its arguments, or one of them, or parses a substituted command
// into the unknown, fails once they are.
// vanilla: FunctionCustomExecutor.arguments, getArgumentTag,
// MacroFunction.instantiate
func TestFunctionMacroRefusesWhatVanillaRefuses(t *testing.T) {
	t.Parallel()
	c := functionWorld(t, map[string]string{
		"t/function/echo.mcfunction":  "$say a:$(a) b:$(b)\n",
		"t/function/parse.mcfunction": "$e_$(v)\n",
	})

	c.ChatCommand("data merge storage t:in {a:0.5}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "t:in")

	// A path that reads no compound refuses before the announce.
	c.ChatCommand("function t:echo with storage t:in a")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.function.error.argument_not_compound", "DOUBLE"))
	// A path that reads nothing refuses likewise.
	c.ChatCommand("function t:echo with storage t:in nope")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("arguments.nbtpath.nothing_found", "nope"))
	// A block that holds no position refuses before the announce too.
	c.ChatCommand("function t:echo with block 1000 80 1000")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("argument.pos.unloaded"))
	// A block entity's own tag reads fine but names none of the macro's
	// arguments, so the call announces and refuses at the instantiation.
	c.ChatCommand("setblock 1 63 1 minecraft:chest")
	c.ExpectCommandFeedback("commands.setblock.success", "1", "63", "1")
	c.ChatCommand("function t:echo with block 1 63 1")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:echo")
	c.ExpectCommandFailure(macroInstantiationFailure("t:echo", protocol.Text{
		Translate: "commands.function.error.missing_argument",
		With:      []protocol.Text{{Text: "t:echo"}, {Text: "a"}}}))

	// Without arguments the call refuses having named none, once the
	// function was announced.
	c.ChatCommand("function t:echo")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:echo")
	c.ExpectCommandFailure(macroInstantiationFailure("t:echo", protocol.Text{
		Translate: "commands.function.error.missing_arguments", With: []protocol.Text{{Text: "t:echo"}}}))
	// A source that keeps nothing is the compound that misses every name,
	// the first of them in order.
	c.ChatCommand("function t:echo with storage t:neverwritten")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:echo")
	c.ExpectCommandFailure(macroInstantiationFailure("t:echo", protocol.Text{
		Translate: "commands.function.error.missing_argument",
		With:      []protocol.Text{{Text: "t:echo"}, {Text: "a"}}}))
	// One kept name leaves the next missing.
	c.ChatCommand("function t:echo with storage t:in")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:echo")
	c.ExpectCommandFailure(macroInstantiationFailure("t:echo", protocol.Text{
		Translate: "commands.function.error.missing_argument",
		With:      []protocol.Text{{Text: "t:echo"}, {Text: "b"}}}))

	// The with clause reads an entity's own tag over its paths.
	c.ChatCommand("summon minecraft:cow 2.5 64 2.5")
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.ExpectCommandFeedbackText("commands.summon.success",
		servertest.EntityDisplayComponent("minecraft:cow", "entity.minecraft.cow", cow.Uuid))
	c.ChatCommand("function t:echo with entity @e[type=minecraft:cow,limit=1] Health")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.function.error.argument_not_compound", "FLOAT"))
	c.ChatCommand("function t:echo with entity @e[type=minecraft:cow,limit=1]")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:echo")
	c.ExpectCommandFailure(macroInstantiationFailure("t:echo", protocol.Text{
		Translate: "commands.function.error.missing_argument",
		With:      []protocol.Text{{Text: "t:echo"}, {Text: "a"}}}))

	// A line that does not parse names the function, the command and the
	// read that failed.
	c.ChatCommand("data merge storage t:p {v:x}")
	c.ExpectCommandFeedback("commands.data.storage.modified", "t:p")
	c.ChatCommand("function t:parse with storage t:p")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:parse")
	c.ExpectCommandFailure(macroInstantiationFailure("t:parse", protocol.Text{
		Translate: "commands.function.error.parse", With: []protocol.Text{
			{Text: "t:parse"}, {Text: "e_x"}, {Text: "Unknown or incomplete command. See below for error at position 0: <--[HERE]"}}}))
	c.Disconnect()
}

// TestFunctionTagRunsWhatQueuedBeforeTheRefusal checks a tag with a macro
// member that cannot substitute: the tag is announced whole, the member
// that cannot be refuses naming itself, and the members whose calls queued
// before it still run.
// vanilla: FunctionCommand.instantiateAndQueueFunctions
func TestFunctionTagRunsWhatQueuedBeforeTheRefusal(t *testing.T) {
	t.Parallel()
	c := functionWorld(t, map[string]string{
		"t/function/lead.mcfunction":        "say lead ran\n",
		"t/function/broken.mcfunction":      "$e_$(v)\n",
		"t/tags/function/broken_after.json": `{"values":["t:lead","t:broken"]}`,
	})

	c.ChatCommand("function #t:broken_after {v:x}")
	with := expectScoreboardFeedback(t, c, "commands.function.scheduled.multiple")
	if len(with) != 1 || len(with[0].Extra) != 3 ||
		with[0].Extra[0].Text != "t:lead" || with[0].Extra[2].Text != "t:broken" {
		t.Errorf("/function #t:broken_after announced %+v, want t:lead and t:broken", with)
	}
	c.ExpectCommandFailure(macroInstantiationFailure("t:broken", protocol.Text{
		Translate: "commands.function.error.parse", With: []protocol.Text{
			{Text: "t:broken"}, {Text: "e_x"}, {Text: "Unknown or incomplete command. See below for error at position 0: <--[HERE]"}}}))
	// The plain member queued before the macro that refused runs anyway.
	macroFeedback(c, "lead ran")
	c.Disconnect()
}

// TestFunctionTagInstantiatesItsMacroPerCall checks that each call of a tag
// instantiates its members from the arguments of its own: the
// instantiations never go back into the tag's shared line-up.
// vanilla: FunctionCommand.instantiateAndQueueFunctions
func TestFunctionTagInstantiatesItsMacroPerCall(t *testing.T) {
	t.Parallel()
	c := functionWorld(t, map[string]string{
		"t/function/val.mcfunction": "$say n:$(n)\n",
		"t/tags/function/one.json":  `{"values":["t:val"]}`,
	})

	for _, v := range []string{"1", "2"} {
		c.ChatCommand("data merge storage t:v {n:" + v + "}")
		c.ExpectCommandFeedback("commands.data.storage.modified", "t:v")
		c.ChatCommand("function #t:one with storage t:v")
		c.ExpectCommandFeedback("commands.function.scheduled.single", "t:val")
		macroFeedback(c, "n:"+v)
	}
	c.Disconnect()
}

// TestFunctionMacroSpellsInstantiationFailures checks the reason a macro
// call that failed to instantiate carries: the dispatcher's own wording
// rendered through the client's language table, with the position and the
// context line the client points at.
// vanilla: MacroFunction.MacroEntry.instantiate, MacroFunction.substituteAndParse
func TestFunctionMacroSpellsInstantiationFailures(t *testing.T) {
	t.Parallel()
	c := functionWorld(t, map[string]string{
		"t/function/saymac.mcfunction":  "$say$(x)\n",
		"t/function/timemac.mcfunction": "$time set $(x)\n",
		"t/function/asmods.mcfunction":  "$execute as @a $(x)\n",
		"t/function/runmac.mcfunction":  "$execute as @a run $(x)\n",
	})

	// An empty value glued to the command word leaves the line the shape
	// vanilla answers with its unknown-command wording, at the line's end.
	c.ChatCommand(`function t:saymac {x:""}`)
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:saymac")
	c.ExpectCommandFailure(macroInstantiationFailure("t:saymac", protocol.Text{
		Translate: "commands.function.error.parse", With: []protocol.Text{
			{Text: "t:saymac"}, {Text: "say"},
			{Text: "Unknown or incomplete command. See below for error at position 3: say<--[HERE]"}}}))
	// An empty value behind its arguments leaves the input's end unread,
	// which the dispatcher answers with its unknown-argument wording,
	// where the read stopped.
	c.ChatCommand(`function t:timemac {x:""}`)
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:timemac")
	c.ExpectCommandFailure(macroInstantiationFailure("t:timemac", protocol.Text{
		Translate: "commands.function.error.parse", With: []protocol.Text{
			{Text: "t:timemac"}, {Text: "time set "},
			{Text: "Incorrect argument for command at position 8: time set<--[HERE]"}}}))
	// The values the line ended without are its own case: one trailing
	// space behind a redirecting word is the separator its continuation
	// skips, so the line ends with no command after it and the
	// unknown-command wording lands at the input's end, not at the word.
	c.ChatCommand(`function t:asmods {x:""}`)
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:asmods")
	c.ExpectCommandFailure(macroInstantiationFailure("t:asmods", protocol.Text{
		Translate: "commands.function.error.parse", With: []protocol.Text{
			{Text: "t:asmods"}, {Text: "execute as @a "},
			{Text: "Unknown or incomplete command. See below for error at position 14: ...ute as @a <--[HERE]"}}}))
	c.ChatCommand(`function t:runmac {x:""}`)
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:runmac")
	c.ExpectCommandFailure(macroInstantiationFailure("t:runmac", protocol.Text{
		Translate: "commands.function.error.parse", With: []protocol.Text{
			{Text: "t:runmac"}, {Text: "execute as @a run "},
			{Text: "Unknown or incomplete command. See below for error at position 18: ...as @a run <--[HERE]"}}}))
	c.Disconnect()
}

// TestScheduleRefusesAMacroAndDropsTagMemberQuietly checks /schedule with
// macros: a macro named directly is refused, a tag schedules regardless,
// and at its fire time the game loop drops the macro member quietly while
// the plain member beside it runs.
// vanilla: ScheduleCommand.schedule, ServerFunctionManager.execute
func TestScheduleRefusesAMacroAndDropsTagMemberQuietly(t *testing.T) {
	t.Parallel()
	srv, connect := startFunctionServer(t, map[string]string{
		"t/function/quiet.mcfunction": "$scoreboard players add #m cnt $(v)\n",
		"t/function/noisy.mcfunction": "scoreboard players add #p cnt 1\n",
		"t/tags/function/mixed.json":  `{"values":["t:quiet","t:noisy"]}`,
	}, "Testificate")
	servertest.FreezeUntilHeld(t, srv)
	c := connect().Join("Testificate")
	c.ChatCommand("scoreboard objectives add cnt dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	servertest.HoldTicks(t, srv)

	// A scheduled function runs with no arguments to substitute, so a
	// macro the schedule could never run is refused where it is asked for.
	c.ChatCommand("schedule function t:quiet 2")
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.schedule.macro"))
	// The tag schedules fine; at its fire time the macro member drops
	// quietly and the plain one runs.
	schedule(t, c, "#t:mixed 2", "commands.schedule.created.tag", "t:mixed", 2)
	servertest.RunTicks(t, srv, 2)
	// No line of the macro member ran: its holder was never scored, while
	// the plain member beside it did run.
	c.ChatCommand("scoreboard players get #m cnt")
	expectScoreboardRefusal(t, c, "commands.scoreboard.players.get.null")
	if got := score(t, c, "#p"); got != 1 {
		t.Errorf("the plain function touched its counter %d times, want 1", got)
	}
	c.Disconnect()
}
