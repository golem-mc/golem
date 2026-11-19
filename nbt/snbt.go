package nbt

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseSNBT reads text that names one compound and nothing after it, in the
// vanilla grammar: typed arrays, quoted or bare strings, 0x/0b integers with
// b, s, i or l suffixes, f/d floats, true and false as bytes, and trailing
// commas where the grammar allows them. Suffixes are kept, so a compound
// round-trips; an unsuffixed integer too wide for an int is refused rather
// than widened. vanilla: SnbtGrammar, TagParser.parseCompoundAsArgument
func ParseSNBT(text string) (Compound, error) {
	tag, err := parseSNBT(text, false)
	if err != nil {
		return nil, err
	}
	return tag.Entries(), nil
}

// ParseOrderedSNBT is ParseSNBT retaining compound insertion history.
func ParseOrderedSNBT(text string) (CompoundTag, error) {
	return parseSNBT(text, true)
}

func parseSNBT(text string, ordered bool) (CompoundTag, error) {
	r := &snbtReader{src: text, far: -1, ordered: ordered}
	r.skipSpace()
	v, err := r.value()
	if err != nil {
		return nil, err
	}
	compound, ok := v.(CompoundTag)
	if !ok {
		return nil, r.fail("", "expected a compound tag")
	}
	r.skipSpace()
	if r.pos < len(r.src) {
		// The reading is done; the dispatcher's expected separator is the
		// caller's complaint, at the offset the trailing data begins.
		return nil, r.fail(ErrTrailing, "trailing data after the compound")
	}
	return compound, nil
}

// ErrTrailing marks text with data after the compound; the caller answers
// the command dispatcher's own separator complaint at the error's offset.
const ErrTrailing = "snbt.parser.trailing"

// ParseValue reads one SNBT value at the start of text and returns the
// offset just past it, even where what follows is no value. A failure is the
// reading's own error at its furthest expectation.
func ParseValue(text string) (int, *ParseError) {
	r := &snbtReader{src: text, far: -1}
	r.skipSpace()
	if _, err := r.value(); err != nil {
		pe, _ := err.(*ParseError)
		return r.pos, pe
	}
	return r.pos, nil
}

// ParseValueTag is ParseValue that also answers the tag read.
func ParseValueTag(text string) (Tag, int, *ParseError) {
	return parseValueTag(text, false)
}

// ParseOrderedValueTag is ParseValueTag retaining compound insertion history.
// Callers must accept CompoundTag rather than just Compound.
func ParseOrderedValueTag(text string) (Tag, int, *ParseError) {
	return parseValueTag(text, true)
}

func parseValueTag(text string, ordered bool) (Tag, int, *ParseError) {
	r := &snbtReader{src: text, far: -1, ordered: ordered}
	r.skipSpace()
	v, err := r.value()
	if err != nil {
		pe, _ := err.(*ParseError)
		return nil, r.pos, pe
	}
	return v, r.pos, nil
}

// The vanilla parser's error names, the keys the client renders a failure
// with; structural failures (a missing comma or colon) have no key.
// vanilla: SnbtGrammar
const (
	errEmptyKey           = "snbt.parser.empty_key"
	errDecimalNumeral     = "snbt.parser.expected_decimal_numeral"
	errHexEscape          = "snbt.parser.expected_hex_escape"
	errHexNumeral         = "snbt.parser.expected_hex_numeral"
	errBinaryNumeral      = "snbt.parser.expected_binary_numeral"
	errNonNegative        = "snbt.parser.expected_non_negative_number"
	errIntegerType        = "snbt.parser.expected_integer_type"
	errNumberOrBoolean    = "snbt.parser.expected_number_or_boolean"
	errStringUUID         = "snbt.parser.expected_string_uuid"
	errUnquoted           = "snbt.parser.expected_unquoted_string"
	errInfinity           = "snbt.parser.infinity_not_allowed"
	errInvalidCharName    = "snbt.parser.invalid_character_name"
	errInvalidCodepoint   = "snbt.parser.invalid_codepoint"
	errArrayElement       = "snbt.parser.invalid_array_element_type"
	errStringContents     = "snbt.parser.invalid_string_contents"
	errLeadingZero        = "snbt.parser.leading_zero_not_allowed"
	errNoSuchOperation    = "snbt.parser.no_such_operation"
	errNumberParseFailure = "snbt.parser.number_parse_failure"

	// errTooDeep has no snbt.parser key of its own, vanilla's grammar
	// having no depth limit; see snbtReader.value.
	errTooDeep = "arguments.nbtpath.too_deep"
)

// literalIncorrect is the grammar's expectation of a literal; its argument
// is the alternatives joined with '|'.
const literalIncorrect = "argument.literal.incorrect"

// ParseError is a SNBT failure: the byte offset, the vanilla error name
// where the client has one, and the values of its %s placeholders.
// vanilla: SnbtGrammar
type ParseError struct {
	Offset int
	Key    string
	With   []string
	msg    string
}

// Error spells the prose the parser failed with.
func (e *ParseError) Error() string { return e.msg }

// snbtReader walks SNBT text one rune at a time, building the tags it reads.
//
// Every expectation is recorded where it stood, and a failed parse reports
// the first one at the furthest position: that is why a missing comma after
// a number reads as the number's expected dot.
// vanilla: Grammar.parseForCommands
type snbtReader struct {
	ordered    bool
	completion *snbtCompletion
	src        string
	pos        int
	far        int // furthest recorded position, -1 before anything records
	fark       string
	farw       []string

	// deferred is an array element failure held until the closing bracket,
	// where vanilla validates array elements.
	deferred *ParseError
	// depth counts nesting as Decode does, the root at 0.
	depth int
}

