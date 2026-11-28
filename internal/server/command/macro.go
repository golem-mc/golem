package command

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode"
	"unicode/utf16"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// The translation keys a macro function's instantiation refuses with.
// vanilla: MacroFunction, FunctionCommand
const (
	functionInstantiationKey    = "commands.function.instantiationFailure"
	functionMissingArgumentsKey = "commands.function.error.missing_arguments"
	functionMissingArgumentKey  = "commands.function.error.missing_argument"
	functionParseKey            = "commands.function.error.parse"
	functionNotCompoundKey      = "commands.function.error.argument_not_compound"
)

// macroFunction is a function file with a '$' line: its entries in file
// order — the plain lines matched while the file loaded, then each '$'
// line's template — the names its calls resolve in their order of first
// use, and the instantiations its last calls produced. Every connection
// runs its own commands, as do the console and the game loop, so the
// cache is guarded by its own lock.
// vanilla: FunctionBuilder, MacroFunction
type macroFunction struct {
	mu      sync.Mutex
	id      string
	entries []macroEntry
	args    []string
	cache   []macroInstantiation
}

// macroEntry is one line of a macro function: a plain command, or the
// template of a '$' line. Only a plain line reads line.
type macroEntry struct {
	plain    bool
	line     functionLine
	template macroTemplate
}

// macroTemplate is a '$' line's body: the literals between its $(name)
// variables and the function's argument each variable reads, by index.
// vanilla: StringTemplate
type macroTemplate struct {
	segments []string
	args     []int
}

// macroInstantiation is one call's answer and the values that produced it.
type macroInstantiation struct {
	values []string
	fn     *Function
}

// maxMacroCache is the instantiations a macro function keeps of its last
// calls, the oldest dropped. vanilla: MacroFunction.MAX_CACHE_ENTRIES
const maxMacroCache = 8

// beginMacro turns the file into its macro form — nothing to move when it
// already is one — keeping the lines compiled so far in order.
// vanilla: FunctionBuilder.addMacro
func (fn *Function) beginMacro() {
	if fn.macro != nil {
		return
	}
	m := &macroFunction{id: fn.ID}
	for _, line := range fn.lines {
		m.entries = append(m.entries, macroEntry{plain: true, line: line})
	}
	fn.lines, fn.macro = nil, m
}

// addLine appends a compiled command line: among the macro's entries once
// the file has a '$' line, else among the function's own.
// vanilla: FunctionBuilder.addCommand
func (fn *Function) addLine(line functionLine) {
	if m := fn.macro; m != nil {
		m.entries = append(m.entries, macroEntry{plain: true, line: line})
		return
	}
	fn.lines = append(fn.lines, line)
}

// macroArgument is the index a macro variable's name takes among the
// function's arguments, its first use adding one.
// vanilla: FunctionBuilder.getArgumentIndex
func (m *macroFunction) macroArgument(name string) int {
	if i := slices.Index(m.args, name); i >= 0 {
		return i
	}
	m.args = append(m.args, name)
	return len(m.args) - 1
}

// parseMacroTemplate reads a '$' line's body: the literals between its
// $(name) variables and the index of the function's argument each name
// takes, resolved through index. A '$' that names no variable does not
// read as one. vanilla: StringTemplate.fromString
func parseMacroTemplate(body string, index func(name string) int) (macroTemplate, error) {
	var t macroTemplate
	start, at := 0, 0
	for {
		dollar := strings.IndexByte(body[at:], '$')
		if dollar < 0 {
			break
		}
		dollar += at
		if dollar+1 == len(body) || body[dollar+1] != '(' {
			at = dollar + 1
			continue
		}
		t.segments = append(t.segments, body[start:dollar])
		end := strings.IndexByte(body[dollar+1:], ')')
		if end < 0 {
			return t, errors.New("unterminated macro variable")
		}
		end += dollar + 1
		name := body[dollar+2 : end]
		if !validMacroName(name) {
			return t, fmt.Errorf("invalid macro variable name '%s'", name)
		}
		t.args = append(t.args, index(name))
		start, at = end+1, end+1
	}
	if start == 0 {
		return t, errors.New("no variables in macro")
	}
	if start != len(body) {
		t.segments = append(t.segments, body[start:])
	}
	return t, nil
}

