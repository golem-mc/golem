package command

import (
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Setblock is the /setblock command.
var Setblock = Command{
	Name:  "setblock",
	Level: PermLevelGamemasters,
	Build: setblockNode,
	Run:   runSetblock,
}

// The translation keys the /setblock command answers with: what it changed,
// and what it could not.
const (
	setblockSuccessKey = "commands.setblock.success"
	setblockFailedKey  = "commands.setblock.failed"
)

// airState is the block state ID of air, which the keep mode of /setblock
// refuses to overwrite. Air is the first entry in every version's block
// registry, so the ID is always zero.
const airState uint16 = 0

// setblockNode builds the /setblock command tree, matching the
// vanilla report: a position, the block to put there, and an optional
// mode naming what to do with what is already in the way.
func setblockNode() *Node {
	posArg := &Node{
		Type:   NodeArgument,
		Name:   "pos",
		Parser: ParserBlockPos,
	}
	blockArg := &Node{
		Type:       NodeArgument,
		Name:       "block",
		Executable: true,
		Parser:     ParserBlockState,
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return blockNameCompletions(partial)
		},
		IntegerMatches: blockNameIntegers,
	}
	// The modes are literals that change how the placement treats the block
	// that is already there; the vanilla server replaces by default.
	for _, mode := range []string{"destroy", "keep", "replace", "strict"} {
		blockArg.Children = append(blockArg.Children, &Node{
			Type:       NodeLiteral,
			Name:       mode,
			Executable: true,
		})
	}
	posArg.Children = []*Node{blockArg}
	return &Node{
		Type:     NodeLiteral,
		Name:     "setblock",
		Required: PermLevelGamemasters,
		Children: []*Node{posArg},
	}
}

// blockNameIntegers are the matches of blockNameCompletions that are integer
// property values.
func blockNameIntegers(partial string) []string {
	if strings.Contains(partial, "[") {
		return blockPropertyIntegers(partial, false)
	}
	return nil
}

// blockPredicateIntegers is blockNameIntegers for a block predicate.
func blockPredicateIntegers(partial string) []string {
	if strings.Contains(partial, "[") {
		return blockPropertyIntegers(partial, true)
	}
	return nil
}

// blockNameCompletions are what a block argument suggests: every block name,
// and inside a property list the properties and their values.
// vanilla: SharedSuggestionProvider.filterResources
func blockNameCompletions(partial string) (int, []string) {
	if strings.Contains(partial, "[") {
		return blockPropertyCompletions(partial, false)
	}
	if next, complete := afterBlockID(partial); complete {
		return len(partial), next
	}
	return 0, ResourceSuggestions(partial, registry.BlockNames...)
}

// afterBlockID answers a partial that is a whole block id: complete is true
// and next holds the `[` and `{` the block can continue with, none at all for
// a block with neither.
// vanilla: BlockStateParser.suggestOpenPropertiesOrNbt
func afterBlockID(partial string) (next []string, complete bool) {
	name := withMinecraftNamespace(partial)
	if !slices.Contains(registry.BlockNames, name) {
		return nil, false
	}
	return openingsFor(name), true
}

// afterBlockTag answers a partial that is a whole `#tag`: next holds `[` if
// any member has properties and `{` if any has block entity data.
// vanilla: BlockStateParser.suggestOpenVaguePropertiesOrNbt
func afterBlockTag(partial string) (next []string, complete bool) {
	id, ok := strings.CutPrefix(partial, "#")
	if !ok {
		return nil, false
	}
	members, ok := registry.BlockTagMembers(withMinecraftNamespace(id))
	if !ok {
		return nil, false
	}
	var props, nbt bool
	for _, m := range members {
		props = props || len(registry.BlockPropertyNames(m)) > 0
		_, entity := registry.BlockEntityTypes[m]
		nbt = nbt || entity
	}
	if props {
		next = append(next, "[")
	}
	if nbt {
		next = append(next, "{")
	}
	return next, true
}

// openingsFor lists the `[` and `{` a single block continues with.
func openingsFor(name string) []string {
	var next []string
	if len(registry.BlockPropertyNames(name)) > 0 {
		next = append(next, "[")
	}
	if _, ok := registry.BlockEntityTypes[name]; ok {
		next = append(next, "{")
	}
	return next
}