// look records what a reading expected where the reader stands.
func (r *snbtReader) look(key string, with ...string) {
	r.completion.advance(r.pos)
	if r.far < 0 || r.pos > r.far {
		r.far, r.fark, r.farw = r.pos, key, with
	}
}

// lookAt records an expectation at a position the reader has passed.
func (r *snbtReader) lookAt(pos int, key string, with ...string) {
	r.completion.advance(pos)
	if r.far < 0 || pos > r.far {
		r.far, r.fark, r.farw = pos, key, with
	}
}

// atFar is the first recorded expectation at or past pos, the one vanilla
// reports.
func (r *snbtReader) atFar(pos int) *ParseError {
	if r.far >= pos {
		return &ParseError{Offset: r.far, Key: r.fark, With: r.farw}
	}
	return nil
}

// fail is the furthest recorded expectation at or past the reader's
// position, or the failure itself.
func (r *snbtReader) fail(key, msg string, with ...string) *ParseError {
	if far := r.atFar(r.pos); far != nil {
		return far
	}
	return &ParseError{Offset: r.pos, Key: key, With: with, msg: msg}
}

// failAt is fail with the failure pointed somewhere the reader has passed.
func (r *snbtReader) failAt(pos int, key, msg string, with ...string) *ParseError {
	if far := r.atFar(pos); far != nil {
		return far
	}
	return &ParseError{Offset: pos, Key: key, With: with, msg: msg}
}

func (r *snbtReader) skipSpace() {
	for r.pos < len(r.src) {
		switch r.src[r.pos] {
		case ' ', '\t', '\n', '\r':
			r.pos++
		default:
			return
		}
	}
}

// value reads one tag of whichever kind comes next. A numeral that cannot
// complete returns without a value, and the caller retries the run as a
// word; nothing readable is the unquoted string the grammar wanted.
func (r *snbtReader) value() (Tag, error) {
	// Limit nesting to what Decode can read.
	// vanilla: NbtPathArgument$NbtPath.isTooDeep
	if r.depth > maxDepth {
		return nil, &ParseError{Offset: r.pos, Key: errTooDeep, msg: "tags nested more than " + strconv.Itoa(maxDepth) + " deep"}
	}
	if r.pos >= len(r.src) {
		r.look(errUnquoted)
		return nil, r.fail(errUnquoted, "expected a value")
	}
	switch c := r.src[r.pos]; {
	case c == '{':
		return r.compound()
	case c == '[':
		return r.list()
	case c == '"' || c == '\'':
		s, err := r.quoted()
		if err != nil {
			return nil, err
		}
		return String(s), nil
	case startsNumber(c):
		n, err := r.number()
		if err != nil {
			return nil, err
		}
		return n, nil
	default:
		text, ok := r.unquoted()
		if !ok {
			r.look(errUnquoted)
			return nil, r.fail("", "not a tag")
		}
		// A word followed by '(' names an operation, ahead of the booleans:
		// true( is the operation true.
		// vanilla: SnbtGrammar.unquoted_string_or_builtin
		if r.pos < len(r.src) && r.src[r.pos] == '(' {
			return r.operation(text)
		}
		// The parenthesis is expected past any whitespace, the separator
		// right after the word.
		end := r.pos
		for end < len(r.src) && strings.IndexByte(" \t\n\r", r.src[end]) >= 0 {
			end++
		}
		r.lookAt(end, literalIncorrect, "(")
		r.suggestAt(end, "(")
		switch {
		case strings.EqualFold(text, "true"):
			return Byte(1), nil
		case strings.EqualFold(text, "false"):
			return Byte(0), nil
		}
		return String(text), nil
	}
}

// wordChar is whether c continues an unquoted word.
func wordChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '_' || c == '.' || c == '-' || c == '+'
}

// operation reads the arguments of the tag operation the caller named: bool
// (a number or boolean as a byte) and uuid (the dashed form as an int
// array), one argument each. An unclosed argument list keeps what it read and
// leaves the reader at the '('. An unknown operation is reported by name and
// arity, a refused argument past the parenthesis.
// vanilla: SnbtGrammar.booleanTag, SnbtGrammar.uuidTag
func (r *snbtReader) operation(name string) (Tag, error) {
	open := r.pos // the '('
	r.pos++
	var args []Tag
	closed := false
	for {
		r.skipSpace()
		at := r.pos
		value, err := r.value()
		if err != nil {
			r.suggestAt(at, ")")
			// The list ends on a value it could not read; a ')' here is the
			// empty argument list.
			r.pos = at
			if r.pos < len(r.src) && r.src[r.pos] == ')' {
				r.pos++
				closed = true
			}
			break
		}
		args = append(args, value)
		r.skipSpace()
		if r.pos < len(r.src) && r.src[r.pos] == ',' {
			r.pos++
			continue
		}
		if r.pos < len(r.src) && r.src[r.pos] == ')' {
			r.pos++
			closed = true
			break
		}
		r.lookAt(r.pos, literalIncorrect, ")")
		r.suggestAt(r.pos, ",", ")")
		break
	}
	// Unclosed, the operation still runs, but the reader stands back at the
	// parenthesis and the bytes after it are the caller's.
	if !closed {
		defer func() { r.pos = open }()
	}
	switch {
	case name == "bool" && len(args) == 1:
		switch args[0].(type) {
		case Byte, Short, Int, Long, Float, Double:
			return boolByte(!isZero(args[0])), nil
		}
		return nil, r.failAt(r.pos, errNumberOrBoolean, "bool() needs a number or a boolean")
	case name == "uuid" && len(args) == 1:
		text, ok := args[0].(String)
		if !ok {
			return nil, r.failAt(r.pos, errStringUUID, "uuid() needs a string")
		}
		words, err := uuidWords(string(text))
		if err != nil {
			return nil, r.failAt(r.pos, errStringUUID, "uuid() needs a dashed UUID")
		}
		return words, nil
	}
	return nil, r.failAt(r.pos, errNoSuchOperation, "unknown operation "+name, name+"/"+strconv.Itoa(len(args)))
}

