package command

import (
	"slices"
	"strings"
	"testing"
)

// The answer a Match caller sends for a failure: the key, the %s and the
// cursor in the input, as Commands.finishParsing builds them. This is the
// mapping internal/server/commands.go, the console and the command blocks each carry.
func failureAnswer(input string, fail *Failure) (string, []string, int) {
	if fail.Err != nil {
		cursor := fail.Err.Cursor
		if fail.Err.NoContext {
			// A refusal with no input travels as its message alone; -1 is
			// no cursor, the way a caller sends no context line for it.
			cursor = -1
		}
		return fail.Err.Key, fail.Err.With, cursor
	}
	if fail.UnknownCommand {
		cursor := 0
		if fail.At > 0 {
			cursor = len(input)
		}
		return "command.unknown.command", nil, cursor
	}
	if fail.Trailing {
		return "command.unknown.argument", nil, TrailingCursor(input, Tokenize(input))
	}
	return "command.unknown.argument", nil, Tokenize(input)[fail.At].Start
}

// TestMatchFailureSelection sends whole commands through the tree and
// expects the error a vanilla server answered them with, the cursor
// where its reader stopped. A node with more than one argument child speaks
// for all of them, which is why a malformed coordinate in /tp is an unknown
// argument where its branch begins rather than the coordinate parser's own
// error; a node with one child whose error is the only one on the winning
// branch still names it.
func TestMatchFailureSelection(t *testing.T) {
	tree := ForLevel(PermLevelGamemasters)
	cases := []struct {
		input  string
		key    string
		with   []string
		cursor int
	}{
		{"tp ^ ^ 1", "command.unknown.argument", nil, 3},
		{"teleport @s 0 64 0 1.x 0", "command.unknown.argument", nil, 12},
		{"tp 1.2.3 64 0", "command.unknown.argument", nil, 9},
		{"tp @s 1 2", "command.unknown.argument", nil, 6},
		{"tp @s 1.2.3 64 0", "command.unknown.argument", nil, 6},
		{"summon minecraft:zombie 1.2.3 64 0", keyDoubleInvalid, []string{"1.2.3"}, 24},
		{"summon minecraft:zombie 1 64", keyPos3DIncomplete, nil, 24},
		{"execute rotated 1 run say hi", keyDoubleExpected, nil, 18},
		{"time foo", "command.unknown.argument", nil, 5},
		{"xyz", "command.unknown.command", nil, 0},
		{"tp", "command.unknown.command", nil, 2},
		{"time set Noon", "command.unknown.argument", nil, 9},
		{"time set minecraft:Noon", "command.unknown.argument", nil, 9},
		{"time add noon", keyFloatExpected, nil, 9},
		{"time add 1x", keyTimeInvalidUnit, nil, 11},
		{"give @s Stone", keyItemIDInvalid, []string{"minecraft:"}, 8},
		{"give @s minecraft:", keyItemIDInvalid, []string{"minecraft:"}, 8},
		{"give @s nonexistent{a:1}", keyItemIDInvalid, []string{"minecraft:nonexistent"}, 8},
		{"setblock 0 64 0 Stone", keyBlockIDInvalid, []string{"minecraft:"}, 16},
		{"setblock 0 64 0 notablock{a:1}", keyBlockIDInvalid, []string{"minecraft:notablock"}, 16},
		{"setblock 0 64 0 #minecraft:logs", keyBlockTagDisallowed, nil, 16},
		{"setblock 0 64 0 #minecraft:logs[axis=y]", keyBlockTagDisallowed, nil, 16},
		{"fill 0 64 0 1 64 1 #minecraft:logs", keyBlockTagDisallowed, nil, 19},
		{"execute if block 0 64 0 notablock run say hi", keyBlockIDInvalid, []string{"minecraft:notablock"}, 24},
		{"fill 0 64 0 1 65 1 stone replace notablock", keyBlockIDInvalid, []string{"minecraft:notablock"}, 33},
		{"setblock 0 64 0 stone[foo=bar]", keyBlockPropertyUnknown,
			[]string{"minecraft:stone", "foo"}, strings.Index("setblock 0 64 0 stone[foo=bar]", "foo")},
		{"setblock 0 64 0 oak_log[axis=q]", keyBlockPropertyInvalid,
			[]string{"minecraft:oak_log", "q", "axis"}, strings.Index("setblock 0 64 0 oak_log[axis=q]", "=q") + 1},
		{"setblock 0 64 0 oak_log[axis=]", keyBlockPropertyInvalid,
			[]string{"minecraft:oak_log", "", "axis"}, strings.Index("setblock 0 64 0 oak_log[axis=]", "]")},
		{"setblock 0 64 0 oak_log[=y]", keyBlockPropertyUnknown,
			[]string{"minecraft:oak_log", ""}, strings.Index("setblock 0 64 0 oak_log[=y]", "=")},
		{"setblock 0 64 0 oak_stairs[facing=east, bogus=1]", keyBlockPropertyUnknown,
			[]string{"minecraft:oak_stairs", "bogus"}, strings.Index("setblock 0 64 0 oak_stairs[facing=east, bogus=1]", "bogus")},
		{"setblock 0 64 0 oak_stairs[facing=east, ]", keyBlockPropertyUnknown,
			[]string{"minecraft:oak_stairs", ""}, strings.Index("setblock 0 64 0 oak_stairs[facing=east, ]", "]")},
		{"execute if block 0 64 0 #minecraft:stairs[half=top, ] run say hi", keyBlockPropertyNoValue,
			[]string{"", "minecraft:"}, strings.Index("execute if block 0 64 0 #minecraft:stairs[half=top, ] run say hi", "]")},
		{"setblock 0 64 0 oak_log[axis=y,axis=x]", keyBlockPropertyDuplicate,
			[]string{"axis", "minecraft:oak_log"}, strings.LastIndex("setblock 0 64 0 oak_log[axis=y,axis=x]", "axis")},
		{"setblock 0 64 0 oak_log[axis]", keyBlockPropertyNoValue,
			[]string{"axis", "minecraft:oak_log"}, strings.Index("setblock 0 64 0 oak_log[axis]", "]")},
		{"setblock 0 64 0 oak_log[axis=y!]", keyBlockPropertyUnclosed, nil,
			strings.Index("setblock 0 64 0 oak_log[axis=y!]", "!")},
		{"setblock 0 64 0 oak_log[axis=\"q\"]", keyBlockPropertyInvalid,
			[]string{"minecraft:oak_log", "q", "axis"},
			strings.Index("setblock 0 64 0 oak_log[axis=\"q\"]", "\"")},
		{"setblock 0 64 0 oak_log[axis=y", keyBlockPropertyUnclosed, nil, len("setblock 0 64 0 oak_log[axis=y")},
		{"setblock 0 64 0 oak_log[axis=y[x]", keyBlockPropertyUnclosed, nil,
			strings.Index("setblock 0 64 0 oak_log[axis=y[x]", "[x]")},
		{"setblock 0 64 0 oak_log[axis", keyBlockPropertyNoValue,
			[]string{"axis", "minecraft:oak_log"}, len("setblock 0 64 0 oak_log[axis")},
		{"setblock 0 64 0 stone{foo", keyLiteralIncorrect, []string{":"}, len("setblock 0 64 0 stone{foo")},
		{"give @s stone 0", keyIntegerLow, []string{"1", "0"}, 14},
		{"clear @s stone -1", keyIntegerLow, []string{"0", "-1"}, 15},
		{"gamerule random_tick_speed -1", keyIntegerLow, []string{"0", "-1"}, 27},
		{"gamerule keep_inventory maybe", keyBoolInvalid, []string{"maybe"}, 24},
		{"gamerule keep_inventory TRUE", keyBoolInvalid, []string{"TRUE"}, 24},
		{"gamerule keep_inventory true!", keyExpectedSeparator, nil, 28},
		{"gamerule keep_inventory maybe!", keyBoolInvalid, []string{"maybe"}, 24},
		{`gamerule keep_inventory ""`, keyBoolExpected, nil, 26},
		{`gamerule keep_inventory !`, keyBoolExpected, nil, 24},
		{`gamerule keep_inventory "true`, keyQuoteExpectedEnd, nil, 29},
		{`gamerule keep_inventory "tr\ue"`, keyQuoteEscape, []string{"u"}, 28},
		{`gamerule keep_inventory "tr\"ue"`, keyBoolInvalid, []string{`tr"ue`}, 24},
		{`gamerule keep_inventory "tr ue"`, keyBoolInvalid, []string{"tr ue"}, 24},
		{`gamerule keep_inventory "true false"`, keyBoolInvalid, []string{"true false"}, 24},
		{`gamerule keep_inventory " true"`, keyBoolInvalid, []string{" true"}, 24},
		{`gamerule keep_inventory "true "`, keyBoolInvalid, []string{"true "}, 24},
		{`gamerule keep_inventory "true x`, keyQuoteExpectedEnd, nil, 31},
		{`effect give @s speed 5 1 "tr ue"`, keyBoolInvalid, []string{"tr ue"}, 25},
		{"time add 1d!", keyExpectedSeparator, nil, 11},
		{"time add 5t,", keyExpectedSeparator, nil, 11},
		{"weather rain 5s!", keyExpectedSeparator, nil, 15},
		{"time add 1d5", keyTimeInvalidUnit, nil, 12},
		{"time add 1d.", keyTimeInvalidUnit, nil, 12},
		{"time add 1t_", keyTimeInvalidUnit, nil, 12},
		{"time add 1s+", keyTimeInvalidUnit, nil, 12},
		{"weather rain 5s-", keyTimeInvalidUnit, nil, 16},
		{"time add 5x!", keyTimeInvalidUnit, nil, 11},
		{"time add 5dx!", keyTimeInvalidUnit, nil, 12},
		{"effect give @s speed 0", keyIntegerLow, []string{"1", "0"}, 21},
		{"effect give @s speed 1000001", keyIntegerBig, []string{"1000000", "1000001"}, 21},
		{"effect give @s speed 5 256", keyIntegerBig, []string{"255", "256"}, 23},
		{"effect give @s speed 5 -1", keyIntegerLow, []string{"0", "-1"}, 23},
		// The time argument's own minimum, refused where its reading ended.
		{"weather rain 0", keyTimeTooLow, []string{"1", "0"}, 14},
		// The item parser reads each component entry before it reaches the
		// bracket it was reading towards, so an unknown name wins over the
		// missing bracket.
		{"give @s stone[foo", giveComponentUnknownKey, []string{"minecraft:foo"},
			strings.Index("give @s stone[foo", "foo")},
		{"give @s stone[minecraft:damage=1,", giveComponentExpectedKey, nil,
			len("give @s stone[minecraft:damage=1,")},
		{"give @s stone[minecraft:damage=1", keyExpectedSymbol, []string{"]"},
			len("give @s stone[minecraft:damage=1")},
		{"give @s stone[", keyExpectedSymbol, []string{"]"}, len("give @s stone[")},
		// An item predicate's tag is looked up, and its bracket is read as a
		// predicate's own, not a stack's. A predicate's unknown name is the
		// ResourceLookupRule's, refused with the reader past the identifier,
		// so the cursor sits at the name's end; the stack path winds its
		// reader back to the start.
		{"clear @s #nonexistent", itemTagUnknownKey, []string{"minecraft:nonexistent"},
			len("clear @s #nonexistent")},
		{"clear @s #minecraft:logs[bogus]", giveComponentUnknownKey, []string{"minecraft:bogus"},
			strings.Index("clear @s #minecraft:logs[bogus]", "bogus") + len("bogus")},
		// A term's '!', the '~' before its sub-predicate and each '|'
		// alternative are read off, so the bare names behind them are what
		// the registry is asked about.
		{"clear @s stone[!bogus]", giveComponentUnknownKey, []string{"minecraft:bogus"},
			strings.Index("clear @s stone[!bogus]", "bogus") + len("bogus")},
		{"clear @s stone[damage|bogus]", giveComponentUnknownKey, []string{"minecraft:bogus"},
			strings.Index("clear @s stone[damage|bogus]", "bogus") + len("bogus")},
		{"clear @s stone[bogus~{}]", giveComponentUnknownKey, []string{"minecraft:bogus"},
			strings.Index("clear @s stone[bogus~{}]", "bogus") + len("bogus")},
		{"give @s stone[!bogus]", giveComponentUnknownKey, []string{"minecraft:bogus"},
			strings.Index("give @s stone[!bogus]", "bogus")},
		// Transient components have no codec: the item argument refuses
		// them, and a predicate keeps only the '~' form.
		{"give @s stone[creative_slot_lock={}]", giveComponentUnknownKey, []string{"minecraft:creative_slot_lock"},
			strings.Index("give @s stone[creative_slot_lock={}]", "creative_slot_lock")},
		{"clear @s *[creative_slot_lock]", keyLiteralIncorrect, []string{"~"}, len("clear @s *[creative_slot_lock")},
		{"clear @s *[creative_slot_lock=1]", keyLiteralIncorrect, []string{"~"}, len("clear @s *[creative_slot_lock")},
		{"clear @s *[!map_post_processing]", keyLiteralIncorrect, []string{"~"}, len("clear @s *[!map_post_processing")},
		// A malformed predicate reads like the packrat grammar it is: an
		// identifier that reads nowhere is argument.id.invalid back at the
		// read's start, and a rule that stopped short names the literal it
		// was reading towards where its longest-failure landed.
		{"clear @a Stone", keyIDInvalid, nil, strings.Index("clear @a Stone", "Stone")},
		{"clear @a #", keyIDInvalid, nil, len("clear @a #")},
		{"clear @s stone[", keyIDInvalid, nil, len("clear @s stone[")},
		{"clear @s stone[!]", keyIDInvalid, nil, len("clear @s stone[!")},
		{"clear @s stone[a:b:c]", keyIDInvalid, nil, strings.Index("clear @s stone[a:b:c]", "a:b:c")},
		{"clear @s stone[damage=1", keyLiteralIncorrect, []string{"|"}, len("clear @s stone[damage=1")},
		{"clear @s stone[damage~{}x]", keyLiteralIncorrect, []string{"|"},
			strings.Index("clear @s stone[damage~{}x]", "x")},
		{"clear @s stone[damage", keyLiteralIncorrect, []string{"="}, len("clear @s stone[damage")},
		{"clear @s stone[damage=1,", keyIDInvalid, nil, len("clear @s stone[damage=1,")},
		// A bare predicate value is read by the SNBT grammar; 1. is the
		// double it spells, so the dot behind it is the separator the
		// list wanted.
		{"clear @s stone[count=1..5]", keyLiteralIncorrect, []string{"|"},
			strings.Index("clear @s stone[count=1..5]", "..") + 1},
		{"clear @s stone[damage|]", keyIDInvalid, nil, strings.Index("clear @s stone[damage|]", "|") + 1},
		// A list the reader left a tail behind is the dispatcher's missing
		// separator, on the predicate path and the stack path alike.
		{"clear @s stone[damage]x", keyExpectedSeparator, nil, strings.Index("clear @s stone[damage]x", "x")},
		{"give @s stone[damage=1]x", keyExpectedSeparator, nil, strings.Index("give @s stone[damage=1]x", "x")},
		// A removed stack component carries no value and none may follow
		// before the list's separator or bracket.
		{"give @s stone[!damage=1]", keyExpectedSymbol, []string{"]"}, strings.Index("give @s stone[!damage=1]", "=")},
		{"give @s stone[! damage]", giveComponentUnknownKey, []string{"minecraft:"},
			strings.Index("give @s stone[! damage]", "! ") + 1},
		{"give @s stone[!damage x]", keyExpectedSymbol, []string{"]"}, strings.Index("give @s stone[!damage x]", "x")},
		// A message argument reads the selectors its text carries: only an
		// unknown or missing selector letter is stepped over, everything
		// else stops the branch, and text over 256 characters is refused.
		{"say hi @a[", keyOptionsUnterminated, nil, len("say hi @a[")},
		{"say hi @a[limit=0]", keyOptionsLimitTooSmall, nil, 16},
		// The cap counts the UTF-16 code units of the raw text from the
		// argument's start, so a run of spaces counts as typed and a
		// non-ASCII message is not refused for its byte count.
		{"say " + strings.Repeat("a", 257), keyMessageTooLong, []string{"257", "256"}, -1},
		{"say " + strings.Repeat("a  ", 100), keyMessageTooLong, []string{"300", "256"}, -1},
		{"say " + strings.Repeat("ф", 257), keyMessageTooLong, []string{"257", "256"}, -1},
		// A single word's argument left the input's end unread: the
		// dispatcher's separator rules answer an unknown argument where
		// the read stopped, an SNBT compound's tail included.
		{"time set daytime ", "command.unknown.argument", nil, len("time set daytime")},
		{"function t:one {v:1} trailing", "command.unknown.argument", nil,
			strings.Index("function t:one {v:1} trailing", "trailing")},
		{"function t:one {v:1} ", "command.unknown.argument", nil, len("function t:one {v:1}")},
		{"summon minecraft:cow 0 70 0 {NoAI:1b} extra", "command.unknown.argument", nil,
			strings.Index("summon minecraft:cow 0 70 0 {NoAI:1b} extra", "extra")},
		{"data merge storage t:o {v:1} extra", "command.unknown.argument", nil,
			strings.Index("data merge storage t:o {v:1} extra", "extra")},
		// A tail glued to the compound's close stays the dispatcher's
		// missing separator, at the first character behind the compound.
		{"summon minecraft:cow 0 70 0 {NoAI:1b}x", keyExpectedSeparator, nil,
			strings.Index("summon minecraft:cow 0 70 0 {NoAI:1b}x", "x")},
		// A single trailing space behind a redirecting word is the one
		// separator its continuation skips, so the read ends with the
		// input and no command after it: the input's end answers an
		// unknown command. Behind a word that redirects nowhere the
		// space stays unread, and so does the one past the first behind
		// a redirect, where the dispatcher's read picks them up.
		{"execute as @a ", "command.unknown.command", nil, len("execute as @a ")},
		{"execute as @a run ", "command.unknown.command", nil, len("execute as @a run ")},
		{"tp ", "command.unknown.command", nil, len("tp ")},
		{"execute facing ", "command.unknown.argument", nil, len("execute facing")},
		{"time set ", "command.unknown.argument", nil, len("time set")},
		{"execute as @a  ", "command.unknown.argument", nil, len("execute as @a") + 1},
		{"time set daytime  ", "command.unknown.argument", nil, len("time set daytime") + 1},
	}
	for _, tc := range cases {
		node, fail := Match(tree, tc.input, Tokenize(tc.input), true)
		if node != nil {
			t.Errorf("%q: accepted", tc.input)
			continue
		}
		key, with, cursor := failureAnswer(tc.input, fail)
		if key != tc.key || !slices.Equal(with, tc.with) || cursor != tc.cursor {
			t.Errorf("%q: got %s %q at %d, want %s %q at %d", tc.input, key, with, cursor, tc.key, tc.with, tc.cursor)
		}
	}

	// A block state's names and values read through StringReader.readString,
	// so either quote form stands for the text inside the quotes, and an
	// integer value reads through Integer.parseInt, so a padded spelling
	// names the same value. All of them are accepted.
	for _, input := range []string{
		`setblock 0 64 0 oak_log["axis"=y]`,
		`setblock 0 64 0 oak_log['axis'=y]`,
		`setblock 0 64 0 oak_log[axis="y"]`,
		`setblock 0 64 0 oak_log[axis='y']`,
		`setblock 0 64 0 wheat[age=07]`,
		`setblock 0 64 0 oak_stairs[ facing = east , half=top ]`,
		`execute if block 0 64 0 chest{Items:[{Slot:0b, id:"minecraft:stone"}]} run say hi`,
		`execute if block 0 64 0 #minecraft:stairs[half=top, facing=east] run say hi`,
		`fill 0 64 0 1 64 1 stone replace oak_stairs[half=top, facing=east]`,
		`clone 0 64 0 1 64 1 5 64 5 filtered oak_stairs[half=top, facing=east]`,
	} {
		if node, _ := Match(tree, input, Tokenize(input), true); node == nil {
			t.Errorf("%q: refused, want accepted", input)
		}
	}

	// A greedy message reader runs with the spaces its values trail.
	for _, input := range []string{
		`say hi `,
		`say hi  `,
	} {
		if node, _ := Match(tree, input, Tokenize(input), true); node == nil {
			t.Errorf("%q: refused, want accepted", input)
		}
	}

	// The quoted spellings vanilla's boolean reader takes read as the
	// text inside the quotes, so the argument accepts them, and a quoted
	// hideParticles reaches the effect command's handler the same way.
	for _, input := range []string{
		`gamerule keep_inventory "true"`,
		`gamerule keep_inventory 'false'`,
		`effect give @s speed 5 1 "true"`,
	} {
		if node, _ := Match(tree, input, Tokenize(input), true); node == nil {
			t.Errorf("%q: refused, want accepted", input)
		}
	}

	// A /time set below the time argument's minimum reaches the marker
	// branch beside it, the same word vanilla's dispatcher hands the marker
	// handler for: accepted, refused only by the command's own run.
	for _, input := range []string{
		"time set -5",
		"time set -5t",
	} {
		if node, _ := Match(tree, input, Tokenize(input), true); node == nil {
			t.Errorf("%q: refused, want the marker branch to accept it", input)
		}
	}

	// A bare hash reads as the empty location the minecraft namespace fills,
	// so the function argument accepts it rather than refusing the empty read.
	if node, _ := Match(tree, "execute if function # run say hi", Tokenize("execute if function # run say hi"), true); node == nil {
		t.Errorf("bare function tag: refused, want accepted")
	}

	// An item predicate takes any item, a tag or an item name, and its
	// bracket list is read as a predicate's own: count joins the components
	// and the entries need no value. A term may be negated with '!', a
	// comma group may be '|' alternatives, and a sub-predicate follows '~'.
	for _, input := range []string{
		"clear @s *",
		"clear @s * 1",
		"clear @s *[damage]",
		"clear @s stone[damage]",
		"clear @s stone[custom_name='a b']",
		"clear @s stone[damage=1, max_damage=5]",
		"clear @s #minecraft:logs[damage]",
		"clear @s stone[!damage]",
		"clear @s stone[damage|count=1]",
		"clear @s stone[damage~{}]",
		"clear @s stone[minecraft:damage~{durability:1}]",
		"execute if items entity @s weapon.mainhand * run say hi",
	} {
		if node, _ := Match(tree, input, Tokenize(input), true); node == nil {
			t.Errorf("%q: refused, want accepted", input)
		}
	}

	// An item stack's list takes a '!' entry as the removal of that
	// component, alone or beside ones it sets.
	for _, input := range []string{
		"give @s stone[!damage]",
	} {
		if node, _ := Match(tree, input, Tokenize(input), true); node == nil {
			t.Errorf("%q: refused, want accepted", input)
		}
	}

	// A predicate value the grammar cannot read at all is refused.
	for _, input := range []string{
		"clear @s stone[damage~]",
		"clear @s stone[damage=]",
		"clear @s stone[damage|]",
	} {
		if node, _ := Match(tree, input, Tokenize(input), true); node != nil {
			t.Errorf("%q: accepted, want refused", input)
		}
	}
}

