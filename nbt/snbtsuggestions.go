package nbt

import (
	"slices"
	"strings"
)

type snbtCompletion struct {
	offset int
	values []string
}

// ValueSuggestions returns SNBT grammar completions and their replacement offset.
// vanilla: Grammar.parseForSuggestions, SnbtGrammar
func ValueSuggestions(partial string) (int, []string) {
	c := &snbtCompletion{offset: -1}
	r := snbtReader{src: partial, far: -1, completion: c}
	r.skipSpace()
	_, _ = r.value()
	if c.offset < 0 || c.offset > len(partial) {
		return 0, nil
	}
	remaining := partial[c.offset:]
	matches := slices.DeleteFunc(c.values, func(value string) bool {
		return value == remaining || !strings.HasPrefix(strings.ToLower(value), strings.ToLower(remaining))
	})
	return c.offset, matches
}

func (r *snbtReader) suggestAt(pos int, values ...string) {
	if r.completion != nil {
		r.completion.expect(pos, values...)
	}
}

// advance stays small so ordinary parser expectations can still be inlined.
func (c *snbtCompletion) advance(pos int) {
	if c != nil && pos > c.offset {
		c.offset = pos
		c.values = c.values[:0]
	}
}

// Keep completion collection outside the ordinary reader's inline budget.
//
//go:noinline
func (c *snbtCompletion) expect(pos int, values ...string) {
	if pos < c.offset {
		return
	}
	c.advance(pos)
	for _, value := range values {
		if !slices.Contains(c.values, value) {
			c.values = append(c.values, value)
		}
	}
}

// suggestAlternatives records only branches tried before a matching character.
func (r *snbtReader) suggestAlternatives(groups ...string) {
	if r.completion != nil {
		r.suggestAlternativesAt(r.pos, groups...)
	}
}

func (r *snbtReader) suggestAlternativesAt(pos int, groups ...string) {
	if r.completion == nil {
		return
	}
	for pos < len(r.src) && strings.ContainsRune(" \t\n\r", rune(r.src[pos])) {
		pos++
	}
	for _, group := range groups {
		if pos < len(r.src) && strings.IndexByte(group, r.src[pos]) >= 0 {
			return
		}
		for _, c := range group {
			r.suggestAt(pos, string(c))
		}
	}
}

func (r *snbtReader) suggestNext(pos int, values ...string) {
	if r.completion == nil {
		return
	}
	for pos < len(r.src) && strings.ContainsRune(" \t\n\r", rune(r.src[pos])) {
		pos++
	}
	r.suggestAt(pos, values...)
}