// boolByte is the byte a boolean reads as.
func boolByte(v bool) Byte {
	if v {
		return 1
	}
	return 0
}

// uuidWords reads the 8-4-4-4-12 form of a UUID into four signed 32-bit
// words, most significant first. vanilla: SnbtGrammar.uuidTag, UUIDUtil.fromString
func uuidWords(text string) (IntArray, error) {
	if len(text) != 36 || text[8] != '-' || text[13] != '-' || text[18] != '-' || text[23] != '-' {
		return nil, &ParseError{Key: errStringUUID, msg: "a UUID is 36 characters in the 8-4-4-4-12 form"}
	}
	hex := text[:8] + text[9:13] + text[14:18] + text[19:23] + text[24:]
	words := make(IntArray, 4)
	for i := range words {
		v, err := strconv.ParseUint(hex[i*8:(i+1)*8], 16, 32)
		if err != nil {
			return nil, &ParseError{Key: errStringUUID, msg: "not hexadecimal"}
		}
		words[i] = int32(v)
	}
	return words, nil
}

// isZero is whether a number tag the bool operation read holds nought.
func isZero(tag Tag) bool {
	switch t := tag.(type) {
	case Byte:
		return t == 0
	case Short:
		return t == 0
	case Int:
		return t == 0
	case Long:
		return t == 0
	case Float:
		return t == 0
	case Double:
		return t == 0
	}
	return false
}

func (r *snbtReader) compound() (Tag, error) {
	r.pos++ // the '{'
	r.depth++
	defer func() { r.depth-- }()
	var c CompoundTag = Compound{}
	if r.ordered {
		c = &OrderedCompound{}
	}
	for {
		r.skipSpace()
		if r.pos >= len(r.src) {
			// The entry the grammar wanted is a name first, quoted or not.
			r.look(literalIncorrect, "\"")
			r.suggestAt(r.pos, "\"", "'", "}")
			return nil, r.fail("", "unclosed compound")
		}
		if r.src[r.pos] == '}' {
			r.pos++
			return c, nil
		}
		key, err := r.key()
		if err != nil {
			return nil, err
		}
		r.skipSpace()
		if r.pos >= len(r.src) || r.src[r.pos] != ':' {
			r.lookAt(r.pos, literalIncorrect, ":")
			r.suggestAt(r.pos, ":")
			return nil, r.fail("", "expected a colon")
		}
		r.pos++
		r.skipSpace()
		v, err := r.value()
		if err != nil {
			return nil, err
		}
		// An empty name fails after its value is read, before a trailing
		// comma or the closing brace. vanilla: SnbtGrammar map_entry
		if key == "" {
			return nil, r.fail(errEmptyKey, "a compound's key is empty")
		}
		c.Set(key, v)
		r.skipSpace()
		if r.pos >= len(r.src) {
			r.lookAt(r.pos, literalIncorrect, ",")
			r.lookAt(r.pos, literalIncorrect, "}")
			r.suggestAt(r.pos, ",", "}")
			return nil, r.fail("", "unclosed compound")
		}
		switch r.src[r.pos] {
		case ',':
			// A trailing comma is allowed; the next round's brace accepts it.
			r.pos++
		case '}':
			r.pos++
			return c, nil
		default:
			r.lookAt(r.pos, literalIncorrect, ",")
			r.lookAt(r.pos, literalIncorrect, "}")
			r.suggestAt(r.pos, ",", "}")
			return nil, r.fail("", "expected a comma or the compound's end")
		}
	}
}

// key reads an entry's name, quoted or not. An empty quoted name fails
// after its value, where the value's readings are the furthest recorded;
// a name unreadable either way fails where it stood.
func (r *snbtReader) key() (string, error) {
	if r.src[r.pos] == '"' || r.src[r.pos] == '\'' {
		return r.quoted()
	}
	text, ok := r.unquoted()
	if !ok {
		r.look(literalIncorrect, "\"")
		return "", r.fail("", "expected a name")
	}
	return text, nil
}