// blockPredicateCompletions are what a block predicate argument suggests: a
// block name, or a block tag carrying the # that only such a partial reaches,
// so a plain partial never sees the tags.
func blockPredicateCompletions(partial string) (int, []string) {
	if strings.Contains(partial, "[") {
		return blockPropertyCompletions(partial, true)
	}
	tags := registry.BlockTagNames()
	if next, complete := afterBlockID(partial); complete {
		return len(partial), next
	}
	if next, complete := afterBlockTag(partial); complete {
		return len(partial), next
	}
	options := ResourceSuggestions(partial, registry.BlockNames...)
	return 0, append(options, prefixedResourceSuggestions("#", partial, tags...)...)
}

// runSetblock runs /setblock <pos> <block> [mode]: keep refuses an occupied
// cell, destroy breaks the old block first, strict wakes no neighbours,
// replace (the default) neither. The block's entity tag may span words.
func runSetblock(src Source, input string, args []string) error {
	if len(args) < 4 {
		src.Debug("incomplete setblock command")
		return nil
	}
	px, py, pz, ok := resolveBlockPos(src, args[:3])
	if !ok {
		src.Debug("setblock position did not parse", "args", args[:3])
		return nil
	}

	span := blockSpanCount(args[3:])
	block := wordsIn(input, args, 3, span)
	state, defined, tag, fail := parseBlockStateDefined(block)
	if fail != nil {
		src.Debug("setblock block did not parse", "block", block)
		return blockParseError(input, args[3], fail)
	}
	dropInvalidBlockStates(tag, world.BlockStateNBTKeys, false)
	// Outside the world is refused before keep reads the cell, with
	// vanilla's loaded-check key. Deliberately unlike vanilla's
	// argument.pos.unloaded, an unloaded chunk inside the world is loaded
	// and set, so a chain can reach a dimension nobody stands in.
	// vanilla: BlockPosArgument.getLoadedBlockPos
	if !src.InWorldBounds(px, py, pz) {
		return refuse(src, loadedBlockPosRefusal(src, px, py, pz))
	}

	// The mode stands after the block argument, however many words its
	// braces spanned. keep looks before it puts and refuses a cell that
	// already holds a block; destroy breaks what stands there before the
	// placement; strict places without waking the neighbours.
	var strict, destroy bool
	if mode := 3 + span; len(args) > mode {
		switch args[mode] {
		case "keep":
			if src.BlockAt(px, py, pz) != airState {
				return refuse(src, setblockFailedKey)
			}
		case "destroy":
			destroy = true
		case "strict":
			strict = true
		}
	}

	if !src.SetBlockTagged(px, py, pz, state, defined, tag, strict, destroy) {
		return refuse(src, setblockFailedKey)
	}
	src.Info("block set", "x", px, "y", py, "z", pz, "by", src.Name())
	return feedbackToAdmins(src, setblockSuccessKey,
		blockCoord(px), blockCoord(py), blockCoord(pz))
}

// readBlockStateWord reads a block state word: the name, property overrides
// and block entity tag (loaded by placing commands, tested by a predicate). A refusal's cursor is an offset into word.
// vanilla: BlockStateParser.parseForBlock
func readBlockStateWord(word string) (name string, overrides map[string]string, tag nbt.Compound, err *ParseError) {
	n := identifierPrefix(word)
	id, ok := parseIdentifier(word[:n])
	if !ok {
		return "", nil, nil, &ParseError{Key: keyIDInvalid, Cursor: 0}
	}
	if _, known := registry.FindState(id, nil); !known {
		return "", nil, nil, &ParseError{Key: keyBlockIDInvalid, With: []string{id}, Cursor: 0}
	}
	pos := n
	if pos < len(word) && word[pos] == '[' {
		overrides, pos, err = readBlockProperties(word, pos, id)
		if err != nil {
			return "", nil, nil, err
		}
	}
	tag, err = readBlockEntityTagSuffix(word, pos)
	if err != nil {
		return "", nil, nil, err
	}
	return id, overrides, tag, nil
}

