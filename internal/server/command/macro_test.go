package command

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
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

// TestMacroCacheAnswersItsRecentCalls checks that a call's instantiation
// answers the next call with the same values, and that the calls of eight
// different value sets keep only the last eight answers: the first
// instantiates again after a ninth.
// vanilla: MacroFunction.instantiate (instantiationCache)
func TestMacroCacheAnswersItsRecentCalls(t *testing.T) {
	m := macroFunctionOf(t, []string{"$say $(n)"})
	first := nbt.Compound{"n": nbt.String("first")}
	inst, failure := fnOf(m).instantiate(first)
	if failure != nil {
		t.Fatalf("instantiate: %+v", failure)
	}
	// The same values answer the same instantiation.
	again, _ := fnOf(m).instantiate(first)
	if again != inst {
		t.Errorf("a repeated call instantiated %v again, want it answered from the cache", again)
	}
	for i := range 9 {
		if _, fc := fnOf(m).instantiate(nbt.Compound{"n": nbt.String(fmt.Sprintf("other%d", i))}); fc != nil {
			t.Fatalf("instantiate other %d: %+v", i, fc)
		}
	}
	// vanilla: MacroFunction.MAX_CACHE_ENTRIES keeps eight.
	if len(m.cache) != 8 {
		t.Errorf("cache holds %d instantiations, want 8", len(m.cache))
	}
	// First's answer was behind the eight the other calls kept.
	again, _ = fnOf(m).instantiate(first)
	if again == inst {
		t.Errorf("an evicted call answered %v from the cache, want a fresh instantiation", again)
	}
	if _, failure := fnOf(m).instantiate(nbt.Compound{"n": nbt.String("other8")}); failure != nil {
		t.Errorf("the most recent call did not answer from the cache: %+v", failure)
	}
}

// TestARefusedInstantiationStillSpendsTheOldestCacheEntry checks the entry
// a failing call costs: the oldest answer leaves before the new one
// builds, so a call whose substituted line does not parse still spends
// it, and the entries behind it keep their answers.
// vanilla: MacroFunction.instantiate
func TestARefusedInstantiationStillSpendsTheOldestCacheEntry(t *testing.T) {
	m := macroFunctionOf(t, []string{"$say $(n)"})
	inst := make([]*Function, 0, 8)
	for i := range 8 {
		fn, failure := fnOf(m).instantiate(nbt.Compound{"n": nbt.String(fmt.Sprintf("v%d", i))})
		if failure != nil {
			t.Fatalf("instantiate v%d: %+v", i, failure)
		}
		inst = append(inst, fn)
	}
	_, failure := fnOf(m).instantiate(nbt.Compound{"n": nbt.String("x " + strings.Repeat("a", maxFunctionLine))})
	if failure == nil || !strings.Contains(failure.Text, "command too long") {
		t.Fatalf("the over-long call answered %+v, want the length refusal", failure)
	}
	spent, _ := fnOf(m).instantiate(nbt.Compound{"n": nbt.String("v0")})
	if spent == inst[0] {
		t.Errorf("the refused call kept v0's answer, want the oldest entry spent")
	}
	for i := 1; i < 8; i++ {
		still, _ := fnOf(m).instantiate(nbt.Compound{"n": nbt.String(fmt.Sprintf("v%d", i))})
		if still != inst[i] {
			t.Errorf("the refused call spent v%d's answer too, want only the oldest", i)
		}
	}
}

// TestInstantiateIsSafeToCallConcurrently checks the cache against the
// callers this server really has: a player's commands run on that
// player's own connection, the console and the game loop each on another
// goroutine, so the same macro can be instantiated by many at once.
func TestInstantiateIsSafeToCallConcurrently(t *testing.T) {
	m := macroFunctionOf(t, []string{"$say $(n)"})
	fn := fnOf(m)
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			inst, failure := fn.instantiate(nbt.Compound{"n": nbt.String(fmt.Sprintf("v%d", i%4))})
			if failure != nil || inst == nil {
				t.Errorf("concurrent call %d = (%v, %+v), want an instantiation", i, inst, failure)
			}
		}(i)
	}
	wg.Wait()
	if len(m.cache) > maxMacroCache {
		t.Errorf("cache holds %d instantiations, want at most %d", len(m.cache), maxMacroCache)
	}
}

// TestMacroDecimalMatchesVanilla checks the decimal every float and
// double substitutes as: the samples a DecimalFormat("#") run against a
// vanilla server answered, at its fifteen fraction digits.
// vanilla: MacroFunction.DECIMAL_FORMAT
func TestMacroDecimalMatchesVanilla(t *testing.T) {
	for _, tc := range []struct {
		value float64
		want  string
	}{
		{0.5, ".5"},
		{1.0, "1"},
		{0.001, ".001"},
		{1e-5, ".00001"},
		{0.1 + 0.2, ".3"},
		{0.85, ".85"},
		{-2.25, "-2.25"},
		{2.675, "2.675"},
		// A float widens to the double of its own digits.
		{float64(float32(0.1)), ".100000001490116"},
		{float64(float32(0.85)), ".850000023841858"},
		{1.0 / 3, ".333333333333333"},
		{1e20, "100000000000000000000"},
		// 2^130's conversion keeps one digit more than the shortest form
		// that reads it back.
		{1.361129467683754e39, "1361129467683753900000000000000000000000"},
		// The digits the conversion develops, not the exact decimal the
		// double stands for.
		{12345678901234567890, "12345678901234567000"},
		{-12345678901234567890, "-12345678901234567000"},
		{123456789.123456789123, "123456789.12345679"},
		// Half the last kept digit rounds to even.
		{1.0 / 65536, ".000015258789062"},
		{3.0 / 65536, ".000045776367188"},
		{5e-16, ".000000000000001"},
		{1.5e-15, ".000000000000001"},
		{2.5e-16, "0"},
		{4e-16, "0"},
		// A subnormal's two digits start far past the fifteen fraction
		// digits.
		{4.9e-324, "0"},
		// The zero of no digits keeps its sign.
		{0.0, "0"},
		{math.Copysign(0, -1), "-0"},
		{math.NaN(), "NaN"},
		{math.Inf(1), "∞"},
		{math.Inf(-1), "-∞"},
	} {
		if got := macroDecimal(tc.value); got != tc.want {
			t.Errorf("macroDecimal(%v) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

// TestNbtTypeNameSpellsTheTagTypesOwnNames checks the type names the
// not-a-compound refusal spells a tag by.
// vanilla: TagType.getName
func TestNbtTypeNameSpellsTheTagTypesOwnNames(t *testing.T) {
	for _, tc := range []struct {
		tag  nbt.Tag
		want string
	}{
		{nbt.Byte(1), "BYTE"},
		{nbt.Short(1), "SHORT"},
		{nbt.Int(1), "INT"},
		{nbt.Long(1), "LONG"},
		{nbt.Float(1), "FLOAT"},
		{nbt.Double(1), "DOUBLE"},
		{nbt.String("s"), "STRING"},
		{nbt.ByteArray{1}, "BYTE[]"},
		{nbt.IntArray{1}, "INT[]"},
		{nbt.LongArray{1}, "LONG[]"},
		{nbt.List{}, "LIST"},
		{nbt.Compound{}, "COMPOUND"},
	} {
		if got := nbtTypeName(tc.tag); got != tc.want {
			t.Errorf("nbtTypeName(%#v) = %q, want %q", tc.tag, got, tc.want)
		}
	}
}