// list reads a '['-opened value: a typed array when the prefix letter and
// ';' follow, else a plain list. A letter the array prefix could take is
// recorded first, so it is reported before the value's expectations.
// vanilla: SnbtGrammar.ArrayPrefix
func (r *snbtReader) list() (Tag, error) {
	r.pos++ // the '['
	r.skipSpace()
	for _, letter := range []byte{'B', 'L', 'I'} {
		r.look(literalIncorrect, string(letter))
		r.suggestAt(r.pos, string(letter))
	}
	if r.pos >= len(r.src) {
		r.suggestAt(r.pos, "]")
		return nil, r.fail("", "unclosed list")
	}
	if r.src[r.pos] == 'B' || r.src[r.pos] == 'I' || r.src[r.pos] == 'L' {
		kind := r.src[r.pos]
		prefixStart := r.pos
		r.pos++
		r.skipSpace()
		if r.pos >= len(r.src) || r.src[r.pos] != ';' {
			r.lookAt(r.pos, literalIncorrect, ";")
			r.suggestAt(r.pos, ";")
			if r.completion != nil {
				r.pos = prefixStart
				items, elem, err := r.elements()
				return List{Elem: elem, Items: items}, err
			}
			return nil, r.fail("", "expected a semicolon after the array's type")
		}
		r.pos++
		return r.array(kind)
	}
	items, elem, err := r.elements()
	if err != nil {
		return nil, err
	}
	return List{Elem: elem, Items: items}, nil
}

// elements reads a plain list's values to its closing bracket. A mixed list
// reports compound as its type, as it saves; an empty one has no type.
func (r *snbtReader) elements() ([]Tag, Type, error) {
	r.depth++
	defer func() { r.depth-- }()
	var items []Tag
	var elem Type
	for {
		r.skipSpace()
		if r.pos >= len(r.src) {
			r.suggestAt(r.pos, "]")
			return nil, 0, r.fail("", "unclosed list")
		}
		if r.src[r.pos] == ']' {
			// One trailing comma is allowed. vanilla: SnbtGrammar list_entries
			r.pos++
			return items, listElem(items, elem), nil
		}
		v, err := r.value()
		if err != nil {
			return nil, 0, err
		}
		if items == nil {
			elem = v.Type()
		}
		items = append(items, v)
		r.skipSpace()
		if r.pos >= len(r.src) {
			r.lookAt(r.pos, literalIncorrect, ",")
			r.lookAt(r.pos, literalIncorrect, "]")
			r.suggestAt(r.pos, ",", "]")
			return nil, 0, r.fail("", "unclosed list")
		}
		switch r.src[r.pos] {
		case ',':
			r.pos++
		case ']':
			r.pos++
			return items, listElem(items, elem), nil
		default:
			r.lookAt(r.pos, literalIncorrect, ",")
			r.lookAt(r.pos, literalIncorrect, "]")
			r.suggestAt(r.pos, ",", "]")
			return nil, 0, r.fail("", "expected a comma or the list's end")
		}
	}
}

// listElem is the type a parsed list reports: its entries' shared type, or
// compound for a mixed list.
func listElem(items []Tag, elem Type) Type {
	if heterogeneous(items) {
		return TypeCompound
	}
	return elem
}

// array reads a typed array's integers. An element's suffix may narrow it
// below the array's width but not widen it, an unsuffixed element is of the
// array's width, and a narrower value wraps into it. A range or width
// failure is reported past the closing bracket, where vanilla validates.
// vanilla: SnbtGrammar.ArrayPrefix.create
func (r *snbtReader) array(kind byte) (Tag, error) {
	var items []Tag
	for {
		r.skipSpace()
		if r.pos >= len(r.src) {
			// The element the grammar wanted begins with a sign.
			r.look(literalIncorrect, "+")
			r.look(literalIncorrect, "-")
			r.suggestAt(r.pos, "+", "-", "0", "]")
			return nil, r.failAt(r.pos, errDecimalNumeral, "expected digits")
		}
		if r.src[r.pos] == ']' {
			r.pos++
			if err := r.closeArray(); err != nil {
				return nil, err
			}
			return arrayTagOf(kind, items), nil
		}
		v, err := r.arrayElement(kind)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
		r.skipSpace()
		if r.pos >= len(r.src) {
			r.suggestAt(r.pos, ",", "]")
			return nil, r.fail("", "unclosed array")
		}
		switch r.src[r.pos] {
		case ',':
			r.pos++
		case ']':
			r.pos++
			if err := r.closeArray(); err != nil {
				return nil, err
			}
			return arrayTagOf(kind, items), nil
		default:
			r.look(literalIncorrect, ",")
			r.look(literalIncorrect, "]")
			r.suggestAt(r.pos, ",", "]")
			return nil, r.fail("", "expected a comma or the array's end")
		}
	}
}

// deferArray parks an element failure at the array's closing bracket.
func (r *snbtReader) deferArray(pe *ParseError) {
	if r.deferred == nil {
		r.deferred = pe
	}
}

// closeArray raises the held element failure, if any, at the bracket.
func (r *snbtReader) closeArray() error {
	if r.deferred == nil {
		return nil
	}
	pe := r.deferred
	r.deferred = nil
	r.lookAt(r.pos, pe.Key, pe.With...)
	return r.fail("", "an array element out of range")
}