// TestMatchSNBTComponentValues reads a component list's values with the nbt
// package's own grammar: the numerals with underscores, the 0x and 0b
// prefixes and the integer suffixes the grammar takes, and the shapes it
// refuses, while a value it cannot read at all is named where the grammar
// stopped.
func TestMatchSNBTComponentValues(t *testing.T) {
	tree := ForLevel(PermLevelGamemasters)
	for _, input := range []string{
		"clear @a stone[damage=1_000]",
		"clear @a stone[damage=1__0]",
		"clear @a stone[damage=1_0.5]",
		"clear @a stone[damage=1.5_5]",
		"clear @a stone[damage=1e1_0]",
		"clear @a stone[damage=0x10]",
		"clear @a stone[damage=0X1f]",
		"clear @a stone[damage=0xFF]",
		"clear @a stone[damage=0b101]",
		"clear @a stone[damage=0x10l]",
		"clear @a stone[damage=0x10ub]",
		"clear @a stone[damage=bool(1)]",
		// The fraction behind a dot is optional, so a dot alone is a
		// double and an exponent or a suffix may follow it.
		"clear @a stone[damage=1.]",
		"clear @a stone[damage=1.e5]",
		"clear @a stone[damage=1.f]",
		"give @a stone[damage=1.]",
		// A Unicode name escape is valid SNBT and reads as its character.
		"clear @a stone[item_name=\"\\N{DIGIT ONE}\"]",
		"give @s stone[item_name=\"\\N{DIGIT ONE}\"]",
		"give @s stone[damage=1_000]",
		"give @s stone[damage=0x10ub]",
		"give @s stone[damage=bool(1)]",
		"execute if items entity @s weapon.mainhand stone[damage=1_000] run say hi",
	} {
		if node, _ := Match(tree, input, Tokenize(input), true); node == nil {
			t.Errorf("%q: refused, want accepted", input)
		}
	}

	// A suffix a width letter does not follow, a base prefix with no numeral
	// behind it, a leading zero and a bracket that closes the wrong shape
	// are all refused.
	for _, input := range []string{
		"clear @a stone[damage=1u]",
		"clear @a stone[damage=1bb]",
		"clear @a stone[damage=1bs]",
		"clear @a stone[damage=1ib]",
		"clear @a stone[damage=1sbb]",
		"clear @a stone[damage=1fd]",
		"clear @a stone[damage=1ff]",
		"clear @a stone[damage=1.5ff]",
		"clear @a stone[damage=1.5b]",
		"clear @a stone[damage=1.5l]",
		"clear @a stone[damage=.]",
		"clear @a stone[damage=-.]",
		"clear @a stone[damage=00]",
		"clear @a stone[damage=01]",
		"clear @a stone[damage=007]",
		"clear @s stone[damage={,}]",
		"clear @s stone[damage={a}]",
		"clear @s stone[damage={a:}]",
		"clear @s stone[damage={:1}]",
		"clear @s stone[damage={a::1}]",
		"clear @s stone[damage=[1 2]]",
		"clear @s stone[damage=[,]]",
		"clear @s stone[damage={a:1 b:2}]",
		"clear @s stone[damage={a:1,,b:2}]",
		"clear @s stone[damage=\"\\q\"]",
		"clear @s stone[damage=[1}]",
	} {
		if node, _ := Match(tree, input, Tokenize(input), true); node != nil {
			t.Errorf("%q: accepted, want refused", input)
		}
	}

	// An unterminated value names the refusal the grammar ended on, at its
	// own failure cursor.
	for _, tc := range []struct {
		input  string
		key    string
		with   []string
		cursor int
	}{
		{"clear @s stone[damage={a:1", keyLiteralIncorrect, []string{"."},
			len("clear @s stone[damage={a:1")},
		{"clear @s stone[damage='a", "snbt.parser.invalid_string_contents", nil,
			len("clear @s stone[damage='a")},
		{"clear @s stone[damage=\"abc", "snbt.parser.invalid_string_contents", nil,
			len("clear @s stone[damage=\"abc")},
		// An unterminated container names the literal its next byte wanted:
		// a compound's entry, a list's type letter, an array element's sign
		// or width letter. vanilla: SnbtGrammar.list_literal, array_prefix
		{"clear @s stone[damage={", keyLiteralIncorrect, []string{"\""},
			len("clear @s stone[damage={")},
		{"clear @s stone[damage=[{", keyLiteralIncorrect, []string{"\""},
			len("clear @s stone[damage=[{")},
		{"clear @s stone[damage={a:1,", keyLiteralIncorrect, []string{"\""},
			len("clear @s stone[damage={a:1,")},
		{"clear @s stone[damage=[", keyLiteralIncorrect, []string{"B"},
			len("clear @s stone[damage=[")},
		{"clear @s stone[damage=[[", keyLiteralIncorrect, []string{"B"},
			len("clear @s stone[damage=[[")},
		{"clear @s stone[damage=[B;", keyLiteralIncorrect, []string{"+"},
			len("clear @s stone[damage=[B;")},
		{"clear @s stone[damage=[B;1,", keyLiteralIncorrect, []string{"+"},
			len("clear @s stone[damage=[B;1,")},
		{"clear @s stone[damage=[B;1", keyLiteralIncorrect, []string{"u|U"},
			len("clear @s stone[damage=[B;1")},
		{"clear @a stone[damage={a:1u}]", keyLiteralIncorrect, []string{"b|B"},
			strings.Index("clear @a stone[damage={a:1u}]", "u") + 1},
		// A number the float rule cannot finish reads as the longest value
		// behind it, so the list's separator, not the number's own rule, is
		// what the refusal names.
		{"clear @a stone[damage=1e]", keyLiteralIncorrect, []string{"|"},
			strings.Index("clear @a stone[damage=1e]", "1e]") + 1},
		{"clear @a stone[damage=1e+]", keyLiteralIncorrect, []string{"|"},
			strings.Index("clear @a stone[damage=1e+]", "1e+]") + 1},
		{"clear @a stone[damage=1e400]", keyLiteralIncorrect, []string{"|"},
			strings.Index("clear @a stone[damage=1e400]", "1e400]") + 1},
		{"clear @a stone[damage=1.5_f]", keyLiteralIncorrect, []string{"|"},
			strings.Index("clear @a stone[damage=1.5_f]", "1.5_f]") + 2},
		{"clear @a stone[damage=bool(1]", keyLiteralIncorrect, []string{"|"},
			strings.Index("clear @a stone[damage=bool(1]", "(1]")},
		{"clear @a stone[damage=true(]", "snbt.parser.expected_unquoted_string", nil,
			strings.Index("clear @a stone[damage=true(]", "true(]") + len("true(")},
		{"clear @a stone[damage=abc()]", "snbt.parser.no_such_operation", []string{"abc/0"},
			strings.Index("clear @a stone[damage=abc()]", "abc()]") + len("abc()")},
	} {
		node, fail := Match(tree, tc.input, Tokenize(tc.input), true)
		if node != nil {
			t.Errorf("%q: accepted, want refused", tc.input)
			continue
		}
		key, with, cursor := failureAnswer(tc.input, fail)
		if key != tc.key || !slices.Equal(with, tc.with) || cursor != tc.cursor {
			t.Errorf("%q: got %s %q at %d, want %s %q at %d", tc.input, key, with, cursor, tc.key, tc.with, tc.cursor)
		}
	}
}

