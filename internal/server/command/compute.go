package command

import (
	"errors"
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Compute is the /compute command.
var Compute = Command{
	Name:   "compute",
	Level:  PermLevelGamemasters,
	Build:  computeNode,
	Run:    discardResult(runCompute),
	Result: runCompute,
}

// The keys the /compute command answers with.
// vanilla: ComputeCommand
const (
	computeExactKey           = "command.compute.result.unnamed.exact"
	computeRoundedKey         = "command.compute.result.unnamed.rounded"
	computeNamedExactKey      = "command.compute.result.named.exact"
	computeNamedRoundedKey    = "command.compute.result.named.rounded"
	computeInvalidKey         = "command.compute.result.unnamed.invalid"
	computeNamedInvalidKey    = "command.compute.result.named.invalid"
	computeIntProviderRegistr = "minecraft:context_int_provider"
	computeFloatProviderRegis = "minecraft:context_float_provider"
)

// computeNode builds /compute: the context sources, each type's provider
// taking a scale behind the float.
// vanilla: ComputeCommand.register
func computeNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "compute",
		Required: PermLevelGamemasters,
		Children: computeSources(true),
	}
}

// computeSources builds the three context sources (default, block, entity),
// each taking a float or an integer provider; scale puts /compute's scale
// behind the float provider.
// vanilla: LootContextSources.addContextSources
func computeSources(scale bool) []*Node {
	provider := func(parser int32, check func(string) *ParseError, children ...*Node) *Node {
		return &Node{
			Type:       NodeArgument,
			Name:       "provider",
			Executable: true,
			Parser:     parser,
			Check:      check,
			Children:   children,
		}
	}
	kinds := func() []*Node {
		var floatTail []*Node
		if scale {
			floatTail = []*Node{{Type: NodeArgument, Name: "scale", Executable: true, Parser: ParserFloat, Float: &FloatArg{}}}
		}
		return []*Node{
			{Type: NodeLiteral, Name: "float", Children: []*Node{provider(ParserContextFloatProvider, checkFloatProviderArg, floatTail...)}},
			{Type: NodeLiteral, Name: "integer", Children: []*Node{provider(ParserContextIntProvider, checkIntProviderArg)}},
		}
	}
	return []*Node{
		{Type: NodeLiteral, Name: "default", Children: kinds()},
		{Type: NodeLiteral, Name: "block", Children: []*Node{
			itemBlockPosArg("computePos", kinds()),
		}},
		{Type: NodeLiteral, Name: "entity", Children: []*Node{{
			Type:       NodeArgument,
			Name:       "computeTarget",
			Parser:     ParserEntity,
			Properties: func(b *protocol.Buffer) { b.PutU8(0x01) }, // one entity
			EntityArg:  &EntityArg{Single: true},
			Suggests:   AskServerSuggestion,
			Completions: func(ctx any, partial string) (int, []string) {
				return EntityCompletions(ctx, partial)
			},
			Children: kinds(),
		}}},
	}
}

// parseProviderArg reads a provider argument: an identifier names a data
// registry entry (so a bare number is a lookup that fails), anything else is
// an inline provider's SNBT.
// vanilla: ResourceOrIdArgument.parse
func parseProviderArg(text, registryName string, decode func(nbt.Tag) (registry.LootNumber, string, error), ref func(string) (registry.LootNumber, string)) (registry.LootNumber, *ParseError) {
	var zero registry.LootNumber
	if name, n, ok := readIdentifier(text); ok && n == len(text) {
		if p, msg := ref(name); msg == "" {
			return p, nil
		}
		return zero, &ParseError{
			Key:    lootTableNotFoundKey,
			With:   []string{registry.Qualify(name), registryName},
			Cursor: len(text),
		}
	}
	tag, n, pe := nbt.ParseValueTag(text)
	if pe != nil {
		return zero, snbtRefusal(pe, joinSelector([]Token{{Text: text}}), pe.Offset)
	}
	if n != len(text) {
		return zero, &ParseError{Key: "command.unknown.argument", Cursor: n}
	}
	p, msg, err := decode(tag)
	if err != nil {
		return zero, weightOverflowError(err, len(text))
	}
	if msg != "" {
		return zero, &ParseError{Key: modifierFailedParseKey, With: []string{msg}, Cursor: len(text)}
	}
	return p, nil
}