// arrayElement reads one integer of the array, at the array's width. A
// range or width failure is returned for closeArray to raise, not raised
// here. vanilla: SnbtGrammar.ArrayPrefix.create
func (r *snbtReader) arrayElement(kind byte) (Tag, error) {
	elemWidth := 32
	switch kind {
	case 'B':
		elemWidth = 8
	case 'L':
		elemWidth = 64
	}
	r.look(literalIncorrect, "+")
	r.look(literalIncorrect, "-")
	sign, base, cut := r.numberStart()
	if cut != noCut {
		if cut == hexCut {
			r.look(errHexNumeral)
		} else {
			r.look(errBinaryNumeral)
		}
		return nil, r.fail("", "expected a numeral after the prefix")
	}
	digits, edge := r.digitRun(base)
	if edge {
		return nil, r.fail(errDecimalNumeral, "a numeral carries no underscore at its edge")
	}
	if digits == "" {
		return nil, r.failAt(r.pos, errDecimalNumeral, "expected digits")
	}
	if base == 10 && len(digits) > 1 && digits[0] == '0' {
		r.look(errLeadingZero)
		return nil, r.fail("", "a decimal number carries no leading zero")
	}
	// The suffix's width must fit the array's: [B;1s] is refused, [I;1s]
	// narrows into the int array.
	v, width, err := r.integerSuffix(sign, base, digits, false, elemWidth)
	if err != nil {
		if pe, ok := err.(*ParseError); ok && (pe.Key == errNumberParseFailure || pe.Key == errArrayElement || pe.Key == errNonNegative) {
			// Deferred to the closing bracket; the element still reads.
			r.deferArray(pe)
			return dudElement, nil
		}
		return nil, err
	}
	// No suffix followed: the grammar names the width letter it wanted.
	r.look(literalIncorrect, "u|U")
	if width > elemWidth {
		// Deferred like the range failure.
		r.deferArray(&ParseError{Key: errArrayElement})
		return dudElement, nil
	}
	// A dot, an exponent or a float suffix makes the element no integer.
	// vanilla: SnbtGrammar.ArrayPrefix
	if r.at(".eEfFdD") != 0 {
		return nil, r.fail(errIntegerType, "an array element is an integer")
	}
	var n int64
	switch t := v.(type) {
	case Byte:
		n = int64(t)
	case Short:
		n = int64(t)
	case Int:
		n = int64(t)
	case Long:
		n = int64(t)
	}
	switch elemWidth {
	case 8:
		return Byte(int8(n)), nil
	case 32:
		return Int(int32(n)), nil
	default:
		return Long(n), nil
	}
}

// dudElement is what a failed element reads as, so the array's brackets
// stay in reach.
var dudElement Tag = Int(0)

// arrayTagOf wraps an array's numbers as the tag its kind names.
func arrayTagOf(kind byte, items []Tag) Tag {
	switch kind {
	case 'B':
		out := make(ByteArray, len(items))
		for i, v := range items {
			out[i] = byte(v.(Byte))
		}
		return out
	case 'I':
		out := make(IntArray, len(items))
		for i, v := range items {
			out[i] = int32(v.(Int))
		}
		return out
	default:
		out := make(LongArray, len(items))
		for i, v := range items {
			out[i] = int64(v.(Long))
		}
		return out
	}
}

// quoted reads a single- or double-quoted string with its escapes: control
// characters, backslash, quotes, \x and \u code points and \N{NAME}.
func (r *snbtReader) quoted() (string, error) {
	quote := r.src[r.pos]
	r.pos++
	var out strings.Builder
	for r.pos < len(r.src) {
		c := r.src[r.pos]
		r.pos++
		switch c {
		case quote:
			return out.String(), nil
		case '\\':
			r.suggestAlternatives("b", "s", "t", "n", "f", "r", "\\", "'", "\"", "x", "u", "U", "N")
			if r.pos >= len(r.src) {
				r.suggestAt(r.pos, "\"", "'", "\\", "b", "s", "t", "n", "f", "r", "x", "u", "U", "N")
				return "", r.fail(errStringContents, "unterminated escape")
			}
			// The escape letters are expected where the backslash stood.
			r.lookAt(r.pos, literalIncorrect, "b")
			e := r.src[r.pos]
			r.pos++
			switch e {
			case 'b':
				out.WriteByte('\b')
			case 's':
				out.WriteByte(' ')
			case 't':
				out.WriteByte('\t')
			case 'n':
				out.WriteByte('\n')
			case 'f':
				out.WriteByte('\f')
			case 'r':
				out.WriteByte('\r')
			case '\\', '\'', '"':
				out.WriteByte(e)
			case 'x', 'u', 'U':
				width := 2
				switch e {
				case 'u':
					width = 4
				case 'U':
					width = 8
				}
				r.suggestAt(r.pos)
				if r.pos+width > len(r.src) {
					return "", r.fail(errHexEscape, "unterminated hex escape", strconv.Itoa(width))
				}
				n, err := strconv.ParseUint(r.src[r.pos:r.pos+width], 16, 32)
				if err != nil {
					return "", r.fail(errHexEscape, "bad code point in escape", strconv.Itoa(width))
				}
				if n > 0x10ffff {
					return "", r.fail(errInvalidCodepoint, "bad code point in escape", fmt.Sprintf("U+%08X", n))
				}
				r.pos += width
				out.WriteRune(rune(n))
			case 'N':
				// \N{NAME}: a character named by its Unicode name.
				// vanilla: Character.codePointOf
				if r.pos >= len(r.src) || r.src[r.pos] != '{' {
					r.suggestNext(r.pos, "{")
					return "", r.fail(literalIncorrect, "a character name needs its brace", "{")
				}
				r.pos++
				start := r.pos
				for r.pos < len(r.src) && unicodeNameChar(r.src[r.pos]) {
					r.pos++
				}
				if r.pos == start {
					return "", r.fail(errInvalidCharName, "a character name is empty")
				}
				if r.pos >= len(r.src) || r.src[r.pos] != '}' {
					r.suggestAt(r.pos, "}")
					return "", r.fail(literalIncorrect, "a character name needs its brace", "}")
				}
				name := r.src[start:r.pos]
				r.pos++
				cp, ok := codePointOf(name)
				if !ok {
					return "", r.fail(errInvalidCharName, "no such character name")
				}
				out.WriteRune(cp)
			default:
				return "", r.failAt(r.pos-1, errStringContents, "unknown escape \\"+string(e))
			}
		default:
			out.WriteByte(c)
		}
	}
	r.suggestAt(r.pos, "\"", "'", "\\")
	return "", r.fail(errStringContents, "unterminated string")
}

