package command

import (
	"slices"
	"strings"

	"github.com/golem-mc/golem/nbt"
)

// The three ways reading a summon's compound tag can go: it is not a compound
// at all, it is one with something left over after it, or its shape does not
// read as SNBT. A compound that reads is the fourth answer.
type nbtRead int

const (
	nbtIsCompound nbtRead = iota
	nbtNotCompound
	nbtTrailing
	nbtMalformed
)

// readSummonNBT reads a /summon's SNBT compound as the compound-tag argument
// does, answering the verdict and the top-level entries: scalars as their
// values' raw text, compounds and lists as their spans' raw text.
func readSummonNBT(text string) (nbtRead, map[string]string, map[string]string) {
	verdict, entries, spans, _ := readSummonTag(text)
	return verdict, entries, spans
}

// readSummonTag is readSummonNBT with the parsed compound beside the
// entries: the tag a mob's summon hands the reader a saved mob uses.
func readSummonTag(text string) (nbtRead, map[string]string, map[string]string, nbt.Compound) {
	r := &nbtReader{src: text, scalars: map[string]string{}, values: map[string]string{}, sep: ':', ordered: true}
	r.skipSpace()
	if !r.at('{') {
		return nbtNotCompound, nil, nil, nil
	}
	compound, ok := r.readCompound()
	if !ok {
		return nbtMalformed, nil, nil, nil
	}
	if r.skipSpace() {
		// Anything after the compound's close is data the argument does
		// not take, the vanilla parser's own trailing-data refusal.
		return nbtTrailing, nil, nil, nil
	}
	return nbtIsCompound, r.scalars, r.values, compound
}

// readComponentList reads the component list an item stack argument carries
// in its brackets, the same shape as a compound's entries with an equals
// sign where the compound's colon would be; the verdicts and the entries
// are the summon reader's own.
// The fourth result is the keys in first-appearance order, the order vanilla's
// parser applies them in.
// vanilla: ItemParser.readComponents
func readComponentList(text string) (nbtRead, map[string]string, map[string]string, []string) {
	r := &nbtReader{src: "{" + text + "}", scalars: map[string]string{}, values: map[string]string{}, sep: '=', keyColon: true}
	if _, ok := r.readCompound(); !ok {
		return nbtMalformed, nil, nil, nil
	}
	if r.skipSpace() {
		// Anything after the list's close is not part of the item.
		return nbtTrailing, nil, nil, nil
	}
	return nbtIsCompound, r.scalars, r.values, r.keys
}

// nbtReader tracks the outer compound or component list's raw entry spans;
// recursive values are read by nbt.
type nbtReader struct {
	src string
	pos int
	sep byte
	// keyColon is the component list's own key rule: the component's name
	// reads as a resource location, whose unquoted form carries a colon and
	// may carry slashes (painting/variant).
	// vanilla: Identifier.isAllowedInIdentifier
	ordered  bool
	keyColon bool
	scalars  map[string]string
	values   map[string]string
	keys     []string // outermost keys, first appearance
}

// note records an outermost key's first appearance.
func (r *nbtReader) note(key string) {
	if !slices.Contains(r.keys, key) {
		r.keys = append(r.keys, key)
	}
}

// at reports whether the next rune is the one given.
func (r *nbtReader) at(c byte) bool {
	return r.pos < len(r.src) && r.src[r.pos] == c
}

// skipSpace runs past the whitespace the grammar allows between the parts of
// a tag, and reports whether any text is left after it.
func (r *nbtReader) skipSpace() bool {
	for r.pos < len(r.src) {
		switch r.src[r.pos] {
		case ' ', '\t', '\n', '\r':
			r.pos++
		default:
			return true
		}
	}
	return false
}