// readBlockEntityTagSuffix reads what follows a block word's name and
// property list at word[pos:]: nothing, or one '{...}' compound to the end.
// vanilla: BlockStateParser.readNbt
func readBlockEntityTagSuffix(word string, pos int) (nbt.Compound, *ParseError) {
	if pos >= len(word) {
		return nil, nil
	}
	if word[pos] != '{' {
		return nil, &ParseError{Key: keyExpectedSeparator, Cursor: pos}
	}
	parsed, perr := nbt.ParseOrderedSNBT(word[pos:])
	if perr != nil {
		// The tag's own failure carries the vanilla grammar's key.
		return nil, nbtParseError(perr.(*nbt.ParseError), pos)
	}
	if parsed.Entries() == nil {
		return nbt.Compound{}, nil
	}
	return parsed.Entries(), nil
}

// readBlockProperties reads the bracketed list at text[open] ('['),
// returning the offset past ']'. An unknown name or value is refused (cursor
// on the name, or the value's start, as vanilla leaves it), so no override
// silently becomes the default. Values may be padded or signed, as
// Integer.parseInt takes them, and are stored canonically.
// vanilla: BlockStateParser.readProperties
func readBlockProperties(text string, open int, block string) (map[string]string, int, *ParseError) {
	overrides := map[string]string{}
	// Whitespace is skipped before the first ']' test only; after a comma
	// the ']' is tested first, so "[a=b, ]" reads an empty name.
	pos := skipSpace(text, open+1)
	for {
		if pos >= len(text) {
			// The ']' the list opened was never reached.
			return nil, 0, &ParseError{Key: keyBlockPropertyUnclosed, Cursor: len(text)}
		}
		if text[pos] == ']' {
			return overrides, pos + 1, nil
		}
		pos = skipSpace(text, pos)
		nameStart := pos
		prop, n, err := readStringValue(text[pos:])
		if err != nil {
			err.Cursor += pos
			return nil, 0, err
		}
		pos = skipSpace(text, pos+n)
		values, known := registry.BlockPropertyValues(block, prop)
		if !known {
			return nil, 0, &ParseError{
				Key:    keyBlockPropertyUnknown,
				With:   []string{block, prop},
				Cursor: nameStart,
			}
		}
		if _, duplicate := overrides[prop]; duplicate {
			return nil, 0, &ParseError{
				Key:    keyBlockPropertyDuplicate,
				With:   []string{prop, block},
				Cursor: nameStart,
			}
		}
		// The unknown and duplicate refusals rewind to the name; this one
		// stays where the reader stopped.
		if pos >= len(text) || text[pos] != '=' {
			return nil, 0, &ParseError{
				Key:    keyBlockPropertyNoValue,
				With:   []string{prop, block},
				Cursor: pos,
			}
		}
		valueStart := skipSpace(text, pos+1)
		value, valueLen, err := readStringValue(text[valueStart:])
		if err != nil {
			err.Cursor += valueStart
			return nil, 0, err
		}
		pos = skipSpace(text, valueStart+valueLen)
		if !values[value] {
			if parsed, convErr := strconv.Atoi(value); convErr == nil && values[strconv.Itoa(parsed)] {
				value = strconv.Itoa(parsed)
			}
		}
		if !values[value] {
			return nil, 0, &ParseError{
				Key:    keyBlockPropertyInvalid,
				With:   []string{block, value, prop},
				Cursor: valueStart,
			}
		}
		overrides[prop] = value
		// After a value only a comma or the closing bracket may stand.
		if pos >= len(text) {
			return nil, 0, &ParseError{Key: keyBlockPropertyUnclosed, Cursor: len(text)}
		}
		if text[pos] == ',' {
			pos++
			continue
		}
		if text[pos] == ']' {
			return overrides, pos + 1, nil
		}
		return nil, 0, &ParseError{Key: keyBlockPropertyUnclosed, Cursor: pos}
	}
}

// tagPredicateBlock is the block a vague property error names: the vanilla
// parser's id field keeps its "minecraft:" default on the tag path.
const tagPredicateBlock = "minecraft:"

