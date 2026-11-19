package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: NbtTagArgument, CompoundTagArgument, SnbtGrammar
func TestCommandStrictSNBTValues(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")
	for _, tc := range []struct{ input, want string }{
		{"0x1Fs", "31s"},
		{"0b101", "5"},
		{"1_000_000", "1000000"},
		{"5ub", "5b"},
		{"200ub", "-56b"},
		{"-5sb", "-5b"},
		{"0xFFFFFFFF", "-1"},
		{`"\u00e9\x41\N{Snowman}"`, `"éA☃"`},
		{"bool(5)", "1b"},
		{`uuid("f81d4fae-7dec-11d0-a765-00a0c91e6bf6")`, "[I; -132296786, 2112623056, -1486552928, -920753162]"},
		{"TRUE", "1b"},
	} {
		c.Run(t, tc.input, func(t *testing.T) {
			// Reset the value so equal consecutive results still modify it.
			c.ChatCommand("data modify storage t:a v set value {}")
			c.ExpectCommandFeedback("commands.data.storage.modified", "t:a")
			c.ChatCommand("data modify storage t:a v set value " + tc.input)
			c.ExpectCommandFeedback("commands.data.storage.modified", "t:a")
			c.ChatCommand("data get storage t:a v")
			c.ExpectCommandFeedback("commands.data.storage.query", "t:a", tc.want)
		})
	}
	c.ChatCommand(`summon marker ~ ~ ~ {data:{h:0x10s,u:"\u00e9",b:bool(1),i:1_0}}`)
	feedback, ok := protocol.ReadText(protocol.NewReader(c.ExpectPlay(protocol.IDPlaySystemChat)))
	if !ok || feedback.Translate != "commands.summon.success" || len(feedback.With) != 1 || feedback.With[0].Translate != "entity.minecraft.marker" {
		t.Fatalf("summon feedback = %+v, want a summoned marker", feedback)
	}
	c.ChatCommand("data get entity @e[type=marker,limit=1] data")
	c.ExpectCommandFeedbackText("commands.data.entity.query", feedback.With[0], servertest.NBTArg(`{b: 1b, u: "é", h: 16s, i: 10}`))
	c.ChatCommand(`data merge storage t:a {merged:{h:0x10s,u:"\u00e9",b:bool(1),i:1_0}}`)
	c.ExpectCommandFeedback("commands.data.storage.modified", "t:a")
	c.ChatCommand("data get storage t:a merged")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:a", `{b: 1b, u: "é", h: 16s, i: 10}`)
	c.Disconnect()
}

func TestCommandStrictSNBTRefusals(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Testificate")
	for _, tc := range []struct {
		input  string
		key    string
		with   []string
		offset int
	}{
		{"128b", "snbt.parser.number_parse_failure", []string{`Value out of range. Value:"128" Radix:10`}, 4},
		{"2147483648", "argument.literal.incorrect", []string{"."}, 10},
		{"3.4028235E39f", "snbt.parser.infinity_not_allowed", nil, 13},
		{"01", "argument.literal.incorrect", []string{"."}, 2},
		{"[B;300]", "snbt.parser.number_parse_failure", []string{`Value out of range. Value:"300" Radix:10`}, 7},
		{"[B;true]", "argument.literal.incorrect", []string{"+"}, 3},
		{"[I;1b,2L]", "snbt.parser.invalid_array_element_type", nil, 9},
		{`{"":6}`, "argument.literal.incorrect", []string{"."}, 5},
		{"-0x10", "snbt.parser.expected_non_negative_number", nil, 5},
	} {
		for _, prefix := range []string{
			"data modify storage t:a v set value ",
			"data merge storage t:a {v:",
			"summon marker ~ ~ ~ {data:{v:",
		} {
			command := prefix + tc.input
			switch prefix {
			case "data merge storage t:a {v:":
				command += "}"
			case "summon marker ~ ~ ~ {data:{v:":
				command += "}}"
			}
			c.Run(t, command, func(t *testing.T) {
				c.ChatCommand(command)
				c.ExpectCommandError(servertest.CommandErrorMessageForTest(tc.key, tc.with...),
					servertest.CommandErrorContextForTest(command, len(prefix)+tc.offset))
			})
		}
	}
	c.Disconnect()
}
