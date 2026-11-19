package command

import (
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/registry"
)

// blockPropertyCompletions completes an open property list: the text after
// the last '[' or ',' answers an offset into partial and the matches. An
// unknown block or tag, or a malformed earlier entry, suggests nothing; a
// closed list suggests only '{'. Whitespace is skipped. With tags the id may
// be a #tag, offering its blocks' properties together.
// vanilla: BlockStateParser.fillSuggestions
func blockPropertyCompletions(partial string, tags bool) (int, []string) {
	start, matches, _ := blockPropertyAnswer(partial, tags)
	return start, matches
}

// blockPropertyIntegers are the matches blockPropertyCompletions gives for
// partial that are values of an integer property.
func blockPropertyIntegers(partial string, tags bool) []string {
	_, _, ints := blockPropertyAnswer(partial, tags)
	return ints
}

// blockPropertyAnswer is blockPropertyCompletions plus the matches that are
// integer property values.
func blockPropertyAnswer(partial string, tags bool) (int, []string, []string) {
	open := strings.IndexByte(partial, '[')
	id := partial[:open]
	var blocks []string
	if rest, isTag := strings.CutPrefix(id, "#"); isTag {
		if tags {
			blocks, _ = registry.BlockTagMembers(withMinecraftNamespace(rest))
		}
	} else if name := withMinecraftNamespace(id); slices.Contains(registry.BlockNames, name) {
		blocks = []string{name}
	}
	if len(blocks) == 0 {
		return 0, nil, nil
	}
	exact := !strings.HasPrefix(id, "#")

	set := map[string]bool{}
	entries := strings.Split(partial[open+1:], ",")
	start := open + 1
	for _, done := range entries[:len(entries)-1] {
		key, value, ok := strings.Cut(done, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || set[key] || (exact && !blockHasPropertyValue(blocks[0], key, value)) {
			return 0, nil, nil
		}
		set[key] = true
		start += len(done) + 1
	}
	last := entries[len(entries)-1]
	// vanilla: StringReader.skipWhitespace, between every token of the list
	lead := len(last) - len(strings.TrimLeft(last, " "))
	start += lead
	last = last[lead:]
	if strings.HasSuffix(last, "]") && strings.Count(last, "]") == 1 {
		start, matches := closedListCompletions(partial, blocks, set, strings.TrimSpace(last[:len(last)-1]), exact)
		return start, matches, nil
	}
	if strings.Contains(last, "]") {
		return 0, nil, nil
	}
	key, value, hasValue := strings.Cut(last, "=")
	if !hasValue {
		bare := strings.TrimRight(key, " ")
		if exact && !set[bare] && slices.Contains(registry.BlockPropertyNames(blocks[0]), bare) {
			return len(partial), []string{"="}, nil
		}
		if bare != key {
			return 0, nil, nil
		}
		return start, propertyNameCompletions(blocks, set, key, len(entries) == 1), nil
	}
	key = strings.TrimRight(key, " ")
	valueStart := start + len(last) - len(value)
	lead = len(value) - len(strings.TrimLeft(value, " "))
	valueStart += lead
	value = value[lead:]
	whole := strings.TrimRight(value, " ")
	if set[key] || strings.Contains(value, "=") {
		return 0, nil, nil
	}
	if exact {
		if _, ok := registry.BlockPropertyOrder(blocks[0], key); !ok {
			return 0, nil, nil
		}
		if blockHasPropertyValue(blocks[0], key, whole) {
			// A value that is whole offers what may follow it.
			set[key] = true
			var next []string
			if len(set) < len(registry.BlockPropertyNames(blocks[0])) {
				next = append(next, ",")
			}
			return len(partial), append([]string{"]"}, next...), nil
		}
	}
	if whole != value {
		if exact || whole == "" {
			return 0, nil, nil
		}
		set[key] = true
		next := []string{"]"}
		if tagHasUnsetProperty(blocks, set) {
			next = append(next, ",")
		}
		return len(partial), next, nil
	}
	values := propertyValues(blocks, key)
	ints := integerValues(values)
	if exact {
		return valueStart, values, ints
	}
	// vanilla: BlockStateParser.suggestVaguePropertyValue
	set[key] = true
	if tagHasUnsetProperty(blocks, set) {
		values = append(values, ",")
	}
	values = append(values, "]")
	return valueStart, slices.DeleteFunc(values, func(s string) bool { return s == value }), ints
}

// closedListCompletions answers a property list that ends the text with its
// `]`, whose last entry is last: a `{` when a block can carry block entity
// data, and nothing after a list that does not hold up.
// vanilla: BlockStateParser.suggestOpenNbt
func closedListCompletions(partial string, blocks []string, set map[string]bool, last string, exact bool) (int, []string) {
	if last != "" {
		key, value, ok := strings.Cut(last, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || set[key] || (exact && !blockHasPropertyValue(blocks[0], key, value)) {
			return 0, nil
		}
	}
	for _, block := range blocks {
		if _, ok := registry.BlockEntityTypes[block]; ok {
			return len(partial), []string{"{"}
		}
	}
	return 0, nil
}

// withMinecraftNamespace is name with the default namespace put in front of a
// name that has none.
func withMinecraftNamespace(name string) string {
	if strings.Contains(name, ":") {
		return name
	}
	return "minecraft:" + name
}

// blockHasPropertyValue reports whether the block's property takes the value.
func blockHasPropertyValue(block, key, value string) bool {
	values, ok := registry.BlockPropertyValues(block, key)
	return ok && values[value]
}

// propertyNameCompletions are the `name=` suggestions for a property name
// being typed: each property of the blocks that is not set yet and starts
// with the typed prefix, preceded by `]` when nothing is typed after the
// bracket. A name that is whole offers the `=` behind it instead.
// vanilla: BlockStateParser.suggestPropertyNameOrEnd
func propertyNameCompletions(blocks []string, set map[string]bool, prefix string, first bool) []string {
	var out []string
	if prefix == "" && first {
		out = append(out, "]")
	}
	prefix = strings.ToLower(prefix)
	for _, block := range blocks {
		for _, name := range registry.BlockPropertyNames(block) {
			if set[name] || !strings.HasPrefix(name, prefix) {
				continue
			}
			out = append(out, name+"=")
		}
	}
	return slices.Compact(slices.Sorted(slices.Values(out)))
}

// propertyValues are the values the blocks accept for a property, without
// repeats, in the order the first block listing them gives.
func propertyValues(blocks []string, key string) []string {
	var out []string
	for _, block := range blocks {
		order, _ := registry.BlockPropertyOrder(block, key)
		for _, v := range order {
			if !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
	}
	return out
}

// tagHasUnsetProperty reports whether some block of the tag has a property
// the list has not named yet.
func tagHasUnsetProperty(blocks []string, set map[string]bool) bool {
	for _, block := range blocks {
		for _, name := range registry.BlockPropertyNames(block) {
			if !set[name] {
				return true
			}
		}
	}
	return false
}

// integerValues are the values that are whole numbers, which vanilla offers
// as integer suggestions.
// vanilla: BlockStateParser.addSuggestions
func integerValues(values []string) []string {
	var ints []string
	for _, v := range values {
		if _, err := strconv.Atoi(v); err == nil {
			ints = append(ints, v)
		}
	}
	return ints
}