func parseIntProviderArg(text string) (registry.LootNumber, *ParseError) {
	return parseProviderArg(text, computeIntProviderRegistr, datapack.DecodeIntProvider,
		func(n string) (registry.LootNumber, string) { return datapack.ProviderRef(n, false) })
}

func parseFloatProviderArg(text string) (registry.LootNumber, *ParseError) {
	return parseProviderArg(text, computeFloatProviderRegis, datapack.DecodeFloatProvider,
		func(n string) (registry.LootNumber, string) { return datapack.ProviderRef(n, true) })
}

func checkIntProviderArg(text string) *ParseError {
	_, err := parseIntProviderArg(text)
	return err
}

func checkFloatProviderArg(text string) *ParseError {
	_, err := parseFloatProviderArg(text)
	return err
}

// runCompute runs /compute: the context a source names, then the provider
// evaluated against it.
// vanilla: ComputeCommand
func runCompute(src Source, input string, args []string) (int32, error) {
	lc, rest, err := computeContext(src, input, args)
	if lc == nil {
		return 0, err
	}
	p, kind, tail := readComputeProvider(src, input, rest)
	switch kind {
	case "integer":
		return computeInt(src, lc, p)
	case "float":
		scale := float32(1)
		if len(tail) > 0 {
			f, ok := parseFloatEnd(tail[0])
			if !ok {
				src.Debug("unreadable scale", "word", tail[0])
				return 0, nil
			}
			scale = float32(f)
		}
		return computeFloat(src, lc, p, scale)
	}
	return 0, nil
}

// computeContext resolves a context source (default, block or entity) at
// args[0] into the context its provider is evaluated against, and returns
// the words after it. A nil context means the command stops with the error,
// a refusal already sent when there is one.
// vanilla: LootContextSources.addContextSources
func computeContext(src Source, input string, args []string) (*rules.LootContext, []string, error) {
	lc := &rules.LootContext{}
	if self := src.SelfEntity(); self != nil {
		lc.This = lootEntityOf(self)
	}
	fillLootWorld(lc, src, src.Position())
	if len(args) == 0 {
		src.Debug("compute without its source")
		return nil, nil, nil
	}
	switch args[0] {
	case "default":
		return lc, args[1:], nil
	case "block":
		if len(args) < 4 {
			src.Debug("incomplete compute position", "words", args)
			return nil, nil, nil
		}
		x, y, z, ok := resolveBlockPos(src, args[1:4])
		if !ok {
			src.Debug("compute position did not parse", "words", args[1:4])
			return nil, nil, nil
		}
		if key := loadedBlockPosRefusal(src, x, y, z); key != "" {
			return nil, nil, refuse(src, key)
		}
		return lc, args[4:], nil
	case "entity":
		word, tail := splitTargets(input, args[1:])
		targets, err := resolveEntities(src, word)
		if err != nil {
			return nil, nil, err
		}
		if len(targets) != 1 {
			src.Debug("compute entity named several entities", "count", len(targets))
			return nil, nil, nil
		}
		lc.Target = lootEntityOf(targets[0])
		return lc, tail, nil
	}
	src.Debug("unknown compute source", "source", args[0])
	return nil, nil, nil
}

