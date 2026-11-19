package command

import (
	"github.com/golem-mc/golem/internal/commandsyntax"
	"github.com/golem-mc/golem/nbt"
)

// The minecraft:nbt_path argument's grammar; walking and writing through a
// path is in nbtpath_ops.go.
// vanilla: NbtPathArgument

// The translation keys the path's own refusals answer with.
const (
	pathNodeInvalidKey  = "arguments.nbtpath.node.invalid"
	pathNothingFoundKey = "arguments.nbtpath.nothing_found"
	pathTooDeepKey      = "arguments.nbtpath.too_deep"
	dataExpectedListKey = "commands.data.modify.expected_list"
	dataInvalidIndexKey = "commands.data.modify.invalid_index"
)

// nbtPath is one parsed path: the text it was typed as — which the
// not-found refusal spells its failing element with — the elements it
// steps a tag through, and where each of them ended.
// vanilla: NbtPathArgument$NbtPath
type nbtPath struct {
	original string
	nodes    []nbtPathNode
	// ends is each element's end offset in original, which is how far
	// the not-found refusal spells the path with.
	ends []int
}

// parseNBTPath binds the shared grammar's typed nodes to path operations.
func parseNBTPath(text string) (nbtPath, *ParseError) {
	parsed, err := commandsyntax.ParseNBTPath(text)
	path := nbtPath{original: parsed.Original, ends: parsed.Ends}
	for _, node := range parsed.Nodes {
		var op nbtPathNode
		switch n := node.(type) {
		case commandsyntax.CompoundChildNode:
			op = compoundChildNode{name: n.Name}
		case commandsyntax.IndexedElementNode:
			op = indexedElementNode{index: n.Index}
		case commandsyntax.AllElementsNode:
			op = allElementsNode{}
		case commandsyntax.MatchElementNode:
			op = matchElementNode{pattern: n.Pattern}
		case commandsyntax.MatchObjectNode:
			op = matchObjectNode{name: n.Name, pattern: n.Pattern}
		case commandsyntax.MatchRootObjectNode:
			op = matchRootObjectNode{pattern: n.Pattern}
		}
		path.nodes = append(path.nodes, op)
	}
	return path, syntaxError(err)
}

// pathError is one path operation's refusal: the translation key the
// vanilla path throws with, with the values its %s takes as the compact
// SNBT the vanilla exception's own arguments render as.
type pathError struct {
	key  string
	with []string
}

// pathRefusal builds one refusal naming the values its message takes.
func pathRefusal(key string, with ...string) *pathError {
	return &pathError{key: key, with: with}
}

// collectionElements reads the elements of the lists and the typed
// arrays, which the game counts among its collections; a tag that holds
// no elements answers none.
func collectionElements(tag nbt.Tag) ([]nbt.Tag, bool) {
	switch v := tag.(type) {
	case nbt.List:
		return v.Items, true
	case nbt.ByteArray:
		out := make([]nbt.Tag, len(v))
		for i, b := range v {
			out[i] = nbt.Byte(b)
		}
		return out, true
	case nbt.IntArray:
		out := make([]nbt.Tag, len(v))
		for i, number := range v {
			out[i] = nbt.Int(number)
		}
		return out, true
	case nbt.LongArray:
		out := make([]nbt.Tag, len(v))
		for i, number := range v {
			out[i] = nbt.Long(number)
		}
		return out, true
	}
	return nil, false
}

// arrayElements reads a typed array as the element tags the game's
// CollectionTag.get hands over, with the array's element type. ok is
// false for a tag that is no typed array.
func arrayElements(tag nbt.Tag) (elements []nbt.Tag, kind nbt.Type, ok bool) {
	switch v := tag.(type) {
	case nbt.ByteArray:
		out := make([]nbt.Tag, len(v))
		for i, b := range v {
			out[i] = nbt.Byte(b)
		}
		return out, nbt.TypeByte, true
	case nbt.IntArray:
		out := make([]nbt.Tag, len(v))
		for i, number := range v {
			out[i] = nbt.Int(number)
		}
		return out, nbt.TypeInt, true
	case nbt.LongArray:
		out := make([]nbt.Tag, len(v))
		for i, number := range v {
			out[i] = nbt.Long(number)
		}
		return out, nbt.TypeLong, true
	}
	return nil, nbt.TypeEnd, false
}

