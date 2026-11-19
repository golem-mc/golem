package commands

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"strings"
	"testing"
)

// TestArgumentParseErrors sends the commands a vanilla server refuses
// while it reads their arguments, and expects the replies that server
// answered the same commands with: the key, the values its %s take, and the
// cursor the context line points at. Refusals the command's own run answers
// travel without a context line, the reading having stopped nowhere.
func TestArgumentParseErrors(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.SpawningTestConfig(), "Testificate", servertest.OpsListing(t, "Testificate"))

	for _, tc := range []struct {
		name    string
		command string
		key     string
		with    []string
		// cursor is where the "<--[HERE]" marker points; -1 for the refusals
		// that are no parse error and travel alone.
		cursor int
	}{
		{"unknown item", "give @s minecraft:nonexistent", "argument.item.id.invalid",
			[]string{"minecraft:nonexistent"},
			strings.Index("give @s minecraft:nonexistent", "minecraft:nonexistent")},
		{"count that is no integer", "give @s stone abc", "parsing.int.expected", nil,
			strings.Index("give @s stone abc", "abc")},
		{"unknown block", "setblock 0 64 0 notablock", "argument.block.id.invalid",
			[]string{"minecraft:notablock"},
			strings.Index("setblock 0 64 0 notablock", "notablock")},
		{"component that reads nowhere", "tellraw @s {text:", "snbt.parser.expected_unquoted_string", nil,
			len("tellraw @s {text:")},
		{"component with text behind it", `tellraw @s {text:"a"}x`, "command.expected.separator", nil,
			len(`tellraw @s {text:"a"}`)},
		{"component with spaces inside and text behind it", `tellraw @s {text:"a  b"}x`, "command.expected.separator", nil,
			len(`tellraw @s {text:"a  b"}`)},
		{"compound that reads nowhere", "summon minecraft:zombie ~ ~ ~ {bad", "argument.literal.incorrect",
			[]string{":"}, len("summon minecraft:zombie ~ ~ ~ {bad")},
		{"unterminated compound", "summon cow 0 70 0 {a:1", "argument.literal.incorrect",
			[]string{"."}, len("summon cow 0 70 0 {a:1")},
		{"compound ending at its comma", "summon cow 0 70 0 {a:1,", "argument.literal.incorrect",
			[]string{"\""}, len("summon cow 0 70 0 {a:1,")},
		{"compound with an open brace alone", "summon cow 0 70 0 {", "argument.literal.incorrect",
			[]string{"\""}, len("summon cow 0 70 0 {")},
		{"compound with a key and no value", "summon cow 0 70 0 {a:", "snbt.parser.expected_unquoted_string",
			nil, len("summon cow 0 70 0 {a:")},
		{"compound closed by a bracket", "summon cow 0 70 0 {a:1]", "argument.literal.incorrect",
			[]string{"."}, len("summon cow 0 70 0 {a:1")},
		{"compound with an open list", "summon cow 0 70 0 {a:[", "argument.literal.incorrect",
			[]string{"B"}, len("summon cow 0 70 0 {a:[")},
		{"compound with a second key and no colon", "summon cow 0 70 0 {a:1,b", "argument.literal.incorrect",
			[]string{":"}, len("summon cow 0 70 0 {a:1,b")},
		{"compound with no comma between entries", "summon cow 0 70 0 {a:1 b:2}", "argument.literal.incorrect",
			[]string{","}, len("summon cow 0 70 0 {a:1 b")},
		{"compound ending at a float suffix", "summon cow 0 70 0 {Health:1f", "argument.literal.incorrect",
			[]string{","}, len("summon cow 0 70 0 {Health:1f")},
		{"compound ending at a double suffix", "summon cow 0 70 0 {a:1d", "argument.literal.incorrect",
			[]string{","}, len("summon cow 0 70 0 {a:1d")},
		{"compound ending at a byte suffix", "summon cow 0 70 0 {a:1b", "argument.literal.incorrect",
			[]string{","}, len("summon cow 0 70 0 {a:1b")},
		{"compound ending at a long suffix", "summon cow 0 70 0 {a:1L", "argument.literal.incorrect",
			[]string{","}, len("summon cow 0 70 0 {a:1L")},
		{"compound ending at a short suffix", "summon cow 0 70 0 {a:1s", "argument.literal.incorrect",
			[]string{"b|B"}, len("summon cow 0 70 0 {a:1s")},
		{"compound ending at true", "summon cow 0 70 0 {a:true", "argument.literal.incorrect",
			[]string{"("}, len("summon cow 0 70 0 {a:true")},
		{"compound ending at false", "summon cow 0 70 0 {a:false", "argument.literal.incorrect",
			[]string{"("}, len("summon cow 0 70 0 {a:false")},
		{"compound ending at a closed list", "summon cow 0 70 0 {a:[1,2]", "argument.literal.incorrect",
			[]string{","}, len("summon cow 0 70 0 {a:[1,2]")},
		{"compound ending at a list string", "summon cow 0 70 0 {a:[\"x\"", "argument.literal.incorrect",
			[]string{","}, len("summon cow 0 70 0 {a:[\"x\"")},
		{"compound ending at a bare word", "summon cow 0 70 0 {a:b", "argument.literal.incorrect",
			[]string{"("}, len("summon cow 0 70 0 {a:b")},
		{"compound with a bare word and a second word", "summon cow 0 70 0 {a:b c", "argument.literal.incorrect",
			[]string{"("}, len("summon cow 0 70 0 {a:b ")},
		{"compound padded with whitespace", "summon cow 0 70 0 { a : 1", "argument.literal.incorrect",
			[]string{"."}, len("summon cow 0 70 0 { a : 1")},
		{"compound with a value and a stray word", "summon minecraft:zombie ~ ~ ~ {bad:1 x}", "argument.literal.incorrect",
			[]string{"."}, len("summon minecraft:zombie ~ ~ ~ {bad:1 ")},
		{"compound with an unclosed list", "summon minecraft:zombie ~ ~ ~ {a:[1,2}", "argument.literal.incorrect",
			[]string{"."}, len("summon minecraft:zombie ~ ~ ~ {a:[1,2")},
		{"compound with no value before its close", "summon minecraft:zombie ~ ~ ~ {bad:}", "snbt.parser.expected_unquoted_string",
			nil, len("summon minecraft:zombie ~ ~ ~ {bad:")},
		{"compound with an unterminated quoted key", "summon minecraft:zombie ~ ~ ~ {\"bad", "snbt.parser.invalid_string_contents",
			nil, len("summon minecraft:zombie ~ ~ ~ {\"bad")},
		{"compound with no key", "summon minecraft:zombie ~ ~ ~ {:1}", "argument.literal.incorrect",
			[]string{"\""}, len("summon minecraft:zombie ~ ~ ~ {")},
		{"compound of a lone comma", "summon minecraft:zombie ~ ~ ~ {,}", "argument.literal.incorrect",
			[]string{"\""}, len("summon minecraft:zombie ~ ~ ~ {")},
		{"compound with an unclosed inner compound", "summon minecraft:zombie ~ ~ ~ {a:{b:1}", "argument.literal.incorrect",
			[]string{","}, len("summon minecraft:zombie ~ ~ ~ {a:{b:1}")},
		{"compound with a float and a stray word", "summon minecraft:zombie ~ ~ ~ {Health:20f bad}", "argument.literal.incorrect",
			[]string{","}, len("summon minecraft:zombie ~ ~ ~ {Health:20f ")},
		{"compound with a bare word and a stray word", "summon minecraft:zombie ~ ~ ~ {a:b c}", "argument.literal.incorrect",
			[]string{"("}, len("summon minecraft:zombie ~ ~ ~ {a:b ")},
		{"compound with a text behind its close", "summon minecraft:zombie ~ ~ ~ {}x", "command.expected.separator",
			nil, len("summon minecraft:zombie ~ ~ ~ {}")},
		{"compound with a spaced word behind its close", "summon minecraft:zombie ~ ~ ~ {} x", "command.unknown.argument",
			nil, len("summon minecraft:zombie ~ ~ ~ {} ")},
		{"marker that is none", "time set abc", "commands.time.no_time_marker_found",
			[]string{"minecraft:abc", "minecraft:overworld"}, -1},
		// The set argument's minimum 0 refuses the word as a time, so the
		// marker branch takes it and refuses it as its own.
		{"set below the minimum", "time set -5", "commands.time.no_time_marker_found",
			[]string{"minecraft:-5", "minecraft:overworld"}, -1},
		{"set below the minimum with a unit", "time set -5t", "commands.time.no_time_marker_found",
			[]string{"minecraft:-5t", "minecraft:overworld"}, -1},
		{"marker that reads nowhere", "time set Noon", "command.unknown.argument", nil,
			strings.Index("time set Noon", "Noon")},
		{"namespaced marker that reads nowhere", "time set minecraft:Noon", "command.unknown.argument", nil,
			strings.Index("time set minecraft:Noon", "minecraft:Noon")},
		{"add that is no number", "time add noon", "parsing.float.expected", nil,
			strings.Index("time add noon", "noon")},
		{"add whose unit is none", "time add 1x", "argument.time.invalid_unit", nil,
			len("time add 1x")},
		{"add that is no number at all", "time add abc", "parsing.float.expected", nil,
			strings.Index("time add abc", "abc")},
		{"item that reads nowhere", "give @s Stone", "argument.item.id.invalid",
			[]string{"minecraft:"}, strings.Index("give @s Stone", "Stone")},
		{"item that ends at its namespace", "give @s minecraft:", "argument.item.id.invalid",
			[]string{"minecraft:"}, strings.Index("give @s minecraft:", "minecraft:")},
		{"item with a tail", "give @s nonexistent{a:1}", "argument.item.id.invalid",
			[]string{"minecraft:nonexistent"}, strings.Index("give @s nonexistent{a:1}", "nonexistent{a:1}")},
		{"item whose component list never closes", "give @s stone[", "parsing.expected",
			[]string{"]"}, len("give @s stone[")},
		// The item parser reads each component entry before the bracket it
		// was reading towards, so an unknown name wins over the missing one.
		{"item component that names none", "give @s stone[foo", "arguments.item.component.unknown",
			[]string{"minecraft:foo"}, strings.Index("give @s stone[foo", "foo")},
		{"item whose list ends on a comma", "give @s stone[minecraft:damage=1,", "arguments.item.component.expected",
			nil, len("give @s stone[minecraft:damage=1,")},
		{"item whose last component has no bracket", "give @s stone[minecraft:damage=1", "parsing.expected",
			[]string{"]"}, len("give @s stone[minecraft:damage=1")},
		{"item predicate tag that names none", "clear @s #nonexistent", "arguments.item.tag.unknown",
			[]string{"minecraft:nonexistent"}, len("clear @s #nonexistent")},
		{"block that reads nowhere", "setblock 0 64 0 Stone", "argument.block.id.invalid",
			[]string{"minecraft:"}, strings.Index("setblock 0 64 0 Stone", "Stone")},
		{"block with a tail", "setblock 0 64 0 notablock{a:1}", "argument.block.id.invalid",
			[]string{"minecraft:notablock"}, strings.Index("setblock 0 64 0 notablock{a:1}", "notablock{a:1}")},
		{"block tag where a state is asked", "setblock 0 64 0 #minecraft:logs", "argument.block.tag.disallowed",
			nil, strings.Index("setblock 0 64 0 #minecraft:logs", "#minecraft:logs")},
		{"block tag with properties", "setblock 0 64 0 #minecraft:logs[axis=y]", "argument.block.tag.disallowed",
			nil, strings.Index("setblock 0 64 0 #minecraft:logs[axis=y]", "#minecraft:logs")},
		{"fill block that is a tag", "fill 0 64 0 1 64 1 #minecraft:logs", "argument.block.tag.disallowed",
			nil, strings.Index("fill 0 64 0 1 64 1 #minecraft:logs", "#minecraft:logs")},
		{"block predicate that names none", "execute if block 0 64 0 notablock run say hi", "argument.block.id.invalid",
			[]string{"minecraft:notablock"}, strings.Index("execute if block 0 64 0 notablock run say hi", "notablock")},
		{"fill filter that names none", "fill 0 64 0 1 65 1 stone replace notablock", "argument.block.id.invalid",
			[]string{"minecraft:notablock"}, strings.Index("fill 0 64 0 1 65 1 stone replace notablock", "notablock")},
		{"block predicate tag that names none", "execute if block 0 64 0 #nonexistent run say hi", "arguments.block.tag.unknown",
			[]string{"minecraft:nonexistent"}, strings.Index("execute if block 0 64 0 #nonexistent run say hi", "#nonexistent")},
		{"fill filter tag that names none", "fill 0 64 0 1 65 1 stone replace #nonexistent", "arguments.block.tag.unknown",
			[]string{"minecraft:nonexistent"}, strings.Index("fill 0 64 0 1 65 1 stone replace #nonexistent", "#nonexistent")},
		{"clone filter tag that names none", "clone 0 64 0 1 64 0 2 64 2 filtered #nonexistent", "arguments.block.tag.unknown",
			[]string{"minecraft:nonexistent"}, strings.Index("clone 0 64 0 1 64 0 2 64 2 filtered #nonexistent", "#nonexistent")},
		// A tag's property list is read vaguely, so an unknown key is not
		// refused; only its shape is read, and its errors point into it.
		{"clone filter tag property list never closed",
			"clone 0 64 0 1 64 0 2 64 2 filtered #minecraft:stairs[facing=north",
			"argument.block.property.unclosed", nil,
			strings.Index("clone 0 64 0 1 64 0 2 64 2 filtered #minecraft:stairs[facing=north", "north")},
		{"clone filter tag property with no value",
			"clone 0 64 0 1 64 0 2 64 2 filtered #minecraft:stairs[facing]",
			"argument.block.property.novalue", []string{"facing", "minecraft:"},
			strings.Index("clone 0 64 0 1 64 0 2 64 2 filtered #minecraft:stairs[facing]", "facing")},
		{"clone filter tag property set twice",
			"clone 0 64 0 1 64 0 2 64 2 filtered #minecraft:stairs[facing=north,facing=south]",
			"argument.block.property.duplicate", []string{"facing", "minecraft:"},
			strings.LastIndex("clone 0 64 0 1 64 0 2 64 2 filtered #minecraft:stairs[facing=north,facing=south]", "facing")},
		{"block property that is unknown", "setblock 0 64 0 stone[foo=bar]", "argument.block.property.unknown",
			[]string{"minecraft:stone", "foo"},
			strings.Index("setblock 0 64 0 stone[foo=bar]", "foo")},
		{"block property with an unknown value", "setblock 0 64 0 oak_log[axis=q]", "argument.block.property.invalid",
			[]string{"minecraft:oak_log", "q", "axis"},
			strings.Index("setblock 0 64 0 oak_log[axis=q]", "=q") + 1},
		{"block property with an empty value", "setblock 0 64 0 oak_log[axis=]", "argument.block.property.invalid",
			[]string{"minecraft:oak_log", "", "axis"},
			strings.Index("setblock 0 64 0 oak_log[axis=]", "]")},
		{"block property that is empty", "setblock 0 64 0 oak_log[=y]", "argument.block.property.unknown",
			[]string{"minecraft:oak_log", ""},
			strings.Index("setblock 0 64 0 oak_log[=y]", "=")},
		{"block property set twice", "setblock 0 64 0 oak_log[axis=y,axis=x]", "argument.block.property.duplicate",
			[]string{"axis", "minecraft:oak_log"},
			strings.LastIndex("setblock 0 64 0 oak_log[axis=y,axis=x]", "axis")},
		{"block property with no value", "setblock 0 64 0 oak_log[axis]", "argument.block.property.novalue",
			[]string{"axis", "minecraft:oak_log"},
			strings.Index("setblock 0 64 0 oak_log[axis]", "]")},
		{"block property whose value runs on", "setblock 0 64 0 oak_log[axis=y!]", "argument.block.property.unclosed",
			nil, strings.Index("setblock 0 64 0 oak_log[axis=y!]", "!")},
		{"block property with a quoted value", "setblock 0 64 0 oak_log[axis=\"q\"]", "argument.block.property.invalid",
			[]string{"minecraft:oak_log", "q", "axis"},
			strings.Index("setblock 0 64 0 oak_log[axis=\"q\"]", "\"")},
		{"block property list never closed", "setblock 0 64 0 oak_log[axis=y", "argument.block.property.unclosed",
			nil, len("setblock 0 64 0 oak_log[axis=y")},
		{"block entity tag that reads nowhere", "setblock 0 64 0 stone{foo", "argument.literal.incorrect",
			[]string{":"}, len("setblock 0 64 0 stone{foo")},
		{"clear item that names none", "clear @s nonexistent", "argument.item.id.invalid",
			[]string{"minecraft:nonexistent"}, len("clear @s nonexistent")},
		{"clear predicate that reads nowhere", "clear @s Stone", "argument.id.invalid",
			nil, strings.Index("clear @s Stone", "Stone")},
		{"clear predicate whose bracket never closes", "clear @s stone[", "argument.id.invalid",
			nil, len("clear @s stone[")},
		{"clear predicate left with a tail", "clear @s stone[damage]x", "command.expected.separator",
			nil, strings.Index("clear @s stone[damage]x", "x")},
		{"count below the minimum", "give @s stone 0", "argument.integer.low",
			[]string{"1", "0"}, strings.Index("give @s stone 0", "0")},
		{"count below zero", "clear @s stone -1", "argument.integer.low",
			[]string{"0", "-1"}, strings.Index("clear @s stone -1", "-1")},
		{"rule below its minimum", "gamerule random_tick_speed -1", "argument.integer.low",
			[]string{"0", "-1"}, strings.Index("gamerule random_tick_speed -1", "-1")},
		{"reward below zero", "xp set @s -1", "argument.integer.low",
			[]string{"0", "-1"}, strings.Index("xp set @s -1", "-1")},
		{"rule that is no bool", "gamerule keep_inventory maybe", "parsing.bool.invalid",
			[]string{"maybe"}, strings.Index("gamerule keep_inventory maybe", "maybe")},
		{"rule spelled wrongly", "gamerule keep_inventory TRUE", "parsing.bool.invalid",
			[]string{"TRUE"}, strings.Index("gamerule keep_inventory TRUE", "TRUE")},
		{"rule that reads on", "gamerule keep_inventory true!", "command.expected.separator",
			nil, strings.Index("gamerule keep_inventory true!", "!")},
		{"rule whose tail is no bool", "gamerule keep_inventory maybe!", "parsing.bool.invalid",
			[]string{"maybe"}, strings.Index("gamerule keep_inventory maybe!", "maybe")},
		{"add with a tail", "time add 1d!", "command.expected.separator",
			nil, strings.Index("time add 1d!", "!")},
		{"add whose unit runs on", "time add 5t,", "command.expected.separator",
			nil, strings.Index("time add 5t,", ",")},
		{"duration with a tail", "weather rain 5s!", "command.expected.separator",
			nil, strings.Index("weather rain 5s!", "!")},
		// The duration argument's own minimum, refused where its reading
		// ended, at the end of the word.
		{"duration below its minimum", "weather rain 0", "argument.time.tick_count_too_low",
			[]string{"1", "0"}, len("weather rain 0")},
		{"unit that takes the digit", "time add 1d5", "argument.time.invalid_unit",
			nil, len("time add 1d5")},
		{"unit that takes the dot", "time add 1d.", "argument.time.invalid_unit",
			nil, len("time add 1d.")},
		{"unit that takes the underscore", "time add 1t_", "argument.time.invalid_unit",
			nil, len("time add 1t_")},
		{"unit that takes the plus", "time add 1s+", "argument.time.invalid_unit",
			nil, len("time add 1s+")},
		{"duration unit that takes the minus", "weather rain 5s-", "argument.time.invalid_unit",
			nil, len("weather rain 5s-")},
		{"unit stopped short of its tail", "time add 5x!", "argument.time.invalid_unit",
			nil, strings.Index("time add 5x!", "!")},
		{"two-letter unit stopped short of its tail", "time add 5dx!", "argument.time.invalid_unit",
			nil, strings.Index("time add 5dx!", "!")},
		{"rule whose value is an empty string", `gamerule keep_inventory ""`, "parsing.bool.expected",
			nil, len(`gamerule keep_inventory ""`)},
		{"rule whose value is no string", `gamerule keep_inventory !`, "parsing.bool.expected",
			nil, strings.Index(`gamerule keep_inventory !`, "!")},
		{"rule whose quote never closes", `gamerule keep_inventory "true`, "parsing.quote.expected.end",
			nil, len(`gamerule keep_inventory "true`)},
		{"rule whose escape is none", `gamerule keep_inventory "tr\ue"`, "parsing.quote.escape",
			[]string{"u"}, strings.Index(`gamerule keep_inventory "tr\ue"`, "ue")},
		{"rule whose quote is escaped", `gamerule keep_inventory "tr\"ue"`, "parsing.bool.invalid",
			[]string{`tr"ue`}, strings.Index(`gamerule keep_inventory "tr\"ue"`, `"tr\"ue"`)},
		{"rule whose quote holds a space", `gamerule keep_inventory "tr ue"`, "parsing.bool.invalid",
			[]string{"tr ue"}, strings.Index(`gamerule keep_inventory "tr ue"`, `"tr`)},
		{"rule whose quote holds two words", `gamerule keep_inventory "true false"`, "parsing.bool.invalid",
			[]string{"true false"}, strings.Index(`gamerule keep_inventory "true false"`, `"true`)},
		{"rule whose quote opens on a space", `gamerule keep_inventory " true"`, "parsing.bool.invalid",
			[]string{" true"}, strings.Index(`gamerule keep_inventory " true"`, `"`)},
		{"rule whose quote closes on a space", `gamerule keep_inventory "true "`, "parsing.bool.invalid",
			[]string{"true "}, strings.Index(`gamerule keep_inventory "true "`, `"true`)},
		{"rule whose quote swallows the next word", `gamerule keep_inventory "true x`, "parsing.quote.expected.end",
			nil, len(`gamerule keep_inventory "true x`)},
		{"effect particles whose quote holds a space", `effect give @s speed 5 1 "tr ue"`, "parsing.bool.invalid",
			[]string{"tr ue"}, strings.Index(`effect give @s speed 5 1 "tr ue"`, `"tr`)},
		{"effect duration below its minimum", "effect give @s speed 0", "argument.integer.low",
			[]string{"1", "0"}, strings.Index("effect give @s speed 0", "0")},
		{"effect duration above its maximum", "effect give @s speed 1000001", "argument.integer.big",
			[]string{"1000000", "1000001"}, strings.Index("effect give @s speed 1000001", "1000001")},
		{"effect amplifier above its maximum", "effect give @s speed 5 256", "argument.integer.big",
			[]string{"255", "256"}, strings.Index("effect give @s speed 5 256", "256")},
		{"effect amplifier below its minimum", "effect give @s speed 5 -1", "argument.integer.low",
			[]string{"0", "-1"}, strings.Index("effect give @s speed 5 -1", "-1")},
		// Identifier.read stops at the first character the location's
		// alphabet does not cover, so the name read before it is what the
		// registry lookup refuses, and the cursor stays on that character.
		{"resource location that reads nowhere", "summon Day", "argument.resource.not_found",
			[]string{"minecraft:", "minecraft:entity_type"}, strings.Index("summon Day", "Day")},
		{"namespaced location whose path reads nowhere", "summon minecraft:Day", "argument.resource.not_found",
			[]string{"minecraft:", "minecraft:entity_type"}, strings.Index("summon minecraft:Day", "Day")},
		{"summon name with no namespace", "summon Pig", "argument.resource.not_found",
			[]string{"minecraft:", "minecraft:entity_type"}, strings.Index("summon Pig", "Pig")},
		{"timeline that ends at its namespace", "time query day:", "argument.resource.not_found",
			[]string{"day:", "minecraft:timeline"}, len("time query day:")},
		// A location with a tail is read up to it, and the tail is the
		// dispatcher's missing separator.
		{"timeline with a tail", "time query day#x", "command.expected.separator",
			nil, strings.Index("time query day#x", "#")},
		{"summon name with a tail", "summon pig#x", "command.expected.separator",
			nil, strings.Index("summon pig#x", "#")},
		// The registry lookup and the dispatcher's separator are read before
		// the command's own summonable check, which the tailed word never
		// reaches.
		{"summonable name with a tail", "summon player#x", "command.expected.separator",
			nil, strings.Index("summon player#x", "#")},
		// The summonable check is vanilla's run-time getSummonableEntityType,
		// so its refusal travels without the context line.
		{"summonable name", "summon player", "entity.not_summonable",
			[]string{"minecraft:player"}, -1},
		// A dimension is resolved at run time by the vanilla argument's
		// getDimension, so its refusal carries no input either.
		{"unknown dimension", "execute in minecraft:foo run seed", "argument.dimension.invalid",
			[]string{"minecraft:foo"}, -1},
		// A run-time refusal stands behind any parse error of the same command.
		{"dimension then an unknown command", "execute in minecraft:foo run bogus", "command.unknown.argument", nil,
			strings.Index("execute in minecraft:foo run bogus", "bogus")},
		{"dimension then an incomplete command", "execute in minecraft:foo", "command.unknown.command", nil,
			len("execute in minecraft:foo")},
		{"summonable name then a bad position", "summon player ~ ~ bogus", "parsing.double.expected", nil,
			strings.Index("summon player ~ ~ bogus", "bogus")},
		{"summonable name then a word for a position", "summon player foo", "parsing.double.expected", nil,
			strings.Index("summon player foo", "foo")},
		{"summonable name then an incomplete position", "summon player ~ ~", "argument.pos3d.incomplete", nil,
			strings.Index("summon player ~ ~", "~ ~")},
		// EntityAnchorArgument.parse winds its reader back to the word's start
		// before it throws, so the context window opens there.
		{"anchor that is none", "execute anchored foo run seed", "argument.anchor.invalid",
			[]string{"foo"}, strings.Index("execute anchored foo run seed", "foo")},
		// The read takes the whole word, but its path holds a second colon,
		// so the location parses nowhere and the invalid-id refusal carries
		// no arguments, its cursor where the read began.
		{"identifier that parses nowhere", "summon minecraft:pig:extra", "argument.id.invalid",
			nil, strings.Index("summon minecraft:pig:extra", "minecraft")},
		// A function name whose identifier parses nowhere is refused with
		// the cursor back at the read's start, after the hash a tag opens
		// with.
		{"function name that parses nowhere", "execute if function a/b:c", "argument.id.invalid",
			nil, strings.Index("execute if function a/b:c", "a/b:c")},
		{"function tag that parses nowhere", "execute if function #a/b:c", "argument.id.invalid",
			nil, strings.Index("execute if function #a/b:c", "a/b:c")},
		// The function argument reads its location like any other, so a
		// word only part of which reads is the dispatcher's missing
		// separator, one past the hash a tag opens with.
		{"function name that reads partly", "execute if function Foo", "command.expected.separator",
			nil, strings.Index("execute if function Foo", "Foo")},
		{"function name with a tail", "execute if function foo#x", "command.expected.separator",
			nil, strings.Index("execute if function foo#x", "#x")},
		{"function tag with a tail", "execute if function #foo#x", "command.expected.separator",
			nil, strings.Index("execute if function #foo#x", "#x")},
		// The dispatcher's separator rules read behind a read that
		// stopped where its last word did: input left unread behind it
		// is an unknown argument there, whether word or whitespace.
		{"function arguments with a word behind", "function t:one {v:1} trailing", "command.unknown.argument", nil,
			strings.Index("function t:one {v:1} trailing", "trailing")},
		{"function arguments with a space behind", "function t:one {v:1} ", "command.unknown.argument", nil,
			len("function t:one {v:1}")},
		{"summon compound with a word behind", "summon minecraft:cow 0 70 0 {NoAI:1b} extra", "command.unknown.argument", nil,
			strings.Index("summon minecraft:cow 0 70 0 {NoAI:1b} extra", "extra")},
		{"data compound with a word behind", "data merge storage t:o {v:1} extra", "command.unknown.argument", nil,
			strings.Index("data merge storage t:o {v:1} extra", "extra")},
		{"time set with a space behind", "time set daytime ", "command.unknown.argument", nil,
			len("time set daytime")},
		{"bare say with a space behind", "say ", "command.unknown.argument", nil,
			len("say")},
		// One trailing space behind a redirecting word is the separator
		// its continuation skips, so the input ends with no command
		// after it: the dispatcher's unknown command at the input's end.
		{"execute as with a space behind", "execute as @a ", "command.unknown.command", nil,
			len("execute as @a ")},
		{"execute run with a space behind", "execute as @a run ", "command.unknown.command", nil,
			len("execute as @a run ")},
	} {
		// The cases share one server and one player and run in turn: a
		// server each, all in parallel, starve the dials at GOMAXPROCS 1.
		c.Run(t, tc.name, func(t *testing.T) {
			c.ChatCommand(tc.command)
			if tc.cursor < 0 {
				// A refusal of the command's own run is the wrapped
				// message alone, the way every command failure is sent.
				c.ExpectCommandFailure(servertest.CommandErrorMessageForTest(tc.key, tc.with...))
			} else {
				c.ExpectCommandError(
					servertest.CommandErrorMessageForTest(tc.key, tc.with...),
					servertest.CommandErrorContextForTest(tc.command, tc.cursor))
			}
			// Nothing follows the reply, so the next case reads its own.
			expectNoSystemChat(t, c)
		})
	}
	c.Disconnect()
}
