package command

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// itemStackCompletions follows the item reader's suggestion state through components.
// vanilla: ItemParser.State, ItemParser.SuggestionsVisitor
func itemStackCompletions(partial string) (int, []string) {
	name, p, ok := readIdentifierNonEmpty(partial)
	if !ok {
		return 0, ResourceSuggestions(partial, registry.Items...)
	}
	if _, known := registry.ItemID(name); !known {
		return 0, ResourceSuggestions(partial, registry.Items...)
	}
	if p == len(partial) {
		return p, []string{"["}
	}
	if partial[p] != '[' {
		return p, nil
	}
	p++
	seen := make(map[string]bool)
	for {
		p = skipSpace(partial, p)
		if p < len(partial) && partial[p] == ']' {
			return p + 1, nil
		}
		removed := p < len(partial) && partial[p] == '!'
		if removed {
			p++
		}
		start := p
		key, end, fail := readComponentName(joinedSelector{text: partial, tokens: []Token{{Text: partial}}, starts: []int{0}}, p)
		if fail != nil {
			return start, componentNameSuggestions(partial[start:], removed)
		}
		p = end
		if seen[key] {
			return p, nil
		}
		seen[key] = true
		p = skipSpace(partial, p)
		if !removed {
			if p == len(partial) {
				return p, []string{"="}
			}
			if partial[p] != '=' {
				return p, nil
			}
			p = skipSpace(partial, p+1)
			valueStart := p
			tag, end, err := nbt.ParseValueTag(partial[p:])
			if err != nil || componentValueTagMalformed(key, tag) != "" {
				return valueStart, nil
			}
			p = skipSpace(partial, p+end)
		}
		if p == len(partial) {
			return p, []string{",", "]"}
		}
		if partial[p] != ',' {
			return p, nil
		}
		p++
	}
}

func componentNameSuggestions(partial string, removed bool) []string {
	var candidates []string
	for name := range registry.ItemComponents {
		if !registry.TransientItemComponents[name] {
			candidates = append(candidates, name)
		}
	}
	matches := ResourceSuggestions(partial, candidates...)
	if !removed {
		for i := range matches {
			matches[i] += "="
		}
		matches = append(matches, "!")
	}
	return matches
}
