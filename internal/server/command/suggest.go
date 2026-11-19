package command

import (
	"cmp"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Suggest completes the last word of text, returning where the replacement
// starts and the matches. tree is the caller's per-player tree, so a non-op
// is offered no /time; ctx is passed to each argument's Completions with the
// words already filled in (see suggestState).
func Suggest(tree *Node, text string, ctx any) (start int, matches []string) {
	// The slash is skipped only when the text has one: the client may send
	// an empty or slash-less text, which is read from its first byte.
	// vanilla: ServerGamePacketListenerImpl.handleCustomCommandSuggestions
	base := 0
	if strings.HasPrefix(text, "/") {
		base = 1
	}
	// The last word starts after the last space, or after the slash if
	// there is none.
	lastSpace := strings.LastIndex(text, " ")
	wordStart := base
	if lastSpace >= 0 {
		wordStart = lastSpace + 1
	}

	// state is the ctx the callbacks see: whoever asked, plus what the
	// completed words filled the arguments with. Only a ctx that is a
	// Source is wrapped; anything else goes through as it came.
	state := ctx
	filled := map[string]string{}
	if src, ok := ctx.(Source); ok {
		state = &suggestState{Source: src, args: filled}
	}

	// Walk the tree through the words that are already complete — everything
	// between the slash and the last space. An argument that has not had all
	// the words it reads yet is the one the partial belongs to.
	node := tree
	// partialStart is where the text the completing argument replaces from
	// begins: the last word, or the whole selector a selector argument is
	// still reading across the spaces.
	partialStart := wordStart
	if lastSpace >= base {
		words := Tokenize(text[base:wordStart])
		for i := 0; i < len(words); {
			// An alias continues at its redirect target.
			for node.Redirect != nil {
				node = node.Redirect
			}
			child := FindLiteralChild(node, words[i].Text)
			if child == nil {
				arg := argumentChild(node, words[i].Text)
				if arg == nil {
					return wordStart, nil
				}
				if arg.EntityArg != nil || arg.Selector != nil {
					// A selector still open at the last whole word reads
					// on into the partial, so its own completions answer
					// for the whole selector.
					if _, open := scanSelector(words[i:], tokenText); open {
						partialStart = words[i].Start + base
						break
					}
				}
				if arg.Greedy && arg.InputCompletions != nil {
					// A greedy argument stays open through every word up to the cursor.
					partialStart = words[i].Start + base
					break
				}
				if arg.Quotable && arg.InputCompletions != nil {
					if _, _, err := readStringValue(text[words[i].Start+base : wordStart]); err != nil {
						partialStart = words[i].Start + base
						break
					}
				}
				arity := ArgumentArity(arg, words[i:])
				if arg.Parser == ParserBlockPredicate {
					// ArgumentArity leaves a predicate at one word; its
					// property list may be parted by spaces like a state's.
					arity = blockStateSpanCount(words[i:])
				}
				if i+arity > len(words) {
					// A custom provider completes the whole unfinished argument.
					if arg.InputCompletions == nil {
						return wordStart, nil
					}
					partialStart = words[i].Start + base
					break
				}
				if i+arity == len(words) && (arg.Parser == ParserBlockState || arg.Parser == ParserBlockPredicate) &&
					strings.Count(text[words[i].Start+base:], "[") > strings.Count(text[words[i].Start+base:], "]") {
					// A property list still open at the last whole word may
					// be parted by spaces, so the argument's own completions
					// answer for all of it.
					partialStart = words[i].Start + base
					break
				}
				if i+arity == len(words) && arg.InputCompletions != nil {
					prefix := text[words[i].Start+base : wordStart]
					var incomplete bool
					switch arg.Parser {
					case ParserItemStack:
						incomplete = parseItemStackArgument(Tokenize(prefix)) != nil
					case ParserComponent:
						end, err := nbt.ParseValue(prefix)
						incomplete = err != nil || strings.TrimSpace(prefix[end:]) != "" || checkComponentArg(prefix) != nil
					}
					if incomplete {
						partialStart = words[i].Start + base
						break
					}
				}
				filled[arg.Name] = words[i].Text
				node = arg
				i += arity
				continue
			}
			node = child
			i++
		}
	}
	for node.Redirect != nil {
		node = node.Redirect
	}
	partial := text[partialStart:]

	// The offset a Completions callback answers with moves the replacement
	// range into the word, which only a selector's own stages do.
	at := 0
	var ints []string
	for _, child := range node.Children {
		switch child.Type {
		case NodeLiteral:
			if strings.HasPrefix(child.Name, partial) {
				matches = append(matches, child.Name)
			}
		case NodeArgument:
			// Answered whatever the provider is named: the client may
			// resolve a built-in provider itself, but a request that
			// reaches here still gets the names.
			if child.Suggests != "" && (child.Completions != nil || child.InputCompletions != nil) {
				var offset int
				var argMatches []string
				if child.InputCompletions != nil {
					if src, ok := ctx.(Source); ok {
						offset, argMatches = child.InputCompletions(src, text[base:], partial, ArgumentValues(tree, text[base:partialStart], src.PermLevel() >= PermLevelGamemasters))
					}
				} else {
					offset, argMatches = child.Completions(state, partial)
				}
				matches = append(matches, argMatches...)
				if child.IntegerMatches != nil {
					ints = append(ints, child.IntegerMatches(partial)...)
				}
				if offset != 0 {
					at = offset
				}
			}
		}
	}
	// The merged list is offered sorted, case-insensitively and without the
	// duplicates, literals and argument completions together, in the order
	// Brigadier hands its merged suggestions out in. Two integer suggestions
	// compare by value, anything else by text.
	// vanilla: Suggestions.create, IntegerSuggestion.compareToIgnoreCase
	slices.SortStableFunc(matches, func(a, b string) int {
		if slices.Contains(ints, a) && slices.Contains(ints, b) {
			x, _ := strconv.Atoi(a)
			y, _ := strconv.Atoi(b)
			return cmp.Compare(x, y)
		}
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
	matches = slices.Compact(matches)
	return partialStart + at, matches
}

// SuggestionRange is the range a Command Suggestions packet carries for the
// replacement Suggest found at byte offset start of text: its start and its
// length to the end of text, both in UTF-16 code units, the indices of the
// Java string the client's Brigadier reads.
// vanilla: StringRange (ClientboundCommandSuggestionsPacket)
func SuggestionRange(text string, start int) (from, length int) {
	return utf16Length(text[:start]), utf16Length(text[start:])
}

// argumentChild is the argument child a word goes to when no literal matches:
// the first one, except that a word beginning with @ is a selector, which only
// an entity or score-holder argument reads.
func argumentChild(node *Node, word string) *Node {
	var first *Node
	for _, child := range node.Children {
		if child.Type != NodeArgument {
			continue
		}
		if first == nil {
			first = child
		}
		if word != "" && word[0] == '@' && (child.EntityArg != nil || child.Selector != nil) {
			return child
		}
	}
	return first
}

// wordTexts gives the words of tokens, which is the shape selectorScan reads.
func wordTexts(tokens []Token) []string {
	words := make([]string, len(tokens))
	for i, token := range tokens {
		words[i] = token.Text
	}
	return words
}

// suggestState is the ctx a completions callback reads: whoever asked, plus
// the words the completed part of the line filled the arguments with, keyed
// by the argument node's name — a completion that depends on another
// argument's value, a timeline by its clock, reads them here.
type suggestState struct {
	Source
	args map[string]string
}

// arg is the word the walk filled the named argument with, if the line
// reached past it.
func (s *suggestState) arg(name string) (string, bool) {
	word, ok := s.args[name]
	return word, ok
}

// ResourceCompletions are what a resource argument offers: the entries of
// the registry the argument reads from, filtered by the vanilla match and
// in the vanilla order (see ResourceSuggestions).
// vanilla: SharedSuggestionProvider.suggestResource
func ResourceCompletions(registryName string) func(any, string) (int, []string) {
	return func(_ any, partial string) (int, []string) {
		r, ok := registry.Get(registryName)
		if !ok {
			return 0, nil
		}
		return 0, ResourceSuggestions(partial, r.Entries...)
	}
}

// entityTargets, playerTargets, oneEntity, onePlayer and profileTargets are
// the shapes an entity argument node takes, the flags the vanilla server
// writes in the node's properties byte and checks its selectors against.
func entityTargets() *EntityArg  { return &EntityArg{} }
func profileTargets() *EntityArg { return &EntityArg{PlayersOnly: true, Profile: true} }

// EntityCompletions are what an entity argument offers: the online players
// and the selectors where no @ has been typed, and the inside of a selector
// where one has. The offset returned is where in the partial word the
// matches replace from, which the selector's own stages move inside the
// brackets; the rest of the stages' matches carry the whole word.
func EntityCompletions(ctx any, partial string) (int, []string) {
	src, ok := ctx.(Source)
	if !ok {
		return 0, nil
	}
	return SuggestSelector(src, partial)
}

// Suggestions filters candidates down to the ones a partial word could still
// become, in the order they are given.
func Suggestions(partial string, candidates ...string) []string {
	var matches []string
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, partial) {
			matches = append(matches, candidate)
		}
	}
	return slices.Clip(matches)
}

// ResourceSuggestions filters registry names a partial word could still
// become: matching at the start of the name, of the path after its
// namespace, or after any '.', '_' or '/', case folded; sorted, as
// Brigadier hands them out.
//
// vanilla: SharedSuggestionProvider.filterResources
func ResourceSuggestions(partial string, candidates ...string) []string {
	var matches []string
	for _, candidate := range candidates {
		if matchesSubStr(partial, candidate) || matchesSubStr(partial, resourcePath(candidate)) {
			matches = append(matches, candidate)
		}
	}
	slices.Sort(matches)
	return slices.Clip(matches)
}

// resourcePath is the path of a resource name, the part behind the namespace.
func resourcePath(name string) string {
	if _, after, ok := strings.Cut(name, ":"); ok {
		return after
	}
	return name
}

// prefixedResourceSuggestions filters a pool whose words carry a prefix (block
// tags' '#'): an empty partial offers all, a partial reaches the pool only
// through the prefix, which is put back on the matches; a partial not
// sharing the prefix's start returns nothing.
//
// vanilla: SharedSuggestionProvider.filterResources
func prefixedResourceSuggestions(prefix, partial string, candidates ...string) []string {
	common := commonPrefix(partial, prefix)
	if partial != "" && common == "" {
		return nil
	}
	matches := ResourceSuggestions(partial[len(common):], candidates...)
	for i := range matches {
		matches[i] = prefix + matches[i]
	}
	return matches
}
