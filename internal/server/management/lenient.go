package management

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// lenientNesting is Gson's default JsonReader nesting limit.
const lenientNesting = 255

// nestedDeeper reports whether the valid JSON v nests arrays and objects more
// than limit levels deep.
func nestedDeeper(v []byte, limit int) bool {
	depth, inString := 0, false
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case inString && c == '\\':
			i++
		case c == '"':
			inString = !inString
		case inString:
		case c == '[' || c == '{':
			if depth++; depth > limit {
				return true
			}
		case c == ']' || c == '}':
			depth--
		}
	}
	return false
}

// parseLenient reads msg as Gson's JsonParser.parseString does and returns it
// as strict JSON: an empty message is null, and comments, unquoted and
// single-quoted strings and names, '=' or '=>' after a name, ';' between
// entries and empty array entries (null) are accepted. Content after the
// value is an error unless the value is null.
// vanilla: WebSocketToJsonCodec.decode, JsonParser.parseReader
func parseLenient(msg []byte) (json.RawMessage, error) {
	r := &lenientReader{in: msg}
	// Only the first read may find the message empty, and the prefix is
	// skipped inside it, so a message of only the prefix is null too.
	// vanilla: Streams.parse (isEmpty), JsonReader.consumeNonExecutePrefix
	for skippedPrefix := false; ; skippedPrefix = true {
		if _, err := r.nextNonWhitespace(); err == errLenientEOF {
			return nullID, nil
		} else if err != nil {
			return nil, err
		}
		r.pos--
		if skippedPrefix || !bytes.HasPrefix(msg[r.pos:], nonExecutePrefix) {
			break
		}
		r.pos += len(nonExecutePrefix)
	}
	var out bytes.Buffer
	isNull, err := r.value(&out, 0, false)
	if err != nil {
		return nil, err
	}
	if !isNull {
		// JsonParser checks the end with the reader's strictness restored, so
		// trailing comments are an error too.
		rest := strings.TrimLeft(string(msg[r.pos:]), " \t\r\n")
		if rest != "" {
			return nil, r.errorf("Use JsonReader.setStrictness(Strictness.LENIENT) to accept malformed JSON")
		}
	}
	return out.Bytes(), nil
}

// nonExecutePrefix is skipped at the start of a lenient document.
// vanilla: JsonReader.consumeNonExecutePrefix
var nonExecutePrefix = []byte(")]}'\n")

// lenientReader is Gson's JsonReader in Strictness.LENIENT.
type lenientReader struct {
	in  []byte
	pos int
}

var errLenientEOF = errors.New("end of input")

func (r *lenientReader) errorf(format string, a ...any) error {
	return fmt.Errorf(format+" at offset %d", append(a, r.pos)...)
}

// nextNonWhitespace consumes and returns the next byte that is neither
// whitespace nor part of a comment, or errLenientEOF at the end of input.
// vanilla: JsonReader.nextNonWhitespace
func (r *lenientReader) nextNonWhitespace() (byte, error) {
	for r.pos < len(r.in) {
		c := r.in[r.pos]
		r.pos++
		switch {
		case c == ' ' || c == '\n' || c == '\r' || c == '\t':
			continue
		case c == '#' || c == '/' && r.pos < len(r.in) && r.in[r.pos] == '/':
			r.skipToEndOfLine()
			continue
		case c == '/' && r.pos < len(r.in) && r.in[r.pos] == '*':
			end := bytes.Index(r.in[r.pos+1:], []byte("*/"))
			if end < 0 {
				return 0, r.errorf("Unterminated comment")
			}
			r.pos += 1 + end + 2
			continue
		}
		return c, nil
	}
	return 0, errLenientEOF
}

func (r *lenientReader) skipToEndOfLine() {
	for r.pos < len(r.in) {
		c := r.in[r.pos]
		r.pos++
		if c == '\n' || c == '\r' {
			return
		}
	}
}