// TestTickRateArgumentReadsABrigadierFloat checks the /tick rate argument
// is read as brigadier's float with vanilla's 1.0..10000.0 bounds, so a word
// that is no float and one outside the range stop the command in the walk
// rather than reaching the handler.
// vanilla: TickCommand.register, FloatArgumentType.parse
func TestTickRateArgumentReadsABrigadierFloat(t *testing.T) {
	tree := ForLevel(PermLevelOwners)
	for _, tc := range []struct {
		input string
		key   string
		with  []string
	}{
		{"tick rate abc", keyFloatExpected, nil},
		{"tick rate inf", keyFloatExpected, nil},
		{"tick rate NaN", keyFloatExpected, nil},
		{"tick rate 1.2.3", keyFloatInvalid, []string{"1.2.3"}},
		{"tick rate 0", keyFloatLow, []string{"1.0", "0.0"}},
		{"tick rate -5", keyFloatLow, []string{"1.0", "-5.0"}},
		{"tick rate 20000", keyFloatBig, []string{"10000.0", "20000.0"}},
	} {
		node, fail := Match(tree, tc.input, Tokenize(tc.input), true)
		if node != nil {
			t.Errorf("%q: accepted, want refused", tc.input)
			continue
		}
		if fail.Err == nil {
			t.Errorf("%q: no typed error, want %s", tc.input, tc.key)
			continue
		}
		if fail.Err.Key != tc.key || !slices.Equal(fail.Err.With, tc.with) {
			t.Errorf("%q: got %s %q, want %s %q", tc.input, fail.Err.Key, fail.Err.With, tc.key, tc.with)
		}
	}
	if node, _ := Match(tree, "tick rate 20", Tokenize("tick rate 20"), true); node == nil {
		t.Error("tick rate 20: refused, want accepted")
	}
}