// readVagueBlockProperties reads a tag predicate's property list, unchecked
// against any block (an absent key or value matches nothing when tested).
// text[open] is '[', and the returned end is just past ']'.
// vanilla: BlockStateParser.readVagueProperties
func readVagueBlockProperties(text string, open int) (map[string]string, int, *ParseError) {
	overrides := map[string]string{}
	// As in readBlockProperties, "[a=b, ]" reads an empty name.
	pos := skipSpace(text, open+1)
	for {
		if pos >= len(text) {
			return nil, 0, &ParseError{Key: keyBlockPropertyUnclosed, Cursor: len(text)}
		}
		if text[pos] == ']' {
			return overrides, pos + 1, nil
		}
		pos = skipSpace(text, pos)
		nameStart := pos
		prop, n, err := readStringValue(text[pos:])
		if err != nil {
			err.Cursor += pos
			return nil, 0, err
		}
		pos = skipSpace(text, pos+n)
		if _, duplicate := overrides[prop]; duplicate {
			return nil, 0, &ParseError{
				Key:    keyBlockPropertyDuplicate,
				With:   []string{prop, tagPredicateBlock},
				Cursor: nameStart,
			}
		}
		if pos >= len(text) || text[pos] != '=' {
			return nil, 0, &ParseError{
				Key:    keyBlockPropertyNoValue,
				With:   []string{prop, tagPredicateBlock},
				Cursor: nameStart,
			}
		}
		valueStart := skipSpace(text, pos+1)
		value, valueLen, err := readStringValue(text[valueStart:])
		if err != nil {
			err.Cursor += valueStart
			return nil, 0, err
		}
		pos = skipSpace(text, valueStart+valueLen)
		overrides[prop] = value
		if pos >= len(text) {
			// The input ended at the value rather than at the ']', which
			// is where the vanilla reader leaves its cursor.
			return nil, 0, &ParseError{Key: keyBlockPropertyUnclosed, Cursor: valueStart}
		}
		if text[pos] == ',' {
			pos++
			continue
		}
		if text[pos] == ']' {
			return overrides, pos + 1, nil
		}
		return nil, 0, &ParseError{Key: keyBlockPropertyUnclosed, Cursor: pos}
	}
}

// parseBlockState reads a block argument: a name (minecraft: optional), an
// optional property list and an optional block entity compound, returned
// for the caller to load (a block without one has nowhere to put it).
func parseBlockState(word string) (uint16, nbt.Compound, *ParseError) {
	state, _, tag, fail := parseBlockStateDefined(word)
	return state, tag, fail
}

// parseBlockStateDefined is parseBlockState that also returns the properties
// the argument spelled out.
func parseBlockStateDefined(word string) (uint16, map[string]string, nbt.Compound, *ParseError) {
	name, overrides, tag, fail := readBlockStateWord(word)
	if fail != nil {
		return 0, nil, nil, fail
	}
	state, _ := registry.FindState(name, overrides)
	return state, overrides, tag, nil
}

// nbtParseError carries one of the SNBT reader's typed failures into this
// layer's own parse error, the reader's offset turned into a cursor by
// the base given. A compound whose text ended with something behind it
// is the dispatcher's separator complaint.
func nbtParseError(pe *nbt.ParseError, base int) *ParseError {
	key := pe.Key
	if key == nbt.ErrTrailing {
		key = keyExpectedSeparator
	}
	return &ParseError{Key: key, With: pe.With, Cursor: base + pe.Offset}
}

// blockParseError moves a block argument's cursor from argument-relative to
// whole-command, using where the first word stands; a word not in the input
// leaves it as is.
func blockParseError(input, firstWord string, fail *ParseError) *ParseError {
	if start := blockWordStart(input, firstWord); start >= 0 {
		fail.Cursor += start
	}
	return fail
}

// parseBlockStateArgument validates a block state during the walk: a tag is
// refused at the argument's start, the rest read by readBlockStateWord.
// vanilla: BlockStateParser
func parseBlockStateArgument(tokens []Token) *ParseError {
	span := joinSelector(tokens)
	if strings.HasPrefix(span.text, "#") {
		return &ParseError{Key: keyBlockTagDisallowed, Cursor: span.absolute(0)}
	}
	_, _, _, err := readBlockStateWord(span.text)
	if err != nil {
		err.Cursor = span.absolute(err.Cursor)
	}
	return err
}