// validMacroName reports whether a macro variable's name is one letter or
// digit or underscore at a time — the empty name among them — reading at
// Java's UTF-16 units, so a letter beyond the BMP does not pass.
// vanilla: StringTemplate.isValidVariableName
func validMacroName(name string) bool {
	for _, unit := range utf16.Encode([]rune(name)) {
		if !unicode.IsLetter(rune(unit)) && !unicode.IsDigit(rune(unit)) && unit != '_' {
			return false
		}
	}
	return true
}

// substitute splices the values a call resolved into the line's literals.
// vanilla: StringTemplate.substitute
func (t macroTemplate) substitute(values []string) string {
	var b strings.Builder
	for i, arg := range t.args {
		b.WriteString(t.segments[i])
		b.WriteString(values[arg])
	}
	if len(t.segments) > len(t.args) {
		b.WriteString(t.segments[len(t.args)])
	}
	return b.String()
}

// stringifyTag is the text an argument's value substitutes into a line
// as: a float or a double at its own decimal, a byte, short or long in
// decimal without its suffix, a string as its own text, anything else as
// its SNBT. vanilla: MacroFunction.stringify
func stringifyTag(tag nbt.Tag) string {
	switch v := tag.(type) {
	case nbt.Float:
		return macroDecimal(float64(v))
	case nbt.Double:
		return macroDecimal(float64(v))
	case nbt.Byte:
		return strconv.FormatInt(int64(v), 10)
	case nbt.Short:
		return strconv.FormatInt(int64(v), 10)
	case nbt.Long:
		return strconv.FormatInt(int64(v), 10)
	case nbt.String:
		return string(v)
	}
	return nbt.Stringify(tag)
}

// macroDecimal writes a float or double in DecimalFormat("#")'s fifteen
// fraction digits: the digits a double's binary-to-decimal conversion
// develops (not the exact decimal it stands for) rounded at the fifteenth
// fraction digit. A tie at the conversion's last digit is settled by what
// that conversion did — half to even when its digits are the value's
// exact decimal, up only when it has not rounded up itself.
// The leading zero of a fraction and the zeros after a whole number are
// dropped. A float widens to a double first, so its widened digits read.
// vanilla: MacroFunction.DECIMAL_FORMAT, java.text.DigitList.round
func macroDecimal(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "∞"
	case math.IsInf(v, -1):
		return "-∞"
	}
	sign := ""
	if math.Signbit(v) {
		sign, v = "-", math.Abs(v)
	}
	if v == 0 {
		return sign + "0"
	}
	digits, decExp, roundedUp, exact := binaryToDecimalDigits(v)
	// The digit list drops the conversion's trailing zeros; the exponent
	// keeps where the digit before the point sits.
	digits = strings.TrimRight(digits, "0")
	exp := decExp - 1
	if len(digits)-exp-1 > maxMacroFractionDigits {
		digits, exp = roundMacroDigits(digits, exp, roundedUp, exact)
		if digits == "" {
			// Everything the pattern prints rounds away; Java keeps the
			// zero's sign.
			return sign + "0"
		}
	}
	if exp >= len(digits)-1 {
		return sign + digits + strings.Repeat("0", exp-len(digits)+1)
	}
	if exp >= 0 {
		return sign + digits[:exp+1] + "." + strings.TrimRight(digits[exp+1:], "0")
	}
	// The fraction starts with the zeros the exponent asks for; the ones
	// after its digits are not printed.
	fraction := strings.TrimRight(strings.Repeat("0", -exp-1)+digits, "0")
	if fraction == "" {
		return sign + "0"
	}
	return sign + "." + fraction
}

// maxMacroFractionDigits is the most fraction digits the decimal format
// prints. vanilla: MacroFunction.DECIMAL_FORMAT, setMaximumFractionDigits(15)
const maxMacroFractionDigits = 15