// readCompound reads a SNBT compound: entries between braces, each a key, a
// colon and a value, separated by commas. The scalar entries of the outermost
// one are kept, which is what the depth counts.
func (r *nbtReader) readCompound() (nbt.Compound, bool) {
	out := nbt.Compound{}
	r.pos++ // the '{'
	if !r.skipSpace() {
		return nil, false
	}
	if r.at('}') {
		r.pos++
		return out, true
	}
	for {
		if !r.skipSpace() {
			return nil, false
		}
		// A component list's entry behind '!' removes the component and
		// carries no value.
		// vanilla: ItemParser.readComponents
		removal := r.keyColon && r.at('!')
		if removal {
			r.pos++
		}
		key, ok := r.readKey()
		if !ok || key == "" {
			return nil, false
		}
		if removal {
			r.note("!" + key)
			r.scalars["!"+key] = "!"
			out["!"+key] = nbt.Compound{}
			if !r.skipSpace() {
				return nil, false
			}
			switch {
			case r.at(','):
				r.pos++
				continue
			case r.at('}'):
				r.pos++
				return out, true
			}
			return nil, false
		}
		if !r.skipSpace() || !r.at(r.sep) {
			return nil, false
		}
		r.pos++
		if !r.skipSpace() {
			return nil, false
		}
		start := r.pos
		value, ok := r.readValue()
		if !ok {
			return nil, false
		}
		// An entry of the outermost compound: the raw text between its
		// colon and its comma or close. A scalar's text is what it
		// said; a compound's or a list's is its whole span.
		if text := strings.TrimSpace(r.src[start:r.pos]); text != "" {
			r.note(key)
			if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
				r.values[key] = text
			} else {
				r.scalars[key] = text
			}
		}
		out[key] = value
		if !r.skipSpace() {
			return nil, false
		}
		switch {
		case r.at(','):
			r.pos++
			// A comma may close the compound: the grammar's entry list
			// allows a trailing separator.
			if r.skipSpace() && r.at('}') {
				r.pos++
				return out, true
			}
		case r.at('}'):
			r.pos++
			return out, true
		default:
			return nil, false
		}
	}
}

// unquoted strips the quotes a quoted key carries, which is the only part of
// a key's text the map's reader cares to keep.
func unquoted(text string) string {
	if len(text) >= 2 && (text[0] == '"' || text[0] == '\'') && text[len(text)-1] == text[0] {
		return text[1 : len(text)-1]
	}
	return text
}

// readKey decodes a quoted key or reads a bare name.
func (r *nbtReader) readKey() (string, bool) {
	if r.at('"') || r.at('\'') {
		return r.readQuotedValue()
	}
	start := r.pos
	ok := r.readUnquoted()
	return r.src[start:r.pos], ok
}

// readValue delegates recursive SNBT values to nbt's strict grammar.
func (r *nbtReader) readValue() (nbt.Tag, bool) {
	// Bare resource locations belong only to the outer component value.
	if r.keyColon {
		start := r.pos
		r.readUnquoted()
		if text := r.src[start:r.pos]; strings.ContainsRune(text, ':') {
			return nbt.String(text), true
		}
		r.pos = start
	}
	parse := nbt.ParseValueTag
	if r.ordered {
		parse = nbt.ParseOrderedValueTag
	}
	value, n, err := parse(r.src[r.pos:])
	r.pos += n
	return value, err == nil
}

// readQuotedValue decodes a quoted key through the shared SNBT reader.
func (r *nbtReader) readQuotedValue() (string, bool) {
	value, ok := r.readValue()
	text, quoted := value.(nbt.String)
	return string(text), ok && quoted
}

// readUnquoted reads a bare token: the run of the letters, digits, dots,
// underscores, hyphens and plus-and-minus signs the grammar takes an
// unquoted string or a number from, up to the punctuation that ends it.
func (r *nbtReader) readUnquoted() bool {
	start := r.pos
	for r.pos < len(r.src) {
		c := r.src[r.pos]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
			c == '_' || c == '.' || c == '-' || c == '+' || (r.keyColon && (c == ':' || c == '/')) {
			r.pos++
			continue
		}
		break
	}
	return r.pos > start
}