// arrayNarrow narrows a tag to one element of the array kind given, the
// reading the game's arrays do when an element is set or added; ok is
// false for a tag that is no number.
func arrayNarrow(kind nbt.Type, tag nbt.Tag) (nbt.Tag, bool) {
	switch kind {
	case nbt.TypeByte:
		if v, ok := nbt.ByteValue(tag); ok {
			return nbt.Byte(v), true
		}
	case nbt.TypeInt:
		if v, ok := nbt.IntValue(tag); ok {
			return nbt.Int(v), true
		}
	case nbt.TypeLong:
		if v, ok := nbt.LongValue(tag); ok {
			return nbt.Long(v), true
		}
	}
	return nil, false
}

// arrayOfKind builds the typed array of the kind given from element tags.
// ok is false for a tag that is no number.
func arrayOfKind(kind nbt.Type, elements []nbt.Tag) (nbt.Tag, bool) {
	switch kind {
	case nbt.TypeByte:
		out := make(nbt.ByteArray, len(elements))
		for i, t := range elements {
			v, ok := nbt.ByteValue(t)
			if !ok {
				return nil, false
			}
			out[i] = byte(v)
		}
		return out, true
	case nbt.TypeInt:
		out := make(nbt.IntArray, len(elements))
		for i, t := range elements {
			v, ok := nbt.IntValue(t)
			if !ok {
				return nil, false
			}
			out[i] = v
		}
		return out, true
	case nbt.TypeLong:
		out := make(nbt.LongArray, len(elements))
		for i, t := range elements {
			v, ok := nbt.LongValue(t)
			if !ok {
				return nil, false
			}
			out[i] = v
		}
		return out, true
	}
	return nil, false
}

// tagsMatchPattern reports whether a tag carries a compound matcher's
// entries (other entries allowed), a pattern list element needing a match
// anywhere in the tag's list.
// vanilla: NbtUtils.compareNbt
func tagsMatchPattern(pattern, tag nbt.Tag) bool {
	return nbt.Matches(pattern, tag)
}

// pathArg is the tree's path argument, checked while the walk reads it.
func pathArg(name string) *Node {
	return &Node{Type: NodeArgument, Name: name, Parser: ParserNbtPath, Check: checkNbtPathArg}
}

// checkNbtPathArg reads a path word while the walk is reading it: a text
// the vanilla grammar refuses stops the command where its reader stood,
// as a typed parse failure.
// vanilla: NbtPathArgument.parse
func checkNbtPathArg(word string) *ParseError {
	if _, err := parseNBTPath(word); err != nil {
		return err
	}
	return nil
}

// pathSpanOf counts how many words a path argument spans: a quoted key or
// a matcher's compound may carry the spaces the tokenizer splits on.
func pathSpanOf(words []string) int {
	return pathSpan(words, plainText)
}

// pathSpan is pathSpanOf over words of either shape, read in place.
func pathSpan[W any](words []W, textOf func(W) string) int {
	depth, quote := 0, byte(0)
	for count, word := range words {
		text := textOf(word)
		for i := 0; i < len(text); i++ {
			c := text[i]
			switch {
			case quote != 0:
				switch c {
				case quote:
					quote = 0
				case '\\':
					i++
				}
			case c == '"' || c == '\'':
				quote = c
			case c == '{' || c == '[':
				depth++
			case c == '}' || c == ']':
				depth--
			}
		}
		if depth <= 0 && quote == 0 {
			return count + 1
		}
	}
	return len(words)
}