// unicodeNameChar reports whether c is in vanilla's \N{NAME} class
// [-a-zA-Z0-9 ].
func unicodeNameChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == ' '
}

// unquoted reads a bare token. It may not begin with a character a number
// starts with; that is how the grammar tells the two apart.
func (r *snbtReader) unquoted() (string, bool) {
	if r.pos < len(r.src) && startsNumber(r.src[r.pos]) {
		return "", false
	}
	start := r.pos
	for r.pos < len(r.src) {
		c := r.src[r.pos]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '_' || c == '.' || c == '-' || c == '+' {
			r.pos++
			continue
		}
		break
	}
	return r.src[start:r.pos], r.pos > start
}

// startsNumber reports whether c can begin a number.
func startsNumber(c byte) bool {
	return c == '+' || c == '-' || c == '.' || c >= '0' && c <= '9'
}

// number reads one numeric tag: integers in decimal, 0x or 0b, '_' between
// digits, an optional u and a b/s/i/l suffix; a dot or exponent makes a
// float (f) or double (d or no suffix), falling back to the integer when the
// float does not complete. A 0x without digits, a bare sign or a dangling
// underscore returns no value with the reader past what it read, so the
// caller retries the run as a word. Each rule's expectation is recorded.
// vanilla: SnbtGrammar.createInteger, SnbtGrammar.createFloat
func (r *snbtReader) number() (Tag, error) {
	start := r.pos
	sign, base, cut := r.numberStart()
	if cut != noCut {
		// The grammar committed at the prefix letter and no digit came;
		// it is expected where the digit belonged. Hex then falls back to
		// a word, binary does not (0b before a decimal digit is a failed
		// binary numeral). A bare 0b never gets here: its b is a suffix.
		// vanilla: SnbtGrammar.integer_literal
		if cut == hexCut {
			r.look(errHexNumeral)
			r.wordRetryAt(start)
		} else {
			r.look(errBinaryNumeral)
		}
		return nil, r.fail("", "expected a numeral after the prefix")
	}
	if base != 10 {
		digits, edge := r.digitRun(base)
		if edge || digits == "" {
			r.wordRetryAt(start)
			return nil, r.fail("", "expected a numeral after the prefix")
		}
		r.skipSpace()
		r.look(literalIncorrect, "u|U")
		v, _, err := r.integerSuffix(sign, base, digits, false, 32)
		return v, err
	}

	// Decimal: digits, then a dot or an exponent.
	// vanilla: SnbtGrammar.float_literal, integer_literal
	whole, edge := r.digitRun(10)
	if edge {
		r.wordRetryAt(start)
		return nil, r.fail("", "a numeral carries no underscore at its edge")
	}
	if r.pos < len(r.src) && r.src[r.pos] == '.' {
		r.pos++
		fractionStart := r.pos
		fraction, edgeF := r.digitRun(10)
		if edgeF {
			// An underscore after the dot separates nothing: the fraction is
			// not taken and the value is the whole number with its dot.
			r.pos = fractionStart
			fraction = ""
		}
		if whole == "" && fraction == "" {
			// No digit on either side of the dot: expected here, before the
			// run is retried as a word.
			r.lookAt(r.pos, errDecimalNumeral)
			r.wordRetryAt(start)
			return nil, r.fail("", "expected digits after the dot")
		}
		if fraction == "" {
			// No digit after the dot: still expected, though the run reads.
			r.lookAt(r.pos, errDecimalNumeral)
		}
		n, err := r.floatSuffix(sign, whole, fraction)
		if err == nil {
			return n, nil
		}
		r.record(err)
		if pe, ok := err.(*ParseError); ok && pe.Key == errInfinity {
			return nil, err
		}
		if whole == "" {
			return nil, err
		}
		return r.decimalInteger(start)
	}
	if whole != "" && r.pos < len(r.src) && (r.src[r.pos] == 'e' || r.src[r.pos] == 'E') {
		n, err := r.floatSuffix(sign, whole, "")
		if err == nil {
			return n, nil
		}
		r.record(err)
		if pe, ok := err.(*ParseError); ok && pe.Key == errInfinity {
			return nil, err
		}
		return r.decimalInteger(start)
	}
	return r.decimalInteger(start)
}

// decimalInteger reads a decimal integer from start, the fallback when a
// float run does not complete.
func (r *snbtReader) decimalInteger(start int) (Tag, error) {
	r.pos = start
	sign, _, _ := r.numberStart()
	whole, edge := r.digitRun(10)
	if edge {
		r.wordRetryAt(start)
		return nil, r.fail("", "a numeral carries no underscore at its edge")
	}
	if whole == "" {
		r.suggestAlternatives(".")
		// A sign without its numeral, named where the digits belonged.
		r.look(errDecimalNumeral)
		r.wordRetryAt(start)
		return nil, r.fail("", "expected a number")
	}
	r.skipSpace()
	r.look(literalIncorrect, ".")
	r.look(literalIncorrect, "e|E")
	r.suggestAlternatives(".", "eE", "fF", "dD")
	if len(whole) > 1 && whole[0] == '0' {
		// Leading zeros are the refused numeral, expected where the run
		// ended. vanilla: SnbtGrammar.leading_zero
		if r.completion != nil {
			end := r.pos
			r.wordRetryAt(start)
			r.pos = end
		}
		r.lookAt(r.pos, errLeadingZero)
		return nil, r.fail("", "a decimal number carries no leading zero")
	}
	v, _, err := r.integerSuffix(sign, 10, whole, true, 32)
	return v, err
}

