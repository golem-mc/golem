package command

import (
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// macroFunctionOf compiles the lines and answers the macro the file
// turned into.
func macroFunctionOf(t *testing.T, lines []string) *macroFunction {
	t.Helper()
	fn, err := compileFunction(PermLevelGamemasters, "t:m", lines)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if fn.macro == nil {
		t.Fatalf("compile kept a plain function, want a macro")
	}
	return fn.macro
}

// TestMacroTemplateReadsTheFile checks the '$' lines a compile reads: the
// plain lines kept in order before and after one, each template's
// literals and the arguments its variables take by first use, and the
// bodies vanilla refuses a file for.
// vanilla: FunctionBuilder, StringTemplate.fromString
func TestMacroTemplateReadsTheFile(t *testing.T) {
	m := macroFunctionOf(t, []string{
		"say first",
		"$say value:$(v) name:$(n)",
		"$summon $(t) ~ ~ ~ {CustomName:'\"$(n)\"'}",
		"$say $(v) $(v) $(n)",
		"say last",
	})
	if got := m.args; !slices.Equal(got, []string{"v", "n", "t"}) {
		t.Errorf("macro arguments = %q, want %q", got, []string{"v", "n", "t"})
	}
	if len(m.entries) != 5 {
		t.Fatalf("entries = %d, want two plain and three macro", len(m.entries))
	}
	if !m.entries[0].plain || m.entries[0].line.input != "say first" ||
		!m.entries[4].plain || m.entries[4].line.input != "say last" {
		t.Errorf("plain entries misplaced: %+v, %+v", m.entries[0], m.entries[4])
	}
	// The last template's variable reads v twice: one literal on each side.
	last := m.entries[3].template
	if !slices.Equal(last.args, []int{0, 0, 1}) {
		t.Errorf("template argument indices = %v, want [0 0 1]", last.args)
	}
	if !slices.Equal(last.segments, []string{"say ", " ", " "}) {
		t.Errorf("template segments = %q, want the literals between and around", last.segments)
	}
	if got, want := last.substitute([]string{".5", "x"}), "say .5 .5 x"; got != want {
		t.Errorf("substitute = %q, want %q", got, want)
	}
	// The empty name is a variable vanilla takes, among the others.
	m = macroFunctionOf(t, []string{"$say empty:$() held:$(v)"})
	if got := m.args; !slices.Equal(got, []string{"", "v"}) {
		t.Errorf("empty name = %q, want it among the arguments", got)
	}

	for _, tc := range []struct {
		line string
		want string
	}{
		{"$say hi", "Can't parse function line 1: 'say hi': no variables in macro"},
		{"$say value:$$ hi", "Can't parse function line 1: 'say value:$$ hi': no variables in macro"},
		{"$say value:$(v", "Can't parse function line 1: 'say value:$(v': unterminated macro variable"},
		{"$say $(a-b)", "Can't parse function line 1: 'say $(a-b)': invalid macro variable name 'a-b'"},
		{"$say $(a b)", "Can't parse function line 1: 'say $(a b)': invalid macro variable name 'a b'"},
	} {
		_, err := compileFunction(PermLevelGamemasters, "t:bad", []string{tc.line})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("compile %q: error %v, want one containing %q", tc.line, err, tc.want)
		}
	}

	// A plain file stays plain: no macro, its lines kept.
	fn, err := compileFunction(PermLevelGamemasters, "t:plain", []string{"say hi"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if fn.macro != nil || len(fn.lines) != 1 {
		t.Errorf("plain file compiled to macro %v with lines %v", fn.macro, fn.lines)
	}
}

// TestInstantiateMacroSubstitutesAndMatches checks the function a call
// runs: each type's own substitution text, the plain lines kept in order
// around the substituted ones, and the refusals that name the missing
// arguments a call left out.
// vanilla: MacroFunction.instantiate, MacroFunction.stringify
func TestInstantiateMacroSubstitutesAndMatches(t *testing.T) {
	m := macroFunctionOf(t, []string{
		"say first",
		"$say by:$(by) sh:$(sh) i:$(i) l:$(l) f:$(f) fl:$(fl) d:$(d) s:$(s) c:$(c)",
		"$data merge storage t:o {v:$(d)}",
		"say last",
	})
	arguments := nbt.Compound{
		"by": nbt.Byte(3), "sh": nbt.Short(-5), "i": nbt.Int(7),
		"l": nbt.Long(9223372036854775807), "f": nbt.Double(2.675),
		"fl": nbt.Float(0.85), "d": nbt.Double(0.5), "s": nbt.String("hello"),
		"c": nbt.Compound{"x": nbt.Int(1)},
	}
	fn, failure := fnOf(m).instantiate(arguments)
	if failure != nil {
		t.Fatalf("instantiate failed: %+v", failure)
	}
	want := []string{
		"say first",
		"say by:3 sh:-5 i:7 l:9223372036854775807 f:2.675 fl:.850000023841858 d:.5 s:hello c:{x:1}",
		"data merge storage t:o {v:.5}",
		"say last",
	}
	var got []string
	for _, line := range fn.lines {
		got = append(got, line.input)
	}
	if !slices.Equal(got, want) {
		t.Errorf("instantiated lines = %q, want %q", got, want)
	}
	for _, line := range fn.lines {
		if !slices.Equal(line.words, strings.Split(line.input, " ")) {
			t.Errorf("line %q keeps words %q", line.input, line.words)
		}
	}
	// The instantiated function keeps the file's id, which the result
	// line names. vanilla: queueFunctions decorateOutputIfNeeded
	if fn.ID != "t:m" {
		t.Errorf("instantiated id = %q, want t:m", fn.ID)
	}

	// Without a with clause the call refuses to name what it had none of.
	if fn, failure := fnOf(m).instantiate(nil); fn != nil || failure == nil ||
		failure.Translate != functionMissingArgumentsKey {
		t.Errorf("no arguments = (%v, %+v), want the missing-arguments refusal", fn, failure)
	}
	// An empty compound misses every name, the first in order.
	if fn, failure := fnOf(m).instantiate(nbt.Compound{}); fn != nil || failure == nil ||
		failure.Translate != functionMissingArgumentKey ||
		len(failure.With) != 2 || failure.With[0].Text != "t:m" || failure.With[1].Text != "by" {
		t.Errorf("empty compound = (%v, %+v), want by missing of t:m", fn, failure)
	}
	// One kept name leaves the next missing.
	if fn, failure := fnOf(m).instantiate(nbt.Compound{"by": nbt.Byte(1)}); fn != nil || failure == nil ||
		failure.With[1].Text != "sh" {
		t.Errorf("one name = (%v, %+v), want sh missing", fn, failure)
	}

	// A plain function answers itself, whatever arguments it was handed.
	plain, err := compileFunction(PermLevelGamemasters, "t:plain", []string{"say hi"})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if fn, failure := plain.instantiate(nil); fn != plain || failure != nil {
		t.Errorf("plain instantiate = (%v, %v), want the function itself", fn, failure)
	}
	if fn, failure := plain.instantiate(arguments); fn != plain || failure != nil {
		t.Errorf("plain instantiate with arguments = (%v, %v), want the function itself", fn, failure)
	}
}

// fnOf answers the macro's function, shared by the instantiation tests.
func fnOf(m *macroFunction) *Function {
	return &Function{ID: m.id, macro: m, level: PermLevelGamemasters}
}

// TestInstantiateMacroRefusesParsingFailuresByCommand checks a
// substituted line that does not parse: the message names the function,
// the text and the read the tree refused.
// vanilla: MacroFunction.MacroEntry.instantiate
func TestInstantiateMacroRefusesParsingFailuresByCommand(t *testing.T) {
	m := macroFunctionOf(t, []string{"$e_$(v)"})
	fn, failure := fnOf(m).instantiate(nbt.Compound{"v": nbt.String("x")})
	if fn != nil || failure == nil || failure.Translate != functionParseKey {
		t.Fatalf("unparsable line = (%v, %+v), want a parse failure", fn, failure)
	}
	if len(failure.With) != 3 || failure.With[0].Text != "t:m" ||
		failure.With[1].Text != "e_x" ||
		failure.With[2].Text != "Unknown or incomplete command. See below for error at position 0: <--[HERE]" {
		t.Errorf("parse refusal = %+v, want t:m, e_x and the read failure", failure.With)
	}

	// The values a line substitutes spell the refusal the dispatcher's
	// own wording does: an empty value behind a word is the command it
	// never finished, one behind its arguments the wrong argument.
	m = macroFunctionOf(t, []string{"$say$(x)"})
	fn, failure = fnOf(m).instantiate(nbt.Compound{"x": nbt.String("")})
	if fn != nil || failure == nil || failure.Translate != functionParseKey {
		t.Fatalf("empty rest = (%v, %+v), want a parse failure", fn, failure)
	}
	if len(failure.With) != 3 || failure.With[1].Text != "say" || failure.With[2].Text !=
		"Unknown or incomplete command. See below for error at position 3: say<--[HERE]" {
		t.Errorf("empty rest = %+v, want vanilla's wording at position 3", failure.With)
	}
	m = macroFunctionOf(t, []string{"$time set $(x)"})
	fn, failure = fnOf(m).instantiate(nbt.Compound{"x": nbt.String("")})
	if fn != nil || failure == nil || failure.Translate != functionParseKey {
		t.Fatalf("empty argument = (%v, %+v), want a parse failure", fn, failure)
	}
	if len(failure.With) != 3 || failure.With[1].Text != "time set " || failure.With[2].Text !=
		"Incorrect argument for command at position 8: time set<--[HERE]" {
		t.Errorf("empty argument = %+v, want vanilla's wording at position 8", failure.With)
	}

	// A message value keeps its trailing space, which the greedy read
	// runs with: the instantiation takes the whole text it was given.
	m = macroFunctionOf(t, []string{"$say $(v)"})
	if fn, failure := fnOf(m).instantiate(nbt.Compound{"v": nbt.String("hi ")}); fn == nil || failure != nil {
		t.Errorf("message value with its trailing space = (%v, %+v), want it instantiated", fn, failure)
	}

	// A value longer than a function line refuses before it is parsed.
	m = macroFunctionOf(t, []string{"$say $(v)"})
	tooLong := strings.Repeat("a", maxFunctionLine+1)
	fn, failure = fnOf(m).instantiate(nbt.Compound{"v": nbt.String(tooLong)})
	if fn != nil || failure == nil || !strings.Contains(failure.Text, "command too long") {
		t.Errorf("long line = (%v, %+v), want the line-length refusal", fn, failure)
	}
}