// TestParseTimeArg checks the tick counts ParseTimeArg reads, including its
// float rounding and int saturation.
// vanilla: TimeArgument.parse
func TestParseTimeArg(t *testing.T) {
	for _, tc := range []struct {
		arg  string
		want int64
		ok   bool
	}{
		{"100", 100, true},
		{"100t", 100, true},
		{"5s", 100, true},
		{"0.5d", 12000, true},
		// Fractions round Math.round's way.
		{"2.5", 3, true},
		{"0.5t", 1, true},
		// The number parses as a Java float, so one an integer cannot state
		// in single precision has already lost digits by the rounding.
		{"16777217", 16777216, true},
		// The count saturates at the int range rather than overflowing it.
		{"100000d", 2147483647, true},
		{"2147483648", 2147483647, true},
		{"-2147483648", -2147483648, true},
		{strings.Repeat("9", 40), 2147483647, true},
		{"-" + strings.Repeat("9", 40), -2147483648, true},
		// The shape the walk refuses before the handler reads a value.
		{"day", 0, false},
		{"1x", 0, false},
		{"+5", 0, false},
	} {
		got, ok := ParseTimeArg(tc.arg)
		if ok != tc.ok || got != tc.want {
			t.Errorf("ParseTimeArg(%q) = (%d, %v), want (%d, %v)", tc.arg, got, ok, tc.want, tc.ok)
		}
	}
}

