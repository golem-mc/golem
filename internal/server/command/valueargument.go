package command

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// ValueArgument builds a vanilla world-value parser for a server-owned command.
func ValueArgument(name string, parser int32) *Node {
	n := &Node{Type: NodeArgument, Name: name, Parser: parser}
	switch parser {
	case ParserBlockPos, ParserVec3:
		n.Suggests = AskServerSuggestion
		n.InputCompletions = func(_ Source, _, partial string, _ map[string]ArgumentValue) (int, []string) {
			return 0, coordinateSuggestions(parser, partial)
		}
	case ParserBlockState:
		n.Suggests = AskServerSuggestion
		n.Completions = func(_ any, partial string) (int, []string) { return blockNameCompletions(partial) }
		n.IntegerMatches = blockNameIntegers
	case ParserItemStack:
		n.Suggests = AskServerSuggestion
		n.InputCompletions = func(_ Source, _, partial string, _ map[string]ArgumentValue) (int, []string) {
			return itemStackCompletions(partial)
		}
	case ParserComponent:
		n.Check = checkComponentArg
		n.Suggests = AskServerSuggestion
		n.InputCompletions = func(_ Source, _, partial string, _ map[string]ArgumentValue) (int, []string) {
			return nbt.ValueSuggestions(partial)
		}
	case ParserDimension:
		n.Check = func(word string) *ParseError { return parseResourceLocArgument(Token{Text: word}) }
	}
	return n
}

// PlayerArgument builds the single-player or multiple-player selector shape.
func PlayerArgument(name string, single bool) *Node { return EntityArgument(name, single, true) }

// EntityArgument builds a minecraft:entity argument over players or any entity.
// vanilla: EntityArgument.player / players / entity / entities
func EntityArgument(name string, single, playersOnly bool) *Node {
	n := playersArg()
	n.Name = name
	n.EntityArg.Single = single
	n.EntityArg.PlayersOnly = playersOnly
	n.Completions = func(ctx any, partial string) (int, []string) {
		src, ok := ctx.(Source)
		if !ok {
			return 0, nil
		}
		if src.PermLevel() < PermLevelGamemasters {
			return 0, playerNameSuggestions(src, partial)
		}
		return EntityCompletions(src, partial)
	}
	flags := byte(0)
	if playersOnly {
		flags = 2
	}
	if single {
		flags |= 1
	}
	n.Properties = func(b *protocol.Buffer) { b.PutU8(flags) }
	return n
}

// coordinateSuggestions uses the server source's default relative coordinates.
// vanilla: SharedSuggestionProvider.suggestCoordinates
func coordinateSuggestions(parser int32, partial string) []string {
	coordinate := "~"
	if strings.HasPrefix(partial, "^") {
		coordinate = "^"
	}
	fields := strings.Split(strings.TrimRight(partial, " "), " ")
	var candidates []string
	switch {
	case partial == "":
		candidates = []string{coordinate, coordinate + " " + coordinate, coordinate + " " + coordinate + " " + coordinate}
	case len(fields) == 1:
		candidates = []string{fields[0] + " " + coordinate, fields[0] + " " + coordinate + " " + coordinate}
	case len(fields) == 2:
		candidates = []string{fields[0] + " " + fields[1] + " " + coordinate}
	}
	if len(candidates) == 0 {
		return nil
	}
	full := candidates[len(candidates)-1]
	if parseCoordinateArgument(parser, Tokenize(full)) != nil {
		return nil
	}
	return Suggestions(partial, candidates...)
}

// ResolveArgument converts a parsed world value using the execution source.
// It returns [3]int32, [3]float64, uint16, protocol.ItemStack, protocol.Text,
// a dimension name, or []Entity; primitive parsers are converted by the host.
func ResolveArgument(src Source, a ArgumentValue) (any, error) {
	switch a.Parser {
	case ParserBlockPos:
		x, y, z, _ := resolveBlockPos(src, strings.Fields(a.Text))
		return [3]int32{x, y, z}, nil
	case ParserVec3:
		x, y, z, _ := resolveVec3(src, strings.Fields(a.Text))
		return [3]float64{x, y, z}, nil
	case ParserBlockState:
		state, _, fail := parseBlockState(a.Text)
		if fail != nil {
			fail.Cursor += a.Start
			return nil, fail
		}
		return state, nil
	case ParserItemStack:
		stack, ok := parseItemStack(a.Text)
		if !ok {
			return nil, &ParseError{Key: "command.failed", NoContext: true}
		}
		stack.Count = 1
		return stack, nil
	case ParserComponent:
		tag, fail := readComponentArg(a.Text)
		if fail != nil {
			fail.Cursor += a.Start
			return nil, fail
		}
		text, ok := componentText(ResolveComponent(src, tag))
		if !ok {
			return nil, &ParseError{Key: "argument.component.invalid", With: []string{a.Text}, NoContext: true}
		}
		return text, nil
	case ParserDimension:
		name := registry.Qualify(a.Text)
		if !dimensionKnown(src, name) {
			return nil, &ParseError{Key: dimensionInvalidKey, With: []string{name}, NoContext: true}
		}
		return name, nil
	case ParserEntity:
		selector, err := parsedSelector(a.Text, src.PermLevel() >= PermLevelGamemasters)
		if err != nil {
			return nil, err
		}
		if a.AnyEntity {
			entities := selector.findEntities(src)
			if len(entities) == 0 {
				return nil, &ParseError{Key: argumentEntityNotFoundKey, NoContext: true}
			}
			return entities, nil
		}
		players := selector.findPlayers(src)
		if len(players) == 0 {
			return nil, &ParseError{Key: argumentPlayerNotFoundKey, NoContext: true}
		}
		return asEntities(players), nil
	}
	return nil, nil
}
