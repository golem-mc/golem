package command

import (
	"slices"
	"strings"
	"testing"
)

// TestCompileFunctionRefusesWhatVanillaRefuses checks the function file
// reader: the lines it keeps, and the files whose leading slash, dangling
// continuation or unparsable command keep them out.
// vanilla: CommandFunction.fromLines
func TestCompileFunctionRefusesWhatVanillaRefuses(t *testing.T) {
	fn, err := compileFunction(ForLevel(PermLevelGamemasters), PermLevelGamemasters, "t:ok", []string{"# c", "", "  say \\", "  hi  ", "\tkill @e "})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	var got []string
	for _, l := range fn.lines {
		got = append(got, l.input)
	}
	if want := []string{"say hi", "kill @e"}; !slices.Equal(got, want) {
		t.Errorf("lines = %q, want %q", got, want)
	}

	for _, tc := range []struct {
		lines []string
		want  string
	}{
		{[]string{"/say hi"}, "did you mean 'say'?"},
		{[]string{"//say hi"}, "use '#' not '//'"},
		{[]string{"say \\"}, "line continuation at end of file"},
		{[]string{"say hi", "nope"}, "line 2"},
		{[]string{"op Alice"}, "line 1"},
	} {
		_, err := compileFunction(ForLevel(PermLevelGamemasters), PermLevelGamemasters, "t:bad", tc.lines)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("compile %q: error %v, want one containing %q", tc.lines, err, tc.want)
		}
	}
}

// TestCompileFunctionSpellsTheParseFailure checks the message a load
// failure carries: what went wrong, the position, and the ten characters
// before it, so the longer inputs owe an ellipsis.
// vanilla: CommandFunction.fromLines, CommandSyntaxException.getMessage
func TestCompileFunctionSpellsTheParseFailure(t *testing.T) {
	for _, tc := range []struct {
		lines []string
		want  string
	}{
		{[]string{"nope"}, "Whilst parsing command on line 1: Unknown or incomplete command. See below for error at position 0: <--[HERE]"},
		{[]string{"say"}, "Whilst parsing command on line 1: Unknown or incomplete command. See below for error at position 3: say<--[HERE]"},
		{[]string{"tp @s ~ ~ ~ oh"}, "Whilst parsing command on line 1: Incorrect argument for command at position 6: tp @s <--[HERE]"},
		{[]string{"time set daytime extra"}, "Whilst parsing command on line 1: Incorrect argument for command at position 17: ...t daytime <--[HERE]"},
		// A word behind the function's arguments is the unknown
		// argument the dispatcher's separator rules answer with, at the
		// word. (A line's trailing whitespace never reaches the parser:
		// fromLines trims each line first.)
		{[]string{"function t:one {v:1} trailing"}, "Whilst parsing command on line 1: Incorrect argument for command at position 21: ...one {v:1} <--[HERE]"},
		{[]string{"give @s diamon"}, "Whilst parsing command on line 1: Unknown item 'minecraft:diamon' at position 8: give @s <--[HERE]"},
		{[]string{"setblock 0 64 0 minecraft:nope"}, "Whilst parsing command on line 1: Unknown block type 'minecraft:nope' at position 16: ...ck 0 64 0 <--[HERE]"},
	} {
		_, err := compileFunction(ForLevel(PermLevelGamemasters), PermLevelGamemasters, "t:bad", tc.lines)
		if err == nil || err.Error() != tc.want {
			t.Errorf("compile %q: error %v, want %q", tc.lines[0], err, tc.want)
		}
	}
}

// infoSource is a Source that records what is logged through Info.
type infoSource struct {
	Source
	infos []string
}

func (s *infoSource) Info(msg string, _ ...any) { s.infos = append(s.infos, msg) }