// record adds a failed reading's furthest expectation to the log, so the
// fallback taken does not lose it.
func (r *snbtReader) record(err error) {
	pe, ok := err.(*ParseError)
	if !ok || pe.Key == "" {
		return
	}
	r.lookAt(pe.Offset, pe.Key, pe.With...)
}

// The prefixes a numeral can be cut at: none, 0x without a digit, and 0b
// followed by a non-binary digit. A bare 0b is the byte zero, b its suffix.
const (
	noCut = iota
	hexCut
	binaryCut
)

// wordRetryAt reads the word run from where a numeral failed and records
// the '(' of the operation the grammar would have read there.
func (r *snbtReader) wordRetryAt(start int) {
	r.pos = start
	for r.pos < len(r.src) && wordChar(r.src[r.pos]) {
		r.pos++
	}
	r.lookAt(r.pos, literalIncorrect, "(")
	r.suggestAt(r.pos, "bool", "false", "true", "uuid")
	r.suggestNext(r.pos, "(")
}

// numberStart reads a number's sign and the base a 0x or 0b prefix names.
func (r *snbtReader) numberStart() (string, int, int) {
	r.suggestAlternatives("+", "-")
	sign := ""
	if r.pos < len(r.src) && (r.src[r.pos] == '+' || r.src[r.pos] == '-') {
		if r.src[r.pos] == '-' {
			sign = "-"
		}
		r.pos++
	}
	r.suggestAlternatives("0")
	if r.pos < len(r.src) && r.src[r.pos] == '0' {
		r.suggestAlternativesAt(r.pos+1, "xX", "bB")
	}
	base := 10
	if r.pos+1 < len(r.src) && r.src[r.pos] == '0' &&
		(r.src[r.pos+1] == 'x' || r.src[r.pos+1] == 'X' ||
			r.src[r.pos+1] == 'b' || r.src[r.pos+1] == 'B') {
		next, cut := 16, hexCut
		if r.src[r.pos+1] == 'b' || r.src[r.pos+1] == 'B' {
			next, cut = 2, binaryCut
		}
		digit := byte(0)
		if r.pos+2 < len(r.src) {
			digit = r.src[r.pos+2]
		}
		if isDigitOfBase(digit, next) {
			base = next
			r.pos += 2
		} else if next == 16 || digit >= '0' && digit <= '9' {
			// The grammar commits at a hex prefix, and at a binary one
			// followed by a non-binary digit, leaving the reader where the
			// digit belonged. A bare 0b is left for the suffix: the byte 0.
			// vanilla: SnbtGrammar.integer_literal
			r.pos += 2
			return sign, 0, cut
		}
	}
	return sign, base, noCut
}

// isDigitOfBase reports whether c is a digit of the base given.
func isDigitOfBase(c byte, base int) bool {
	switch {
	case c >= '0' && c <= '9':
		return base != 2 || c <= '1'
	case base == 16:
		return c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
	}
	return false
}

// digitRun takes the run of digits in base at the reader, '_' allowed only
// between digits. The second result reports a misplaced underscore, which
// is the numeral's own failure rather than missing digits.
func (r *snbtReader) digitRun(base int) (string, bool) {
	start := r.pos
	digits := false
	edge := false
	for r.pos < len(r.src) {
		c := r.src[r.pos]
		if c == '_' {
			edge = edge || !digits
			r.pos++
			continue
		}
		if isDigitOfBase(c, base) {
			digits = true
			r.pos++
			continue
		}
		break
	}
	// An underscore past the last digit dangles rather than separates.
	if digits && r.pos > start && r.src[r.pos-1] == '_' {
		edge = true
	}
	if !digits {
		r.pos = start
	}
	run := ""
	if digits {
		run = strings.ReplaceAll(r.src[start:r.pos], "_", "")
	}
	return run, edge
}