// roundMacroDigits rounds the developed digits at the fraction digit
// maxMacroFractionDigits, answering digits and exponent of the rounded
// value and empty digits when everything rounds away: the digits past the
// cut decide; a half at the conversion's last digit the flags do — the
// value's exact digits fall half to even, and otherwise it rounds up
// unless the conversion already had.
// vanilla: java.text.DigitList.round, shouldRoundUp
func roundMacroDigits(digits string, exp int, roundedUp, exact bool) (string, int) {
	cut := exp + maxMacroFractionDigits + 1
	if cut < 0 {
		// The cut falls among the fraction's leading zeros.
		return "", exp
	}
	up := false
	switch {
	case digits[cut] > '5':
		up = true
	case digits[cut] == '5' && cut < len(digits)-1:
		up = strings.ContainsAny(digits[cut+1:], "123456789")
	case digits[cut] == '5':
		if exact {
			up = cut > 0 && (digits[cut-1]-'0')%2 == 1
		} else {
			up = !roundedUp
		}
	}
	if !up {
		return strings.TrimRight(digits[:cut], "0"), exp
	}
	if cut == 0 {
		// Rounded into the digit before the digits: one new integer digit.
		return "1", exp + 1
	}
	b := []byte(digits[:cut])
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < '9' {
			b[i]++
			return strings.TrimRight(string(b), "0"), exp
		}
		b[i] = '0'
	}
	// The carry ran the whole kept length over.
	return "1", exp + 1
}

// nbtTypeName names the tag's own type, which a source that refuses the
// macros an argument spells it by. vanilla: TagType.getName
func nbtTypeName(tag nbt.Tag) string {
	switch tag.(type) {
	case nbt.Byte:
		return "BYTE"
	case nbt.Short:
		return "SHORT"
	case nbt.Int:
		return "INT"
	case nbt.Long:
		return "LONG"
	case nbt.Float:
		return "FLOAT"
	case nbt.Double:
		return "DOUBLE"
	case nbt.ByteArray:
		return "BYTE[]"
	case nbt.String:
		return "STRING"
	case nbt.List:
		return "LIST"
	case nbt.IntArray:
		return "INT[]"
	case nbt.LongArray:
		return "LONG[]"
	default:
		return "COMPOUND"
	}
}

// instantiate answers the function a call runs: itself when no '$' line is
// in it, its arguments read whatever they were handed, else its every
// line — the plain as compiled, each template substituted with the values
// its arguments resolve and matched against the tree of the level the
// function compiled at. The second answer is the message the refusal the
// failure names takes after the function's id; nil arguments are no with clause at all, and a source that keeps
// nothing reads as the empty compound that misses every name.
// vanilla: MacroFunction.instantiate, PlainTextFunction.instantiate
func (fn *Function) instantiate(arguments nbt.Compound) (*Function, *protocol.Text) {
	m := fn.macro
	if m == nil {
		return fn, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if arguments == nil {
		return nil, &protocol.Text{Translate: functionMissingArgumentsKey,
			With: []protocol.Text{{Text: m.id}}}
	}
	var buf [8]string
	values := buf[:0]
	for _, name := range m.args {
		tag, ok := arguments[name]
		if !ok {
			return nil, &protocol.Text{Translate: functionMissingArgumentKey,
				With: []protocol.Text{{Text: m.id}, {Text: name}}}
		}
		values = append(values, stringifyTag(tag))
	}
	// The last calls' answers stand; a hit becomes the most recent.
	for i, hit := range m.cache {
		if !slices.Equal(hit.values, values) {
			continue
		}
		m.cache = append(slices.Delete(m.cache, i, i+1), hit)
		return hit.fn, nil
	}
	// The oldest answer leaves before the new one builds, so a refusal
	// still spends one entry. vanilla: MacroFunction.instantiate
	if len(m.cache) >= maxMacroCache {
		m.cache = m.cache[1:]
	}
	values = slices.Clone(values)
	out := &Function{ID: m.id}
	for _, entry := range m.entries {
		if entry.plain {
			out.lines = append(out.lines, entry.line)
			continue
		}
		text := entry.template.substitute(values)
		if len(text) > maxFunctionLine {
			// vanilla: CommandFunction.checkCommandLineLength
			return nil, &protocol.Text{Text: fmt.Sprintf("command too long: %d characters", len(text))}
		}
		CacheLineTokens(text)
		tokens := Tokenize(text)
		if node, failure := Match(ForLevel(fn.level), text, tokens, true); node == nil {
			return nil, &protocol.Text{Translate: functionParseKey, With: []protocol.Text{
				{Text: m.id}, {Text: text}, {Text: describeFailure(text, tokens, failure)}}}
		}
		words := make([]string, len(tokens))
		for i, t := range tokens {
			words[i] = t.Text
		}
		out.lines = append(out.lines, functionLine{input: text, words: words})
	}
	m.cache = append(m.cache, macroInstantiation{values: values, fn: out})
	return out, nil
}