// isLiteral reports whether c may continue an unquoted literal.
// vanilla: JsonReader.isLiteral
func isLiteral(c byte) bool {
	switch c {
	case '#', '/', ';', '=', '\\', '\t', '\n', '\f', '\r', ' ', ',', ':', '[', ']', '{', '}':
		return false
	}
	return true
}

// value writes the next value as strict JSON and reports whether it was null.
// inArray makes ',', ';' and ']' an empty entry, read as null.
// vanilla: JsonReader.doPeek, JsonElementTypeAdapter.read
func (r *lenientReader) value(out *bytes.Buffer, depth int, inArray bool) (isNull bool, err error) {
	c, err := r.nextNonWhitespace()
	if err != nil {
		return false, err
	}
	switch c {
	case ']', ',', ';':
		if !inArray {
			return false, r.errorf("Unexpected value")
		}
		r.pos--
		out.WriteString("null")
		return true, nil
	case '"', '\'':
		s, err := r.quoted(c)
		if err != nil {
			return false, err
		}
		writeString(out, s)
		return false, nil
	case '[':
		return false, r.array(out, depth+1)
	case '{':
		return false, r.object(out, depth+1)
	}
	r.pos--
	if kw := r.keyword(); kw != "" {
		out.WriteString(kw)
		return kw == "null", nil
	}
	if n := r.number(); n != "" {
		out.WriteString(n)
		return false, nil
	}
	if !isLiteral(c) {
		return false, r.errorf("Expected value")
	}
	writeString(out, r.unquoted())
	return false, nil
}

func (r *lenientReader) array(out *bytes.Buffer, depth int) error {
	if depth > lenientNesting {
		return r.errorf("Nesting limit %d reached", lenientNesting)
	}
	out.WriteByte('[')
	c, err := r.nextNonWhitespace()
	if err != nil {
		return err
	}
	if c == ']' {
		out.WriteByte(']')
		return nil
	}
	r.pos--
	for first := true; ; first = false {
		if !first {
			out.WriteByte(',')
		}
		if _, err := r.value(out, depth, true); err != nil {
			return err
		}
		c, err := r.nextNonWhitespace()
		switch {
		case err != nil:
			return err
		case c == ']':
			out.WriteByte(']')
			return nil
		case c != ',' && c != ';':
			return r.errorf("Unterminated array")
		}
	}
}

func (r *lenientReader) object(out *bytes.Buffer, depth int) error {
	if depth > lenientNesting {
		return r.errorf("Nesting limit %d reached", lenientNesting)
	}
	out.WriteByte('{')
	for first := true; ; first = false {
		c, err := r.nextNonWhitespace()
		if err != nil {
			return err
		}
		var name string
		switch {
		case c == '}' && first:
			out.WriteByte('}')
			return nil
		case c == '"' || c == '\'':
			if name, err = r.quoted(c); err != nil {
				return err
			}
		case isLiteral(c):
			r.pos--
			name = r.unquoted()
		default:
			return r.errorf("Expected name")
		}
		if !first {
			out.WriteByte(',')
		}
		writeString(out, name)
		out.WriteByte(':')

		switch c, err = r.nextNonWhitespace(); {
		case err != nil:
			return err
		case c == '=':
			if r.pos < len(r.in) && r.in[r.pos] == '>' {
				r.pos++
			}
		case c != ':':
			return r.errorf("Expected ':'")
		}
		if _, err := r.value(out, depth, false); err != nil {
			return err
		}

		switch c, err = r.nextNonWhitespace(); {
		case err != nil:
			return err
		case c == '}':
			out.WriteByte('}')
			return nil
		case c != ',' && c != ';':
			return r.errorf("Unterminated object")
		}
	}
}

// keyword reads true, false or null in any mix of case, not followed by a
// literal character, and returns it in lower case, or "" for none.
// vanilla: JsonReader.peekKeyword
func (r *lenientReader) keyword() string {
	for _, kw := range [...]string{"true", "false", "null"} {
		if r.in[r.pos]|0x20 != kw[0] {
			continue
		}
		end := r.pos + len(kw)
		if end > len(r.in) {
			return ""
		}
		for i := range len(kw) {
			if b := r.in[r.pos+i]; b != kw[i] && b != kw[i]-'a'+'A' {
				return ""
			}
		}
		if end < len(r.in) && isLiteral(r.in[end]) {
			return ""
		}
		r.pos = end
		return kw
	}
	return ""
}