// TestBlockDataArgumentNamesMatchTheCommandReport checks the block position
// argument names a 26.3 commands.json report gives (vanilla:
// BlockDataAccessor.PROVIDER takes its name from DataCommands' "target" and
// "source" provider lists); the client's usage hint shows them.
func TestBlockDataArgumentNamesMatchTheCommandReport(t *testing.T) {
	root := ForLevel(PermLevelOwners)
	cases := []struct {
		path []string
		want string
	}{
		{[]string{"data", "get", "block"}, "target"},
		{[]string{"data", "modify", "block", "target", "targetPath", "set", "from", "block"}, "source"},
		{[]string{"execute", "if", "data", "block"}, "source"},
		{[]string{"execute", "store", "result", "block"}, "target"},
		{[]string{"function", "name", "with", "block"}, "source"},
	}
	for _, c := range cases {
		n := root
		for _, name := range c.path {
			var next *Node
			for _, ch := range n.Children {
				if ch.Name == name {
					next = ch
				}
			}
			if next == nil {
				t.Fatalf("%v: no child %q under %q", c.path, name, n.Name)
			}
			n = next
		}
		var got []string
		for _, ch := range n.Children {
			if ch.Type == NodeArgument {
				got = append(got, ch.Name)
			}
		}
		if !slices.Contains(got, c.want) || len(got) != 1 {
			t.Errorf("%v: argument children %v, want [%s]", c.path, got, c.want)
		}
	}
}