// blockPredicate is a block predicate argument: a block name or a tag,
// the properties it tests, and the block entity compound it tests (nil when
// it names none).
// vanilla: BlockPredicateArgument.BlockPredicate, BlockPredicateArgument.TagPredicate
type blockPredicate struct {
	name  string
	isTag bool
	props map[string]string
	nbt   nbt.Compound
}

// parseBlockPredicate reads a handler's block predicate as the walk
// validates it: '#tag' or a block name, with optional property tests and an
// optional block entity compound. A tag's list is read vaguely (unknown keys
// match nothing).
func parseBlockPredicate(word string) (blockPredicate, bool) {
	if strings.HasPrefix(word, "#") {
		rest := word[1:]
		id, n, idOK := readIdentifier(rest)
		if !idOK {
			return blockPredicate{}, false
		}
		overrides := map[string]string{}
		if n < len(rest) && rest[n] == '[' {
			parsed, end, err := readVagueBlockProperties(rest, n)
			if err != nil {
				return blockPredicate{}, false
			}
			overrides, n = parsed, end
		}
		tag, err := readBlockEntityTagSuffix(rest, n)
		if err != nil {
			return blockPredicate{}, false
		}
		return blockPredicate{name: id, isTag: true, props: overrides, nbt: tag}, true
	}
	name, overrides, blockEntity, err := readBlockStateWord(word)
	if err != nil {
		return blockPredicate{}, false
	}
	return blockPredicate{name: name, props: overrides, nbt: blockEntity}, true
}

// matches reports whether the cell at the position, holding state, is one
// the predicate picks: its block or tag with every property tested, and, for
// a predicate with a compound, a block entity whose full saved compound
// (id and position included) contains it.
// vanilla: BlockPredicateArgument.BlockPredicate.test, BlockPredicateArgument.TagPredicate.test
func (p blockPredicate) matches(src Source, x, y, z int32, state uint16) bool {
	if p.isTag {
		if !blockFilterTagMatches(p.name, p.props, state) {
			return false
		}
	} else if !blockFilterMatches(p.name, p.props, state) {
		return false
	}
	if p.nbt == nil {
		return true
	}
	actual, has := src.BlockEntityTagAt(x, y, z)
	return has && nbt.Matches(p.nbt, actual)
}

// parseBlockPredicateArgument validates a block predicate during the walk:
// '#tag' is looked up and its list read vaguely, then an optional compound;
// anything else goes to readBlockStateWord.
// vanilla: BlockPredicateArgument, BlockStateParser
func parseBlockPredicateArgument(tokens []Token) *ParseError {
	span := joinSelector(tokens)
	if strings.HasPrefix(span.text, "#") {
		id, n, ok := readIdentifier(span.text[1:])
		if !ok {
			return nil // the tag is looked up by the command, not here
		}
		if _, known := registry.BlockTagMembers(id); !known {
			return &ParseError{Key: keyBlockTagUnknown, With: []string{id}, Cursor: span.absolute(0)}
		}
		pos := 1 + n
		if pos < len(span.text) && span.text[pos] == '[' {
			_, end, err := readVagueBlockProperties(span.text, pos)
			if err != nil {
				err.Cursor = span.absolute(err.Cursor)
				return err
			}
			pos = end
		}
		if pos < len(span.text) && span.text[pos] == '{' {
			if _, err := readBlockEntityTagSuffix(span.text, pos); err != nil {
				err.Cursor = span.absolute(err.Cursor)
				return err
			}
		}
		return nil
	}
	_, _, _, err := readBlockStateWord(span.text)
	if err != nil {
		err.Cursor = span.absolute(err.Cursor)
	}
	return err
}

// blockCoord is a block coordinate as the number component vanilla sends.
func blockCoord(v int32) protocol.Text {
	n := int64(v)
	return protocol.Text{Number: &n}
}