// number reads a JSON number not followed by a literal character and returns
// its text, or "" for none. A leading zero followed by a digit is not a
// number, so the text is always strict JSON.
// vanilla: JsonReader.peekNumber
func (r *lenientReader) number() string {
	const (
		none = iota
		sign
		digit
		decimal
		fractionDigit
		expE
		expSign
		expDigit
	)
	last, leadingZero := none, false
	i := r.pos
scan:
	for ; i < len(r.in); i++ {
		switch c := r.in[i]; {
		case c == '-' && last == none:
			last = sign
		case (c == '-' || c == '+') && last == expE:
			last = expSign
		case (c == 'e' || c == 'E') && (last == digit || last == fractionDigit):
			last = expE
		case c == '.' && last == digit:
			last = decimal
		case c >= '0' && c <= '9':
			switch last {
			case none, sign:
				last, leadingZero = digit, c == '0'
			case digit:
				if leadingZero {
					return ""
				}
			case decimal:
				last = fractionDigit
			case expE, expSign:
				last = expDigit
			}
		case !isLiteral(c):
			break scan
		default:
			return ""
		}
	}
	if last != digit && last != fractionDigit && last != expDigit {
		return ""
	}
	s := string(r.in[r.pos:i])
	r.pos = i
	return s
}

// unquoted reads a literal up to the next non-literal character.
// vanilla: JsonReader.nextUnquotedValue
func (r *lenientReader) unquoted() string {
	start := r.pos
	for r.pos < len(r.in) && isLiteral(r.in[r.pos]) {
		r.pos++
	}
	return string(r.in[start:r.pos])
}

// quoted reads a string after its opening quote.
// vanilla: JsonReader.nextQuotedValue, readEscapeCharacter
func (r *lenientReader) quoted(quote byte) (string, error) {
	var units []uint16
	start := r.pos
	flush := func(end int) {
		units = append(units, utf16.Encode([]rune(string(r.in[start:end])))...)
	}
	for r.pos < len(r.in) {
		c := r.in[r.pos]
		r.pos++
		switch c {
		case quote:
			flush(r.pos - 1)
			return string(utf16.Decode(units)), nil
		case '\\':
			flush(r.pos - 1)
			u, err := r.escape()
			if err != nil {
				return "", err
			}
			units = append(units, u)
			start = r.pos
		}
	}
	return "", r.errorf("Unterminated string")
}

func (r *lenientReader) escape() (uint16, error) {
	if r.pos == len(r.in) {
		return 0, r.errorf("Unterminated escape sequence")
	}
	c := r.in[r.pos]
	r.pos++
	switch c {
	case 'u':
		if r.pos+4 > len(r.in) {
			return 0, r.errorf("Unterminated escape sequence")
		}
		var v uint16
		for _, h := range r.in[r.pos : r.pos+4] {
			switch {
			case h >= '0' && h <= '9':
				v = v<<4 | uint16(h-'0')
			case h >= 'a' && h <= 'f':
				v = v<<4 | uint16(h-'a'+10)
			case h >= 'A' && h <= 'F':
				v = v<<4 | uint16(h-'A'+10)
			default:
				return 0, r.errorf("Malformed Unicode escape \\u%s", r.in[r.pos:r.pos+4])
			}
		}
		r.pos += 4
		return v, nil
	case 't':
		return '\t', nil
	case 'b':
		return '\b', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 'f':
		return '\f', nil
	case '\n', '\'', '"', '/', '\\':
		return uint16(c), nil
	}
	return 0, r.errorf("Invalid escape sequence")
}

func writeString(out *bytes.Buffer, s string) {
	b, _ := json.Marshal(s) // a string always encodes
	out.Write(b)
}