// readComputeProvider reads the type word at rest[0] and the provider after
// it. The kind is "integer" or "float", or empty when the words did not
// read, and the words after the provider come back.
func readComputeProvider(src Source, input string, rest []string) (p registry.LootNumber, kind string, tail []string) {
	if len(rest) < 2 {
		src.Debug("incomplete compute", "words", rest)
		return p, "", nil
	}
	span := min(blockSpanCount(rest[1:]), len(rest)-1)
	text := wordsIn(input, rest, 1, span)
	var err *ParseError
	switch rest[0] {
	case "integer":
		p, err = parseIntProviderArg(text)
	case "float":
		p, err = parseFloatProviderArg(text)
	default:
		src.Debug("unknown compute type", "type", rest[0])
		return p, "", nil
	}
	if err != nil {
		src.Debug("unreadable provider", "type", rest[0], "word", text)
		return p, "", nil
	}
	return p, rest[0], rest[1+span:]
}

// evalFailed answers a provider that threw: an uncaught exception is
// command.failed, an ArithmeticException the invalid value.
// vanilla: Commands.performCommand, ComputeCommand.computeAsInt
func evalFailed(src Source, p registry.LootNumber, err error) error {
	if errors.Is(err, rules.ErrLookupIndex) {
		return refuse(src, "command.failed")
	}
	return invalidValue(src, p, err.Error())
}

// vanilla: ComputeCommand.throwInvalidValue
func invalidValue(src Source, p registry.LootNumber, value string) error {
	reason := protocol.Text{Text: value}
	if ref, ok := p.(registry.LootProviderRef); ok {
		return refuse(src, computeNamedInvalidKey, protocol.Text{Text: ref.Name}, reason)
	}
	return refuse(src, computeInvalidKey, reason)
}

// vanilla: ComputeCommand.printExactOutput, ComputeCommand.printRoundedOutput
func computeFeedback(src Source, p registry.LootNumber, unnamedKey, namedKey string, with ...protocol.Text) error {
	if ref, ok := p.(registry.LootProviderRef); ok {
		return src.Feedback(namedKey, append([]protocol.Text{{Text: ref.Name}}, with...)...)
	}
	return src.Feedback(unnamedKey, with...)
}

// computeInt answers the int provider's value.
// vanilla: ComputeCommand.computeAsInt
func computeInt(src Source, lc *rules.LootContext, p registry.LootNumber) (int32, error) {
	v, err := rules.EvalIntProvider(p, lc, src.Random())
	if err != nil {
		return 0, evalFailed(src, p, err)
	}
	n := int64(v)
	return resultOf(v, computeFeedback(src, p, computeExactKey, computeNamedExactKey, protocol.Text{Number: &n}))
}

// computeFloat answers the float provider's value times the scale, floored.
// vanilla: ComputeCommand.computeAsFloat
func computeFloat(src Source, lc *rules.LootContext, p registry.LootNumber, scale float32) (int32, error) {
	original, err := rules.EvalFloatProvider(p, lc, src.Random())
	if err != nil {
		return 0, evalFailed(src, p, err)
	}
	if f := float64(original); math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, invalidValue(src, p, formatJavaFloat(original))
	}
	// Mth.floor(float): the (int) cast, one less where it rounded up.
	scaled := original * scale
	result := rules.JavaF2I(float64(scaled))
	if scaled < float32(result) {
		result--
	}
	n := int64(result)
	if float32(result) == original {
		return resultOf(result, computeFeedback(src, p, computeExactKey, computeNamedExactKey, protocol.Text{Number: &n}))
	}
	return resultOf(result, computeFeedback(src, p, computeRoundedKey, computeNamedRoundedKey, protocol.Text{Float: &original}, protocol.Text{Number: &n}))
}

// weightOverflowError is the command parse failure of a weighted list whose
// total weight overflows: vanilla throws from the list's constructor, so no
// codec wraps it. vanilla: CommandDispatcher.parseNodes
func weightOverflowError(err error, cursor int) *ParseError {
	return &ParseError{Key: "command.exception", With: []string{err.Error()}, Cursor: cursor}
}