// integerSuffix reads an optional u and a b/s/i/l suffix after optional
// whitespace and builds the value at that width, or at defaultWidth (too
// wide is refused); the width answered is the one built at. f and d make a
// float or double only for a decimal number. A u under a minus sign is
// reported past the suffix. vanilla: SnbtGrammar.IntegerSuffix
func (r *snbtReader) integerSuffix(sign string, base int, digits string, decimal bool, defaultWidth int) (Tag, int, error) {
	// Hex and binary default to unsigned; only an explicit s prefix signs them.
	// vanilla: SnbtGrammar.IntegerLiteral.signedOrDefault
	unsigned := base != 10
	r.skipSpace()
	r.suggestAlternatives("uU", "sS", "bB", "sS", "iI", "lL")
	// A signedness prefix needs a width letter; a bare s is the short suffix.
	prefixed := false
	if prefix := r.at("uUsS"); prefix != 0 {
		if r.pos+1 < len(r.src) && strings.IndexByte("bBsSiIlL", r.src[r.pos+1]) >= 0 {
			r.pos++
			unsigned = prefix == 'u' || prefix == 'U'
			prefixed = true
		} else if prefix == 'u' || prefix == 'U' {
			r.lookAt(r.pos+1, literalIncorrect, "b|B")
			r.suggestNext(r.pos+1, "b", "B", "s", "S", "i", "I", "l", "L")
		}
	}
	width := 0
	switch {
	case r.at("bB") != 0:
		r.pos++
		width = 8
	case r.at("sS") != 0:
		r.pos++
		if !prefixed {
			r.look(literalIncorrect, "b|B")
			r.suggestAlternatives("bB", "sS", "iI", "lL")
		}
		width = 16
	case r.at("iI") != 0:
		r.pos++
		width = 32
	case r.at("lL") != 0:
		r.pos++
		width = 64
	default:
		if decimal && r.at("fFdD") != 0 {
			v, err := r.floatSuffix(sign, digits, "")
			return v, defaultWidth, err
		}
		width = defaultWidth
	}
	r.skipSpace()
	if unsigned && sign == "-" {
		return nil, width, &ParseError{Offset: r.pos, Key: errNonNegative, msg: "an unsigned number carries no minus"}
	}
	v, err := r.integerOf(sign, base, digits, width, unsigned)
	return v, width, err
}

// at answers the character at the reader if it is among letters, else 0.
func (r *snbtReader) at(letters string) byte {
	if r.pos < len(r.src) && strings.IndexByte(letters, r.src[r.pos]) >= 0 {
		return r.src[r.pos]
	}
	return 0
}

// integerOf parses digits at width, refusing an unsigned value under a minus
// sign and reporting an overflow with the value and radix Java's parse
// failure carries. vanilla: SnbtGrammar.IntegerLiteral.create,
// NumberFormatException
func (r *snbtReader) integerOf(sign string, base int, digits string, width int, unsigned bool) (Tag, error) {
	if unsigned {
		n, err := strconv.ParseUint(digits, base, width)
		if err != nil {
			return nil, r.failAt(r.pos, errNumberParseFailure, "number out of range",
				numberRangeMessage(sign, digits, base))
		}
		return r.integerOfWidth(int64(n), width), nil
	}
	n, err := strconv.ParseInt(sign+digits, base, width)
	if err != nil {
		return nil, r.failAt(r.pos, errNumberParseFailure, "number out of range",
			numberRangeMessage(sign, digits, base))
	}
	return r.integerOfWidth(n, width), nil
}

// numberRangeMessage spells an overflow as Java's NumberFormatException
// does: the value and the radix.
func numberRangeMessage(sign, digits string, base int) string {
	return "Value out of range. Value:\"" + sign + digits + "\" Radix:" + strconv.Itoa(base)
}

// integerOfWidth wraps a parsed integer as the tag its width named.
func (r *snbtReader) integerOfWidth(n int64, width int) Tag {
	switch width {
	case 8:
		return Byte(int8(n))
	case 16:
		return Short(int16(n))
	case 32:
		return Int(int32(n))
	default:
		return Long(n)
	}
}

// floatSuffix reads a float's optional exponent and f or d suffix (none is
// a double). An exponent letter without digits is not taken, and only the
// missing numeral is expected there. The exponent letter and f/d are
// recorded, so a float's rules are reported before a missing separator.
// vanilla: SnbtGrammar.createFloat, float_exponent_part
func (r *snbtReader) floatSuffix(sign, whole, fraction string) (Tag, error) {
	hadExponent := false
	var text strings.Builder
	if sign == "-" {
		text.WriteByte('-')
	}
	if whole == "" {
		text.WriteByte('0')
	}
	text.WriteString(whole)
	if fraction != "" {
		text.WriteByte('.')
		text.WriteString(fraction)
	}
	if r.pos < len(r.src) && (r.src[r.pos] == 'e' || r.src[r.pos] == 'E') {
		mark := r.pos
		hadExponent = true
		r.pos++
		r.skipSpace()
		r.look(literalIncorrect, "+")
		r.look(literalIncorrect, "-")
		r.suggestAlternatives("+", "-")
		var exp strings.Builder
		if r.pos < len(r.src) && (r.src[r.pos] == '+' || r.src[r.pos] == '-') {
			exp.WriteByte(r.src[r.pos])
			r.pos++
		}
		r.skipSpace()
		digits, edge := r.digitRun(10)
		if edge {
			return nil, r.fail(errDecimalNumeral, "a numeral carries no underscore at its edge")
		}
		if digits == "" {
			// No digits: the exponent is not taken, but the numeral is
			// still expected here.
			r.lookAt(r.pos, errDecimalNumeral)
			r.pos = mark
		} else {
			text.WriteByte('e')
			text.WriteString(exp.String())
			text.WriteString(digits)
		}
	}
	r.skipSpace()
	r.look(literalIncorrect, "e|E")
	r.look(literalIncorrect, "f|d")
	if !hadExponent {
		r.suggestAlternatives("eE")
	}
	r.suggestAlternatives("fF", "dD")
	suffix := byte('d')
	if r.at("fFdD") != 0 {
		suffix = r.src[r.pos]
		r.pos++
	}
	bits := 64
	if suffix == 'f' || suffix == 'F' {
		bits = 32
	}
	f, err := strconv.ParseFloat(text.String(), bits)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return nil, r.fail(errInfinity, "bad float value")
	}
	if bits == 32 {
		return Float(float32(f)), nil
	}
	return Double(f), nil
}
