package command

import (
	"errors"
	"math"
	"reflect"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Execute is the /execute command. Its modifiers are read in order, each
// handing its state to the next, until run names a command or the chain ends
// at a conditional, which answers for itself. The fork modifiers (as, at,
// facing entity, if entity, on, the stores) split the execution per body;
// a fork resolving to nobody ends the command silently, as does a failing
// conditional under a run.
var Execute = Command{
	Name:  "execute",
	Level: PermLevelGamemasters,
	Build: executeNode,
	Run:   runExecute,
}

// The translation keys /execute and its argument parsers answer with, from
// vanilla's ExecuteCommand and the arguments it reaches. Subsystems this
// server lacks are in the tree and refuse at execution with the keys their
// empty registries give.
const (
	conditionalPassKey      = "commands.execute.conditional.pass"
	conditionalPassCountKey = "commands.execute.conditional.pass_count"
	conditionalFailKey      = "commands.execute.conditional.fail"
	conditionalFailCountKey = "commands.execute.conditional.fail_count"
	blocksTooBigKey         = "commands.execute.blocks.toobig"
	forkLimitKey            = "command.forkLimit"
	objectiveNotFoundKey    = "arguments.objective.notFound"
	predicateNotFoundKey    = "argument.resource_or_id.no_such_element"
	dataBlockInvalidKey     = "commands.data.block.invalid"
	bossbarUnknownKey       = "commands.bossbar.unknown"
	posUnloadedKey          = "argument.pos.unloaded"
	posOutOfWorldKey        = "argument.pos.outofworld"
	dimensionInvalidKey     = "argument.dimension.invalid"
	anchorInvalidKey        = "argument.anchor.invalid"
	heightmapInvalidKey     = "argument.enum.invalid"
	swizzleInvalidKey       = "arguments.swizzle.invalid"
	resourceNotFoundKey     = "argument.resource.not_found"
	resourceTagNotFoundKey  = "argument.resource_tag.not_found"
)

// The biomes registry, whose entries and tags the biome conditional reads.
const biomeRegistry = "minecraft:worldgen/biome"

// The heightmap names a positioned over reads, and the dimension names an in
// or a dimension conditional reads, as the vanilla arguments know them.
var heightmapNames = map[string]bool{
	"motion_blocking":           true,
	"motion_blocking_no_leaves": true,
	"ocean_floor":               true,
	"world_surface":             true,
}

// The regions an if blocks compares come to no more cells than the vanilla
// server's own cap, whatever the block-modification rule says.
const maxIfBlocksArea = 32768

// executeNode builds the /execute tree, matching the vanilla report:
// the modifiers in the order vanilla registers them, each ending where it
// began so the next modifier or the run follows it.
func executeNode() *Node {
	execute := &Node{Type: NodeLiteral, Name: "execute", Required: PermLevelGamemasters}
	execute.Children = []*Node{
		// run reads on from the root of the whole tree, which the builder
		// cannot name; the walk and the packet both see it once ForLevel
		// has pointed it there.
		{Type: NodeLiteral, Name: "run", RedirectRoot: true},
		conditionalNode(execute, "if", true),
		conditionalNode(execute, "unless", false),
		{Type: NodeLiteral, Name: "as", Children: []*Node{forkTargets(execute)}},
		{Type: NodeLiteral, Name: "at", Children: []*Node{forkTargets(execute)}},
		storeNode(execute),
		{Type: NodeLiteral, Name: "positioned", Children: []*Node{
			redirectTo(execute, &Node{Type: NodeArgument, Name: "pos", Parser: ParserVec3}),
			{Type: NodeLiteral, Name: "as", Children: []*Node{forkTargets(execute)}},
			{Type: NodeLiteral, Name: "over", Children: []*Node{redirectTo(execute, heightmapArg("heightmap"))}},
		}},
		{Type: NodeLiteral, Name: "rotated", Children: []*Node{
			redirectTo(execute, &Node{Type: NodeArgument, Name: "rot", Parser: ParserRotation}),
			{Type: NodeLiteral, Name: "as", Children: []*Node{forkTargets(execute)}},
		}},
		{Type: NodeLiteral, Name: "facing", Children: []*Node{
			{Type: NodeLiteral, Name: "entity", Children: []*Node{
				{Type: NodeArgument, Name: "targets", Parser: ParserEntity, EntityArg: &EntityArg{},
					Properties: func(b *protocol.Buffer) { b.PutU8(0) },
					Children:   []*Node{forkAnchor(execute)}},
			}},
			redirectTo(execute, &Node{Type: NodeArgument, Name: "pos", Parser: ParserVec3}),
		}},
		{Type: NodeLiteral, Name: "align", Children: []*Node{redirectTo(execute, swizzleArg("axes"))}},
		{Type: NodeLiteral, Name: "anchored", Children: []*Node{redirectTo(execute, anchorArg("anchor"))}},
		{Type: NodeLiteral, Name: "in", Children: []*Node{redirectTo(execute, dimensionArg("dimension"))}},
		{Type: NodeLiteral, Name: "summon", Children: []*Node{redirectTo(execute, &Node{
			Type:       NodeArgument,
			Name:       "entity",
			Parser:     ParserResource,
			Properties: func(b *protocol.Buffer) { b.PutString(entityTypeRegistry) },
			Resource:   &ResourceArg{Registry: entityTypeRegistry},
			Suggests:   summonSuggestionType,
		})}},
		relationNode(execute),
	}
	return execute
}

// redirectTo marks a node whose reading continues at the execute node, the
// way the vanilla builder's .redirect(execute) does.
func redirectTo(execute *Node, n *Node) *Node {
	n.Redirect = execute
	return n
}

// forkTargets is the entity argument an as, an at or a positioned or rotated
// as reads: the bodies the modifier splits the execution over.
func forkTargets(execute *Node) *Node {
	return redirectTo(execute, &Node{
		Type:       NodeArgument,
		Name:       "targets",
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0) },
		EntityArg:  &EntityArg{},
	})
}

// forkAnchor is the anchor argument a facing entity reads after its targets,
// which splits the execution over the bodies it aims at.
func forkAnchor(execute *Node) *Node {
	return redirectTo(execute, anchorArg("anchor"))
}

// anchorArg is the entity anchor argument: the feet or the eyes.
func anchorArg(name string) *Node {
	return &Node{Type: NodeArgument, Name: name, Parser: ParserEntityAnch, Check: checkAnchorArg}
}

// dimensionArg is the dimension argument: the wire name of one of the worlds.
func dimensionArg(name string) *Node {
	return &Node{Type: NodeArgument, Name: name, Parser: ParserDimension}
}

// heightmapArg is the heightmap argument: the name of one of the heightmaps.
func heightmapArg(name string) *Node {
	return &Node{Type: NodeArgument, Name: name, Parser: ParserHeightmap, Check: checkHeightmapArg}
}

// swizzleArg is the swizzle argument: the axes letters.
func swizzleArg(name string) *Node {
	return &Node{Type: NodeArgument, Name: name, Parser: ParserSwizzle, Check: checkSwizzleArg}
}

// scoreHolderArg is the score holder argument, its byte saying whether it
// takes several. Both kinds parse a selector during the walk, so its spaces
// stay inside it; a single-holder one also refuses a multi-target selector.
func scoreHolderArg(name string, multiple bool) *Node {
	n := &Node{Type: NodeArgument, Name: name, Parser: ParserScoreHolder, Selector: &SelectorArg{Single: !multiple}}
	if multiple {
		n.Properties = func(b *protocol.Buffer) { b.PutU8(1) }
	} else {
		n.Properties = func(b *protocol.Buffer) { b.PutU8(0) }
	}
	return n
}

// storeNode builds the store branch: result and success, each into a score,
// a bossbar, an entity's NBT, a block's NBT or a storage's NBT.
func storeNode(execute *Node) *Node {
	return &Node{Type: NodeLiteral, Name: "store", Children: []*Node{
		storeKind(execute, "result"),
		storeKind(execute, "success"),
	}}
}

// storeKind builds one of the store's two kinds, whose branches are the same
// shape under either.
func storeKind(execute *Node, kind string) *Node {
	objective := redirectTo(execute, &Node{Type: NodeArgument, Name: "objective", Parser: ParserObjective})
	targets := scoreHolderArg("targets", true)
	targets.Children = []*Node{objective}
	return &Node{Type: NodeLiteral, Name: kind, Children: []*Node{
		{Type: NodeLiteral, Name: "score", Children: []*Node{
			targets,
		}},
		{Type: NodeLiteral, Name: "bossbar", Children: []*Node{
			{Type: NodeArgument, Name: "id", Parser: ParserResourceLoc,
				Children: []*Node{
					redirectTo(execute, &Node{Type: NodeLiteral, Name: "value"}),
					redirectTo(execute, &Node{Type: NodeLiteral, Name: "max"}),
				}},
		}},
		{Type: NodeLiteral, Name: "entity", Children: []*Node{
			{Type: NodeArgument, Name: "target", Parser: ParserEntity,
				Properties: func(b *protocol.Buffer) { b.PutU8(1) },
				EntityArg:  &EntityArg{Single: true},
				Children: []*Node{{Type: NodeArgument, Name: "path", Parser: ParserNbtPath,
					Children: nbtStoreTypes(execute)}}},
		}},
		{Type: NodeLiteral, Name: "block", Children: []*Node{
			{Type: NodeArgument, Name: "target", Parser: ParserBlockPos,
				Children: []*Node{{Type: NodeArgument, Name: "path", Parser: ParserNbtPath,
					Children: nbtStoreTypes(execute)}}},
		}},
		{Type: NodeLiteral, Name: "storage", Children: []*Node{
			{Type: NodeArgument, Name: "target", Parser: ParserResourceLoc,
				Children: []*Node{{Type: NodeArgument, Name: "path", Parser: ParserNbtPath,
					Children: nbtStoreTypes(execute)}}},
		}},
	}}
}

// nbtStoreTypes is the six type words an NBT store reads its scale after,
// in the order the vanilla builder registers them.
func nbtStoreTypes(execute *Node) []*Node {
	// One shared scale argument stands behind every type word, the way the
	// vanilla tree carries a single node six of its chains point at.
	scale := redirectTo(execute, &Node{Type: NodeArgument, Name: "scale", Parser: ParserDouble,
		Properties: func(b *protocol.Buffer) { b.PutU8(0) }})
	types := []string{"int", "float", "short", "long", "double", "byte"}
	nodes := make([]*Node, len(types))
	for i, name := range types {
		nodes[i] = &Node{Type: NodeLiteral, Name: name, Children: []*Node{scale}}
	}
	return nodes
}

// conditionalNode builds an if or an unless: the same twelve tests under
// either, each ending where it began so the chain goes on. The children keep
// vanilla's insertion order, which the client and /help both show
// (vanilla: ExecuteCommand.addConditionals adds data, then items and slots,
// after the chain).
func conditionalNode(execute *Node, name string, expected bool) *Node {
	return &Node{Type: NodeLiteral, Name: name, Children: []*Node{
		{Type: NodeLiteral, Name: "block", Children: []*Node{
			{Type: NodeArgument, Name: "pos", Parser: ParserBlockPos,
				Children: []*Node{redirectTo(execute, &Node{
					Type: NodeArgument, Name: "block", Parser: ParserBlockPredicate, Executable: true,
					Suggests: AskServerSuggestion,
				})}},
		}},
		{Type: NodeLiteral, Name: "biome", Children: []*Node{
			{Type: NodeArgument, Name: "pos", Parser: ParserBlockPos,
				Children: []*Node{redirectTo(execute, &Node{
					Type: NodeArgument, Name: "biome", Parser: ParserResourceOrTag, Executable: true,
					Properties: func(b *protocol.Buffer) { b.PutString(biomeRegistry) },
					Check:      checkBiomeArg,
				})}},
		}},
		{Type: NodeLiteral, Name: "loaded", Children: []*Node{
			redirectTo(execute, &Node{Type: NodeArgument, Name: "pos", Parser: ParserBlockPos, Executable: true}),
		}},
		{Type: NodeLiteral, Name: "dimension", Children: []*Node{
			redirectTo(execute, &Node{Type: NodeArgument, Name: "dimension", Parser: ParserDimension, Executable: true}),
		}},
		{Type: NodeLiteral, Name: "score", Children: scoreConditional(execute)},
		{Type: NodeLiteral, Name: "blocks", Children: []*Node{
			{Type: NodeArgument, Name: "start", Parser: ParserBlockPos,
				Children: []*Node{{Type: NodeArgument, Name: "end", Parser: ParserBlockPos,
					Children: []*Node{{Type: NodeArgument, Name: "destination", Parser: ParserBlockPos,
						Children: []*Node{
							redirectTo(execute, &Node{Type: NodeLiteral, Name: "all", Executable: true}),
							redirectTo(execute, &Node{Type: NodeLiteral, Name: "masked", Executable: true}),
						}}}}}},
		}},
		{Type: NodeLiteral, Name: "entity", Children: []*Node{
			redirectTo(execute, &Node{Type: NodeArgument, Name: "entities", Parser: ParserEntity, Executable: true,
				Properties: func(b *protocol.Buffer) { b.PutU8(0) },
				EntityArg:  &EntityArg{}}),
		}},
		{Type: NodeLiteral, Name: "predicate", Children: []*Node{
			redirectTo(execute, &Node{Type: NodeArgument, Name: "predicate", Parser: ParserLootPredicate, Executable: true, Check: predicateArgumentCheck(nil)}),
		}},
		{Type: NodeLiteral, Name: "function", Children: []*Node{
			redirectTo(execute, &Node{Type: NodeArgument, Name: "name", Parser: ParserFunction,
				Check: checkFunctionSyntax}),
		}},
		{Type: NodeLiteral, Name: "stopwatch", Children: []*Node{stopwatchTestNode(execute)}},
		{Type: NodeLiteral, Name: "data", Children: []*Node{
			dataConditional(execute, "entity", &Node{Type: NodeArgument, Name: "source", Parser: ParserEntity,
				Properties: func(b *protocol.Buffer) { b.PutU8(1) },
				EntityArg:  &EntityArg{Single: true}}),
			dataConditional(execute, "block", &Node{Type: NodeArgument, Name: "source", Parser: ParserBlockPos}),
			dataConditional(execute, "storage", &Node{Type: NodeArgument, Name: "source", Parser: ParserResourceLoc}),
		}},
		itemsConditional("items", func() *Node {
			return itemSlotsNode("slots", false, redirectTo(execute, &Node{
				Type: NodeArgument, Name: "item_predicate", Parser: ParserItemPred, Executable: true,
			}))
		}),
		itemsConditional("slots", func() *Node { return redirectTo(execute, itemSlotsNode("slots", true)) }),
	}}
}

// dataConditional is one of the data conditional's three sources, whose path
// ends the test.
func dataConditional(execute *Node, name string, source *Node) *Node {
	return &Node{Type: NodeLiteral, Name: name, Children: []*Node{
		{Type: NodeArgument, Name: source.Name, Parser: source.Parser,
			Properties: source.Properties, Check: source.Check, EntityArg: source.EntityArg,
			Children: []*Node{redirectTo(execute, &Node{
				Type: NodeArgument, Name: "path", Parser: ParserNbtPath, Executable: true,
				Check: checkNbtPathArg,
			})}},
	}}
}

// stopwatchTestNode is the stopwatch conditional: an id (suggesting existing
// ones) and the range the elapsed time is compared against.
// vanilla: ExecuteCommand, StopwatchCommand.SUGGEST_STOPWATCHES
func stopwatchTestNode(execute *Node) *Node {
	id := stopwatchIDArg()
	id.Children = []*Node{redirectTo(execute, &Node{
		Type: NodeArgument, Name: "range", Parser: ParserFloatRange, Executable: true,
		Check: checkFloatRangeArg,
	})}
	return id
}

// itemsConditional is the items or slots conditional: entities or a block
// position, each followed by a fresh copy of the test's tail.
// vanilla: ItemCommands.SOURCE_PROVIDERS
func itemsConditional(name string, tail func() *Node) *Node {
	return &Node{Type: NodeLiteral, Name: name, Children: []*Node{
		{Type: NodeLiteral, Name: "entity", Children: []*Node{
			itemEntityArg("source", []*Node{tail()}),
		}},
		{Type: NodeLiteral, Name: "block", Children: []*Node{
			{Type: NodeArgument, Name: "source", Parser: ParserBlockPos, Children: []*Node{tail()}},
		}},
	}}
}

// scoreConditional is the score test's argument chain: the target holder,
// its objective, and the six comparisons after, vanilla's own order.
func scoreConditional(execute *Node) []*Node {
	target := scoreHolderArg("target", false)
	targetObjective := &Node{Type: NodeArgument, Name: "targetObjective", Parser: ParserObjective}
	targetObjective.Children = scoreComparisons(execute)
	target.Children = []*Node{targetObjective}
	return []*Node{target}
}

// scoreComparisons is the six comparisons a score conditional reads its
// second half after, in the order the vanilla builder registers them.
func scoreComparisons(execute *Node) []*Node {
	return []*Node{
		scoreComparison(execute, "="),
		scoreComparison(execute, "<"),
		scoreComparison(execute, "<="),
		scoreComparison(execute, ">"),
		scoreComparison(execute, ">="),
		{Type: NodeLiteral, Name: "matches", Children: []*Node{redirectTo(execute, &Node{
			Type: NodeArgument, Name: "range", Parser: ParserIntRange, Executable: true, Check: checkIntRangeArg,
		})}},
	}
}

// scoreComparison is one of the four ordering comparisons and the equality,
// each reading the source score its test weighs the target's against.
func scoreComparison(execute *Node, name string) *Node {
	source := scoreHolderArg("source", false)
	sourceObj := redirectTo(execute, &Node{Type: NodeArgument, Name: "sourceObjective", Parser: ParserObjective, Executable: true})
	source.Children = []*Node{sourceObj}
	return &Node{Type: NodeLiteral, Name: name, Children: []*Node{source}}
}

// relationNode builds the on branch: the eight relations a body's company
// stands in, each splitting the execution over whoever fills it.
func relationNode(execute *Node) *Node {
	relations := []string{"owner", "leasher", "target", "attacker", "vehicle", "controller", "origin", "passengers"}
	nodes := make([]*Node, len(relations))
	for i, name := range relations {
		nodes[i] = &Node{Type: NodeLiteral, Name: name, Redirect: execute}
	}
	return &Node{Type: NodeLiteral, Name: "on", Children: nodes}
}

// Checks for the argument kinds whose word must name something; a Check
// fails the parse at the word.

// dimensionKnown reports whether a dimension word names a running
// dimension. The parse reads only the location, so a run asks this and
// refuses with dimensionInvalidKey, which carries no input.
// vanilla: DimensionArgument.getDimension
func dimensionKnown(src Source, word string) bool {
	return src.DimensionKnown(registry.Qualify(word))
}

// checkAnchorArg refuses a word that is neither of the two anchors. The
// cursor is the word's start: EntityAnchorArgument.parse winds the reader
// back to it before it throws. vanilla: EntityAnchorArgument.parse
func checkAnchorArg(word string) *ParseError {
	if word == "feet" || word == "eyes" {
		return nil
	}
	return &ParseError{Key: anchorInvalidKey, With: []string{word}}
}

// checkHeightmapArg refuses a word that names no heightmap.
func checkHeightmapArg(word string) *ParseError {
	if heightmapNames[word] {
		return nil
	}
	return &ParseError{Key: heightmapInvalidKey, With: []string{word}, Cursor: len(word)}
}

// checkSwizzleArg refuses a word that is not one to three axis letters, none
// of them twice.
func checkSwizzleArg(word string) *ParseError {
	seen := map[byte]bool{}
	for i := 0; i < len(word); i++ {
		c := word[i]
		if c != 'x' && c != 'y' && c != 'z' || seen[c] {
			return &ParseError{Key: swizzleInvalidKey, Cursor: len(word)}
		}
		seen[c] = true
	}
	if len(word) == 0 {
		return &ParseError{Key: swizzleInvalidKey, Cursor: len(word)}
	}
	return nil
}

// checkFloatRangeArg refuses a word that is not a "5", "..5", "5.." or
// "1..10" range, or has a tail (the dispatcher's missing separator).
// vanilla: MinMaxBounds.Doubles, RangeArgument.floatRange
func checkFloatRangeArg(word string) *ParseError {
	r := &selReader{input: word}
	if _, err := r.parseRange(parseFloatEnd, keyDoubleInvalid, false); err != nil {
		return err
	}
	if r.canRead() {
		return &ParseError{Key: keyExpectedSeparator, Cursor: r.pos}
	}
	return nil
}

// parseFloatRange reads the range a stopwatch test compares against, its ends
// read as doubles.
func parseFloatRange(word string) (*bounds, *ParseError) {
	r := &selReader{input: word}
	return r.parseRange(parseFloatEnd, keyDoubleInvalid, false)
}

// checkFunctionSyntax reads a function argument: a '#' tag or a resource
// location, judged by its characters alone; existence is not the parse's
// business (vanilla answers a bare function test with the path's unknown
// command). A refused name points back at its start after any '#'; a tail is
// the dispatcher's missing separator.
// vanilla: Identifier.read, FunctionArgument.parse
func checkFunctionSyntax(word string) *ParseError {
	offset := 0
	tag := word
	if strings.HasPrefix(word, "#") {
		tag, offset = word[1:], 1
	}
	_, n, ok := readIdentifier(tag)
	if !ok {
		return &ParseError{Key: keyIDInvalid, Cursor: offset}
	}
	if n < len(tag) {
		return &ParseError{Key: keyExpectedSeparator, Cursor: offset + n}
	}
	return nil
}

// checkBiomeArg refuses a biome name the biome registry does not hold and a
// biome tag the tag tables do not.
func checkBiomeArg(word string) *ParseError {
	if strings.HasPrefix(word, "#") {
		tag := registry.Qualify(word[1:])
		if !registry.BiomeTagExists(tag) {
			return &ParseError{Key: resourceTagNotFoundKey, With: []string{tag, biomeRegistry}, Cursor: len(word)}
		}
		return nil
	}
	r, ok := registry.Get(biomeRegistry)
	if ok && r.ID(registry.Qualify(word)) >= 0 {
		return nil
	}
	return &ParseError{Key: resourceNotFoundKey, With: []string{registry.Qualify(word), biomeRegistry}, Cursor: len(word)}
}

// execContext is one branch's state: world position, dimension, the @s body
// and the anchor. A chain starts with the runner's, and each fork multiplies
// them by the bodies it resolves.
type execContext struct {
	pos      player.Position
	dim      string
	executor Entity
	hasExec  bool
	eyes     bool
	// stores are the store steps the chain has passed, in order, each
	// handed the result of whatever finishes the branch.
	stores []storeFn
}

// storeFn is one store's callback with the branch's success and result (a
// failure is false, 0); an error is a throw aborting the command.
// vanilla: CommandResultCallback.onResult
type storeFn func(success bool, result int32) error

// errStoreThrew is a store whose write threw: the rest of the command is
// abandoned and the runner hears command.failed.
// vanilla: Commands.performCommand
var errStoreThrew = errors.New("execute store threw")

// report hands the branch's outcome to every store it carries, the earlier
// store first, stopping at one that throws.
// vanilla: CommandResultCallback.chain
func (ctx *execContext) report(success bool, result int32) error {
	for _, store := range ctx.stores {
		if err := store(success, result); err != nil {
			return err
		}
	}
	return nil
}

// newExecContext is the context a chain starts from: the runner's own place,
// dimension and body.
func newExecContext(src Source, stores []storeFn) execContext {
	ctx := execContext{
		pos:    src.Position(),
		dim:    src.DimensionName(),
		stores: stores,
	}
	if e := src.SelfEntity(); e != nil {
		ctx.executor = e
		ctx.hasExec = true
	}
	return ctx
}

// commandContext is ctx as Source carries it. A forked chain hands the tail
// a quiet source: refusals unheard, successes still reaching the runner.
func (ctx *execContext) commandContext(forked bool) Context {
	return Context{
		Pos:      ctx.pos,
		HasPos:   true,
		Dim:      ctx.dim,
		HasDim:   true,
		Executor: ctx.executor,
		Eyes:     ctx.eyes,
		Quiet:    forked,
	}
}

// view is the source the context's own reading happens through: the runner
// seeing from the context's place, whose selectors name the context's body.
// The chain's own words are never quiet ones.
func (ctx *execContext) view(src Source) (Source, bool) {
	return src.InContext(ctx.commandContext(false))
}

// anchorPoint is where the context aims its facings from: the position, one
// eye height above it when the anchor is the eyes and the body has eyes.
func (ctx *execContext) anchorPoint() player.Position {
	if ctx.eyes && ctx.hasExec {
		p := ctx.pos
		p.Y += ctx.executor.EyePosition().Y - ctx.executor.Position().Y
		return p
	}
	return ctx.pos
}

// resolve reads one targets word against a context's view: nobody being
// nobody rather than a refusal, which is what a fork's empty answer is.
// vanilla: EntityArgument.getOptionalEntities
func resolveOptionalEntities(src Source, word string) []Entity {
	sel, err := parsedSelector(word, src.PermLevel() >= PermLevelGamemasters)
	if err != nil {
		return nil
	}
	return sel.findEntities(src)
}

// selectorArgAt reads the selector-shaped argument whose first word sits at
// the token index: the words it spans joined back into the text a selector
// reader would have read, and how many of the input's words that is.
func selectorArgAt(tokens []Token, at int) (string, int) {
	if at >= len(tokens) {
		return "", 1
	}
	span, _ := scanSelector(tokens[at:], tokenText)
	return joinSelector(tokens[at : at+span]).text, span
}

// resolveRequiredEntities resolves targets as EntityArgument.getEntities
// does but returns the not-found key instead of sending it, since whether it
// shows depends on whether the chain has forked.
func resolveRequiredEntities(src Source, word string) ([]Entity, string) {
	sel, err := parsedSelector(word, src.PermLevel() >= PermLevelGamemasters)
	if err != nil {
		return nil, argumentEntityNotFoundKey
	}
	found := sel.findEntities(src)
	if len(found) == 0 {
		return nil, argumentEntityNotFoundKey
	}
	return found, ""
}

// scaleBetween is how far a position stretches when the context moves it
// from one dimension to another, the way the vanilla server's withLevel
// scales one: the ratio of the two dimensions' coordinate scales.
func scaleBetween(from, to string) float64 {
	scaleOf := func(name string) float64 {
		if name == world.NetherName {
			return 8
		}
		return 1
	}
	return scaleOf(from) / scaleOf(to)
}

// refuseStep carries one context's refusal the way a modifier's throw does:
// under forked mode the context is dropped and the chain goes on, and
// otherwise the bare message is the whole command's answer.
func refuseStep(view Source, forked bool, key string, with ...protocol.Text) error {
	if forked {
		return nil
	}
	return refuse(view, key, with...)
}

// expectOver runs a conditional's test over every context: matching answers
// keep their context on, and every answer is kept beside its context for the
// chain's end to report. numeric says the answers are counts.
func expectOver(src Source, ctxs []execContext, expected, numeric bool, test func(view Source) condAnswer) ([]execContext, *condAsk) {
	ask := &condAsk{expected: expected, numeric: numeric}
	next := ask.nextBuf[:0]
	if len(ctxs) <= 1 {
		ask.asked, ask.answers = ask.askedBuf[:0], ask.answersBuf[:0]
	} else {
		next = make([]execContext, 0, len(ctxs))
	}
	for _, ctx := range ctxs {
		view, ok := ctx.view(src)
		if !ok {
			continue
		}
		ans := test(view)
		ask.asked = append(ask.asked, ctx)
		ask.answers = append(ask.answers, ans)
		if ans.throwKey == "" && ans.passed == expected {
			next = append(next, ctx)
		}
	}
	return next, ask
}

// stepFork is as, at, positioned as, rotated as and facing entity: bodies
// resolved per context, a context settled for each, the chain split over
// them. Nobody resolved leaves nothing; reaching the fork limit is
// errForkLimit with the remaining contexts unread.
func stepFork(src Source, ctxs []execContext, word string, settle func(*execContext, Entity)) ([]execContext, int, error) {
	limit := forkLimit(src)
	var next []execContext
	for _, ctx := range ctxs {
		view, ok := ctx.view(src)
		if !ok {
			continue
		}
		found := resolveOptionalEntities(view, word)
		if len(next)+len(found) >= limit {
			return nil, 0, errForkLimit
		}
		next = slices.Grow(next, max(len(found), len(ctxs)-len(next)))
		for _, entity := range found {
			next = append(next, ctx)
			settle(&next[len(next)-1], entity)
		}
	}
	return next, 0, nil
}

// errForkLimit is a step whose contexts came to max_command_forks, which
// stops the chain at that step.
var errForkLimit = errors.New("execute fork limit reached")

// forkLimit is max_command_forks. A step checks it as its list grows, so a
// chain over many bodies stops at the limit rather than after building
// every context.
// vanilla: BuildContexts.execute
func forkLimit(src Source) int {
	return int(src.IntRule(world.RuleMaxCommandForks))
}

// forkLimitReached ends a chain at the fork limit; the runner hears it only
// outside forked mode, and nothing of the chain runs.
// vanilla: BuildContexts.execute, ERROR_FORK_LIMIT_REACHED
func forkLimitReached(src Source, forked bool, limit int) error {
	src.Debug("execute chain stopped: fork limit reached", "limit", limit)
	if forked {
		return nil
	}
	n := int64(limit)
	return refuse(src, forkLimitKey, protocol.Text{Number: &n})
}

// runExecute runs an accepted /execute: modifiers in order, forks splitting
// over bodies and answers, until run names a command for every remaining
// context or a conditional answers in every context.
func runExecute(src Source, input string, args []string) error {
	f := topFrame(src)
	err := f.finish(runExecuteIn(src, input, args, nil, f, false))
	if errors.Is(err, errStoreThrew) {
		// Vanilla also hovers the exception's message, which protocol.Text
		// cannot carry.
		return refuse(src, "command.failed")
	}
	return err
}

// runExecuteIn is runExecute in frame f under an outer command's stores, so
// each branch reports to them. With ret (a return run) only the first
// context reports to f's caller, and no context left fails f.
func runExecuteIn(src Source, input string, args []string, stores []storeFn, f *frame, ret bool) error {
	return walkExecute(src, input, args, Tokenize(input), f, ret, []execContext{newExecContext(src, stores)}, false, 0)
}

// walkExecute reads the chain from word i on over ctxs. A function test
// ends the walk there and resumes it, from a queued step, over the contexts
// whose function answered.
func walkExecute(src Source, input string, args []string, tokens []Token, f *frame, ret bool, ctxs []execContext, forked bool, i int) error {
	limit := forkLimit(src)
	var (
		last       *condAsk
		lastForked bool
	)
	for i < len(args) {
		if !f.run.spend() {
			return nil
		}
		rest := args[i:]
		// The step's words sit at the token index one past the command
		// word, which is where the span of its selector-shaped arguments
		// is read from.
		at := 1 + i
		var (
			next  []execContext
			used  int
			split bool
			ask   *condAsk
			err   error
		)
		switch rest[0] {
		case "run":
			return executeRun(src, ctxs, forked, rest[1:], input, tokens, at+1, f, ret)
		case "as":
			next, used, err = stepAs(src, ctxs, rest, tokens, at)
			split = true
		case "at":
			next, used, err = stepAt(src, ctxs, rest, tokens, at)
			split = true
		case "positioned":
			next, used, split, err = stepPositioned(src, ctxs, forked, rest, tokens, at)
		case "rotated":
			next, used, split, err = stepRotated(src, ctxs, forked, rest, tokens, at)
		case "facing":
			next, used, split, err = stepFacing(src, ctxs, forked, rest, tokens, at)
		case "align":
			next, used, err = stepAlign(src, ctxs, forked, rest)
		case "anchored":
			next, used, err = stepAnchored(src, ctxs, forked, rest)
		case "in":
			next, used, err = stepIn(src, ctxs, forked, rest)
		case "summon":
			next, used, err = stepSummon(src, ctxs, forked, rest)
		case "on":
			next, used, err = stepOn(src, ctxs, forked, rest)
			split = true
		case "if", "unless":
			if len(rest) > 2 && rest[1] == "function" {
				return scheduleFunctionTest(src, ctxs, rest[0] == "if", rest[2], f, func(passed []execContext) error {
					if len(passed) == 0 {
						// The chain ends without a word, even before a
						// final conditional.
						// vanilla: BuildContexts.execute, FallthroughTask
						if ret {
							return f.returnValue(false, 0)
						}
						return nil
					}
					return walkExecute(src, input, args, tokens, f, ret, passed, true, i+3)
				})
			}
			next, used, ask, err = stepConditional(src, ctxs, forked, rest[0] == "if", rest, tokens, at)
			split = true
		case "store":
			next, used, err = stepStore(src, ctxs, forked, rest, tokens, at)
		default:
			src.Debug("execute walk left its tree", "step", rest[0])
			return nil
		}
		// A step runs in forked mode once the chain has forked or when it
		// is a fork itself.
		if errors.Is(err, errForkLimit) {
			return forkLimitReached(src, forked || split, limit)
		}
		if err != nil {
			return err
		}
		// A conditional the chain stops on answers for itself, with the
		// forked state the chain carried before it; one the chain walks
		// past is a fork modifier, whose answers are never reported.
		terminal := i+used >= len(args)
		if ask != nil && terminal {
			last = ask
			lastForked = forked
		}
		if !terminal && len(next) >= limit {
			// A step reading at most one context per context hits the
			// limit only where the whole list does; the final conditional
			// is the command, not a modifier, and is never held to it.
			return forkLimitReached(src, forked || split, limit)
		}
		if len(next) == 0 {
			// The fork answered nobody, or every context was dropped on
			// the way: the chain ends without a word — unless it stopped
			// on a conditional, which still speaks for itself.
			if last != nil {
				return answerConditional(src, returningAsk(last, f, ret), lastForked)
			}
			if ret {
				// vanilla: BuildContexts.execute, FallthroughTask
				return f.returnValue(false, 0)
			}
			return nil
		}
		ctxs = next
		forked = forked || split
		i += used
	}
	if last == nil {
		src.Debug("execute chain ended without a conditional to answer")
		return nil
	}
	return answerConditional(src, returningAsk(last, f, ret), lastForked)
}

// executeRun runs the tail's command against each remaining context's
// source, with the tail's own input so greedy arguments keep their typed
// spacing. Under a return run a plain command runs in the first context
// only, its result f's return value.
func executeRun(src Source, ctxs []execContext, forked bool, tail []string, input string, tokens []Token, tailAt int, f *frame, ret bool) error {
	if len(tail) == 0 {
		src.Debug("execute run without a command")
		return nil
	}
	cmd, ok := LookupSource(src, tail[0])
	if !ok || cmd.Run == nil {
		src.Debug("execute run names nothing runnable", "word", tail[0])
		return nil
	}
	tailInput := input
	if tailAt < len(tokens) {
		tailInput = input[tokens[tailAt].Start:]
	}
	plain := cmd.Name != "execute" && cmd.Name != "function" && cmd.Name != "return"
	if ret && plain {
		// vanilla: BuildContexts.execute
		ctxs = ctxs[:1]
		ctxs[0].stores = append(slices.Clip(ctxs[0].stores), f.returnValue)
	}
	for _, ctx := range ctxs {
		view, ok := src.InContext(ctx.commandContext(forked))
		if !ok {
			continue
		}
		// The name, not Execute itself, which would be an initialization
		// cycle. The inner chain's own branches are what finish this one.
		if !plain {
			// A function's call and a return cost nothing of their own.
			// vanilla: CustomCommandExecutor
			if cmd.Name == "execute" && !f.run.spend() {
				return nil
			}
			cb := func(success bool, result int32) error {
				reportResult(src, success, result)
				return ctx.report(success, result)
			}
			var err error
			switch cmd.Name {
			case "execute":
				err = runExecuteIn(view, tailInput, tail[1:], ctx.stores, f, ret)
			case "function":
				err = runFunctionIn(view, tailInput, tail[1:], f, cb, ret)
			case "return":
				err = runReturnIn(view, tailInput, tail[1:], f, cb)
			}
			if err != nil && !errors.Is(err, ErrRefused) {
				return err
			}
			continue
		}
		if !f.run.spend() {
			return nil
		}
		result, err := cmd.Exec(view, tailInput, tail[1:])
		reportResult(src, err == nil, result)
		if serr := ctx.report(err == nil, result); serr != nil {
			return serr
		}
		if err != nil && !errors.Is(err, ErrRefused) {
			return err
		}
	}
	return nil
}

// returningAsk is the conditional a chain ends on, as a return run answers
// it: in the first context alone, that answer f's return value.
// vanilla: BuildContexts.execute
func returningAsk(ask *condAsk, f *frame, ret bool) *condAsk {
	if !ret || len(ask.asked) == 0 {
		return ask
	}
	first := ask.asked[0]
	first.stores = append(slices.Clip(first.stores), f.returnValue)
	return &condAsk{expected: ask.expected, numeric: ask.numeric,
		asked: []execContext{first}, answers: ask.answers[:1]}
}

// ResultSink is a Source that counts the results of the commands an execute
// ran, one per context, the callback a command block's success count reads.
// vanilla: CommandSourceStack.withCallback
type ResultSink interface {
	Result(success bool, result int32)
}

func reportResult(src Source, success bool, result int32) {
	if sink, ok := src.(ResultSink); ok {
		sink.Result(success, result)
	}
}

// answerConditional reports a chain ending on a conditional, answered in
// every context. A throw reports only unforked; a pass is vanilla's pass
// message, a failure the refusal it throws.
func answerConditional(src Source, ask *condAsk, forked bool) error {
	var last error
	for i := range ask.asked {
		view, ok := ask.asked[i].view(src)
		if !ok {
			continue
		}
		result, ok, err := answerOne(view, ask, ask.answers[i], forked)
		reportResult(src, ok && err == nil, result)
		if serr := ask.asked[i].report(result > 0, result); serr != nil {
			return serr
		}
		// A pass or a heard refusal is the answer; a silent one is not.
		if err != nil || result > 0 {
			last = err
		}
	}
	return last
}

// answerOne answers the conditional in one context with the test's return
// value: a passing numeric if's count, 1 for other passes, 0 for a failure
// or throw; false for a failure even when a fork swallows it. A numeric test
// passes on its flag, not its count: blocks can pass counting 0 (masked air)
// and fail with the partial count before its mismatch.
// vanilla: ExecuteCommand.addConditional, createNumericConditionalHandler,
// checkIfRegions, checkUnlessRegions
func answerOne(view Source, ask *condAsk, ans condAnswer, forked bool) (int32, bool, error) {
	if ans.throwKey != "" {
		if forked {
			return 0, false, nil
		}
		return 0, false, refuse(view, ans.throwKey, ans.throwWith...)
	}
	if ask.numeric {
		if ask.expected {
			if ans.passed {
				count := int64(ans.count)
				return int32(ans.count), true, view.Feedback(conditionalPassCountKey, protocol.Text{Number: &count})
			}
			// The refusal a failing test throws: unheard where the
			// chain has forked.
			if forked {
				return 0, false, nil
			}
			return 0, false, refuse(view, conditionalFailKey)
		}
		if !ans.passed {
			return 1, true, view.Feedback(conditionalPassKey)
		}
		if forked {
			return 0, false, nil
		}
		count := int64(ans.count)
		return 0, false, refuse(view, conditionalFailCountKey, protocol.Text{Number: &count})
	}
	if ans.passed == ask.expected {
		return 1, true, view.Feedback(conditionalPassKey)
	}
	if forked {
		return 0, false, nil
	}
	return 0, false, refuse(view, conditionalFailKey)
}

// stepAs splits the execution over the bodies the word names, each becoming
// the body the rest of the chain's @s names.
func stepAs(src Source, ctxs []execContext, rest []string, tokens []Token, at int) ([]execContext, int, error) {
	word, span := selectorArgAt(tokens, at+1)
	next, _, err := stepFork(src, ctxs, word, func(c *execContext, e Entity) {
		c.executor = e
		c.hasExec = true
	})
	return next, 1 + span, err
}

// stepAt splits the execution over the bodies the word names, the chain
// reading the world from where each stands. The body they leave behind is
// not one of theirs — an at does not change whose @s the chain names.
func stepAt(src Source, ctxs []execContext, rest []string, tokens []Token, at int) ([]execContext, int, error) {
	word, span := selectorArgAt(tokens, at+1)
	next, _, err := stepFork(src, ctxs, word, func(c *execContext, e Entity) {
		c.pos = e.Position()
		c.dim = e.DimensionName()
	})
	return next, 1 + span, err
}

// stepPositioned settles where the chain reads the world from: the position
// named, over the heightmap of the column it names, or wherever the bodies
// named stand. A position of its own points the anchor back at the feet.
func stepPositioned(src Source, ctxs []execContext, forked bool, rest []string, tokens []Token, at int) ([]execContext, int, bool, error) {
	switch rest[1] {
	case "as":
		word, span := selectorArgAt(tokens, at+2)
		next, _, err := stepFork(src, ctxs, word, func(c *execContext, e Entity) {
			p := e.Position()
			c.pos.X, c.pos.Y, c.pos.Z = p.X, p.Y, p.Z
		})
		return next, 2 + span, true, err
	case "over":
		next := make([]execContext, 0, len(ctxs))
		for _, ctx := range ctxs {
			view, ok := ctx.view(src)
			if !ok {
				continue
			}
			x, z := rules.BlockOf(ctx.pos.X), rules.BlockOf(ctx.pos.Z)
			if !view.BlockLoaded(x, 0, z) {
				if err := refuseStep(view, forked, posUnloadedKey); err != nil {
					return nil, 0, false, err
				}
				continue
			}
			copied := ctx
			copied.pos.Y = float64(view.HeightmapHeight(rest[2], x, z))
			next = append(next, copied)
		}
		return next, 3, false, nil
	default:
		next := make([]execContext, 0, len(ctxs))
		for _, ctx := range ctxs {
			view, ok := ctx.view(src)
			if !ok {
				continue
			}
			x, y, z, ok := resolveVec3(view, rest[1:4])
			if !ok {
				src.Debug("execute positioned arguments did not parse", "args", rest[1:4])
				continue
			}
			copied := ctx
			copied.pos = player.Position{X: x, Y: y, Z: z, Yaw: copied.pos.Yaw, Pitch: copied.pos.Pitch}
			copied.eyes = false
			next = append(next, copied)
		}
		return next, 4, false, nil
	}
}

// stepRotated settles which way the chain faces: the rotation named, or the
// way the bodies named face. Nothing else of the context moves.
func stepRotated(src Source, ctxs []execContext, forked bool, rest []string, tokens []Token, at int) ([]execContext, int, bool, error) {
	switch rest[1] {
	case "as":
		word, span := selectorArgAt(tokens, at+2)
		next, _, err := stepFork(src, ctxs, word, func(c *execContext, e Entity) {
			c.pos.Yaw = e.Position().Yaw
			c.pos.Pitch = e.Position().Pitch
		})
		return next, 2 + span, true, err
	default:
		next := make([]execContext, 0, len(ctxs))
		for _, ctx := range ctxs {
			yaw, pitch, ok := parseRotation(rest[1:3])
			if !ok {
				src.Debug("execute rotated arguments did not parse", "args", rest[1:3])
				continue
			}
			copied := ctx
			copied.pos.Yaw = float32(yaw.resolve(float64(ctx.pos.Yaw)))
			copied.pos.Pitch = float32(pitch.resolve(float64(ctx.pos.Pitch)))
			next = append(next, copied)
		}
		return next, 3, false, nil
	}
}

// stepFacing turns the chain to look at something: a position, or a point of
// a body — its feet or its eyes — with the execution split over the bodies
// the facing entity names.
func stepFacing(src Source, ctxs []execContext, forked bool, rest []string, tokens []Token, at int) ([]execContext, int, bool, error) {
	if rest[1] != "entity" {
		next := make([]execContext, 0, len(ctxs))
		for _, ctx := range ctxs {
			view, ok := ctx.view(src)
			if !ok {
				continue
			}
			x, y, z, ok := resolveVec3(view, rest[1:4])
			if !ok {
				src.Debug("execute facing arguments did not parse", "args", rest[1:4])
				continue
			}
			yaw, pitch := lookAnglesFrom(ctx.anchorPoint(), player.Position{X: x, Y: y, Z: z})
			copied := ctx
			copied.pos.Yaw, copied.pos.Pitch = yaw, pitch
			next = append(next, copied)
		}
		return next, 4, false, nil
	}
	word, span := selectorArgAt(tokens, at+2)
	anchor := rest[2+span]
	limit := forkLimit(src)
	next := make([]execContext, 0, len(ctxs))
	for _, ctx := range ctxs {
		view, ok := ctx.view(src)
		if !ok {
			continue
		}
		bodies, notFound := resolveRequiredEntities(view, word)
		if len(next)+len(bodies) >= limit {
			return nil, 3 + span, true, errForkLimit
		}
		if notFound != "" {
			// A fork's refusal is swallowed whole: the context simply
			// drops, and a chain left with nothing stops without a word.
			continue
		}
		for _, entity := range bodies {
			to := entity.Position()
			if anchor == "eyes" {
				to = entity.EyePosition()
			}
			yaw, pitch := lookAnglesFrom(ctx.anchorPoint(), to)
			copied := ctx
			copied.pos.Yaw, copied.pos.Pitch = yaw, pitch
			next = append(next, copied)
		}
	}
	return next, 3 + span, true, nil
}

// stepAlign floors the coordinates of the position the axes name, so that
// the chain reads the world from the corner of the block it stands in.
func stepAlign(src Source, ctxs []execContext, forked bool, rest []string) ([]execContext, int, error) {
	next := make([]execContext, 0, len(ctxs))
	for _, ctx := range ctxs {
		copied := ctx
		for _, axis := range rest[1] {
			switch axis {
			case 'x':
				copied.pos.X = float64(rules.BlockOf(copied.pos.X))
			case 'y':
				copied.pos.Y = float64(rules.BlockOf(copied.pos.Y))
			case 'z':
				copied.pos.Z = float64(rules.BlockOf(copied.pos.Z))
			}
		}
		next = append(next, copied)
	}
	return next, 2, nil
}

// stepAnchored settles which end of the context's body the chain aims its
// facings from, the eyes above the feet when it has a body with eyes.
func stepAnchored(src Source, ctxs []execContext, forked bool, rest []string) ([]execContext, int, error) {
	next := make([]execContext, 0, len(ctxs))
	for _, ctx := range ctxs {
		copied := ctx
		copied.eyes = rest[1] == "eyes"
		next = append(next, copied)
	}
	return next, 2, nil
}

// stepIn moves the chain to the dimension named, its position stretched by
// the ratio of the two dimensions' coordinate scales, so that the same
// block corner is meant in either.
func stepIn(src Source, ctxs []execContext, forked bool, rest []string) ([]execContext, int, error) {
	// The word may leave out the minecraft: namespace, as a resource
	// location may; the context holds the full name findDimension matches.
	dim := registry.Qualify(rest[1])
	if !dimensionKnown(src, dim) {
		// The modifier throws per context, so a chain with none left
		// never hears it. vanilla: BuildContexts.execute
		for _, ctx := range ctxs {
			view, ok := ctx.view(src)
			if !ok {
				continue
			}
			if err := refuseStep(view, forked, dimensionInvalidKey, protocol.Text{Text: dim}); err != nil {
				return nil, 0, err
			}
		}
		return nil, 2, nil
	}
	next := make([]execContext, 0, len(ctxs))
	for _, ctx := range ctxs {
		copied := ctx
		if copied.dim != dim {
			scale := scaleBetween(copied.dim, dim)
			copied.pos.X *= scale
			copied.pos.Z *= scale
		}
		copied.dim = dim
		next = append(next, copied)
	}
	return next, 2, nil
}

// stepSummon stands the kind of thing named up where the chain reads the
// world from, and the chain goes on as its body: an @s of the tail names the
// one the command stood up, not the runner.
func stepSummon(src Source, ctxs []execContext, forked bool, rest []string) ([]execContext, int, error) {
	name, ok := parseResourceLocation(rest[1])
	next := make([]execContext, 0, len(ctxs))
	if !ok || !entityTypeExists(name) || !entityTypeSummonable(name) {
		// A kind the game will not stand up refuses where the vanilla
		// argument's own check does.
		for _, ctx := range ctxs {
			view, ok := ctx.view(src)
			if !ok {
				continue
			}
			if err := refuseStep(view, forked, summonNotSummonableKey, protocol.Text{Text: registry.Qualify(rest[1])}); err != nil {
				return nil, 0, err
			}
		}
		return nil, 2, nil
	}
	kind := summonKindOf(name)
	if kind == summonNone {
		// The kind is one this server simulates nothing behind: the
		// creation comes to nothing, which is commands.summon.failed.
		for _, ctx := range ctxs {
			view, ok := ctx.view(src)
			if !ok {
				continue
			}
			if err := refuseStep(view, forked, summonFailedKey); err != nil {
				return nil, 0, err
			}
		}
		return nil, 2, nil
	}
	for _, ctx := range ctxs {
		view, ok := ctx.view(src)
		if !ok {
			continue
		}
		copied := ctx
		var body Entity
		// vanilla: ExecuteCommand.spawnEntityAndRedirect, SummonCommand.createEntity
		var id uuid.UUID
		var made bool
		var err error
		if kind == summonItem {
			// An empty item is discarded by vanilla's reader yet still
			// answers as the chain's @s; summonBody's air item registers
			// nothing, so the step spawns the empty stack itself.
			_, id, err = view.SummonItem(ctx.pos.X, ctx.pos.Y, ctx.pos.Z, protocol.ItemStack{}, 10, false, nil)
			made = true
		} else {
			_, id, _, made, err = summonBody(view, name, kind, ctx.pos.X, ctx.pos.Y, ctx.pos.Z, nil, nil, nil, false)
		}
		if made && err == nil {
			if found := view.Entities(EntityQuery{UUID: &id}); len(found) > 0 {
				body = found[0]
			}
		}
		if body != nil {
			copied.executor = body
			copied.hasExec = true
		} else {
			if err := refuseStep(view, forked, summonFailedKey); err != nil {
				return nil, 0, err
			}
			continue
		}
		next = append(next, copied)
	}
	return next, 2, nil
}

// stepOn splits each context over the entities its executor stands in the
// named relation to, each becoming the executor with position and rotation
// kept. A context without an executor, or an executor that tracks no
// relations, drops.
// vanilla: ExecuteCommand.expandOneToOneEntityRelation, expandOneToManyEntityRelation
func stepOn(src Source, ctxs []execContext, forked bool, rest []string) ([]execContext, int, error) {
	limit := forkLimit(src)
	next := make([]execContext, 0, len(ctxs))
	for _, ctx := range ctxs {
		if !ctx.hasExec {
			continue
		}
		rel, ok := ctx.executor.(Related)
		if !ok {
			continue
		}
		found := rel.Relation(rest[1])
		if len(next)+len(found) >= limit {
			return nil, 0, errForkLimit
		}
		for _, e := range found {
			copied := ctx
			copied.executor = e
			next = append(next, copied)
		}
	}
	return next, 2, nil
}

// condAnswer is one context's answer: passed against the ask's expectation,
// the count numeric tests report, and the key of a thrown reading.
type condAnswer struct {
	passed    bool
	count     int
	throwKey  string
	throwWith []protocol.Text
}

// condAsk is what the chain's last conditional asked and what every context
// answered, kept for the chain's own end: a chain that stops on its
// conditional answers the test in each context rather than running on.
type condAsk struct {
	expected bool
	numeric  bool
	asked    []execContext
	answers  []condAnswer
	// The one-context case, by far the commonest, keeps its slices in the
	// ask instead of allocating each.
	askedBuf   [1]execContext
	answersBuf [1]condAnswer
	nextBuf    [1]execContext
}

// stepConditional reads one conditional and answers it in every context,
// keeping those that match. Tests without a registry here refuse as their
// empty registry would.
func stepConditional(src Source, ctxs []execContext, forked bool, expected bool, rest []string, tokens []Token, at int) ([]execContext, int, *condAsk, error) {
	switch rest[1] {
	case "entity":
		word, span := selectorArgAt(tokens, at+2)
		next, ask := expectOver(src, ctxs, expected, true, func(view Source) condAnswer {
			found := resolveOptionalEntities(view, word)
			return condAnswer{passed: len(found) > 0, count: len(found)}
		})
		return next, 2 + span, ask, nil
	case "block":
		coords := rest[2:5]
		span := blockSpanCount(rest[5:])
		pred := wordsInTokens(tokens, rest, 5, span)
		next, ask := expectOver(src, ctxs, expected, false, func(view Source) condAnswer {
			x, y, z, ok := resolveBlockPos(view, coords)
			if !ok {
				src.Debug("execute if block position did not parse", "args", coords)
				return condAnswer{}
			}
			if !view.BlockLoaded(x, y, z) {
				return condAnswer{throwKey: posUnloadedKey}
			}
			state := view.BlockAt(x, y, z)
			predicate, ok := parseBlockPredicate(pred)
			if !ok {
				src.Debug("execute if block predicate did not parse", "predicate", pred)
				return condAnswer{}
			}
			return condAnswer{passed: predicate.matches(view, x, y, z, state)}
		})
		return next, 5 + span, ask, nil
	case "blocks":
		startW, endW, destW := rest[2:5], rest[5:8], rest[8:11]
		skipAir := rest[11] == "masked"
		next, ask := expectOver(src, ctxs, expected, true, func(view Source) condAnswer {
			sx, sy, sz, ok := resolveBlockPos(view, startW)
			if !ok {
				src.Debug("execute if blocks start did not parse", "args", startW)
				return condAnswer{}
			}
			if !view.BlockLoaded(sx, sy, sz) {
				return condAnswer{throwKey: posUnloadedKey}
			}
			ex, ey, ez, ok := resolveBlockPos(view, endW)
			if !ok {
				src.Debug("execute if blocks end did not parse", "args", endW)
				return condAnswer{}
			}
			if !view.BlockLoaded(ex, ey, ez) {
				return condAnswer{throwKey: posUnloadedKey}
			}
			dx0, dy0, dz0, ok := resolveBlockPos(view, destW)
			if !ok {
				src.Debug("execute if blocks destination did not parse", "args", destW)
				return condAnswer{}
			}
			if !view.BlockLoaded(dx0, dy0, dz0) {
				return condAnswer{throwKey: posUnloadedKey}
			}
			loX, hiX := min(sx, ex), max(sx, ex)
			loY, hiY := min(sy, ey), max(sy, ey)
			loZ, hiZ := min(sz, ez), max(sz, ez)
			area := int64(hiX-loX+1) * int64(hiY-loY+1) * int64(hiZ-loZ+1)
			if area > maxIfBlocksArea {
				capN, areaN := int64(maxIfBlocksArea), area
				return condAnswer{
					throwKey:  blocksTooBigKey,
					throwWith: []protocol.Text{{Number: &capN}, {Number: &areaN}},
				}
			}
			offX, offY, offZ := dx0-loX, dy0-loY, dz0-loZ
			count := 0
			for z := loZ; z <= hiZ; z++ {
				for y := loY; y <= hiY; y++ {
					for x := loX; x <= hiX; x++ {
						source := view.BlockAt(x, y, z)
						if skipAir && source == airState {
							continue
						}
						if source != view.BlockAt(x+offX, y+offY, z+offZ) {
							return condAnswer{}
						}
						if !sameBlockEntity(view, x, y, z, x+offX, y+offY, z+offZ) {
							return condAnswer{}
						}
						count++
					}
				}
			}
			return condAnswer{passed: true, count: count}
		})
		return next, 12, ask, nil
	case "data":
		return stepDataConditional(src, ctxs, expected, rest, tokens, at)
	case "predicate":
		// The argument is a pack predicate's name or an inline one.
		text, span := itemPredicateText(tokens, at+2)
		next, ask := expectOver(src, ctxs, expected, false, func(view Source) condAnswer {
			cond, err := parsePredicate(text, nil)
			if err != nil {
				return condAnswer{}
			}
			// vanilla: ExecuteCommand.checkCustomPredicate - ORIGIN is the position, THIS_ENTITY the executor
			ctx := rules.LootContext{}
			if self := view.SelfEntity(); self != nil {
				ctx.This = lootEntityOf(self)
			}
			fillLootWorld(&ctx, view, view.Position())
			return condAnswer{passed: rules.TestLootCondition(cond, &ctx, view.Random())}
		})
		return next, 2 + span, ask, nil
	case "dimension":
		dim := registry.Qualify(rest[2])
		next, ask := expectOver(src, ctxs, expected, false, func(view Source) condAnswer {
			if !dimensionKnown(src, dim) {
				return condAnswer{throwKey: dimensionInvalidKey, throwWith: []protocol.Text{{Text: dim}}}
			}
			return condAnswer{passed: view.DimensionName() == dim}
		})
		return next, 3, ask, nil
	case "loaded":
		next, ask := expectOver(src, ctxs, expected, false, func(view Source) condAnswer {
			x, y, z, ok := resolveBlockPos(view, rest[2:5])
			if !ok {
				src.Debug("execute if loaded position did not parse", "args", rest[2:5])
				return condAnswer{}
			}
			return condAnswer{passed: view.BlockLoaded(x, y, z)}
		})
		return next, 5, ask, nil
	case "biome":
		coords, biomeWord := rest[2:5], rest[5]
		next, ask := expectOver(src, ctxs, expected, false, func(view Source) condAnswer {
			x, y, z, ok := resolveBlockPos(view, coords)
			if !ok {
				src.Debug("execute if biome position did not parse", "args", coords)
				return condAnswer{}
			}
			if !view.BlockLoaded(x, y, z) {
				return condAnswer{throwKey: posUnloadedKey}
			}
			biome := view.BiomeAt(x, y, z)
			if strings.HasPrefix(biomeWord, "#") {
				return condAnswer{passed: registry.BiomeInTag(biome, registry.Qualify(biomeWord[1:]))}
			}
			return condAnswer{passed: biome == registry.Qualify(biomeWord)}
		})
		return next, 6, ask, nil
	case "score":
		return stepScoreConditional(src, ctxs, expected, rest, tokens, at)
	case "stopwatch":
		id, ok := parseResourceLocation(rest[2])
		if !ok {
			src.Debug("execute if stopwatch id is not a resource location", "id", rest[2])
			return nil, 4, nil, nil
		}
		secondsRange, err := parseFloatRange(rest[3])
		if err != nil {
			src.Debug("execute if stopwatch range did not parse", "range", rest[3])
			return nil, 4, nil, nil
		}
		next, ask := expectOver(src, ctxs, expected, false, func(view Source) condAnswer {
			seconds, ok := view.StopwatchSeconds(id)
			if !ok {
				return condAnswer{throwKey: stopwatchDoesNotExistKey, throwWith: []protocol.Text{{Text: id}}}
			}
			return condAnswer{passed: secondsRange.Matches(seconds)}
		})
		return next, 4, ask, nil
	case "items", "slots":
		return stepItemsConditional(src, ctxs, expected, rest, tokens, at)
	}
	// The function test is answered by walkExecute, so no input the walk
	// accepted reaches here.
	src.Debug("execute conditional names a test this server does not answer", "test", rest[1])
	return nil, 2, nil, nil
}

// stepDataConditional tests a block entity's, body's or storage's data at a
// path, counting matches. A block without a block entity refuses; a body's
// data, or an empty storage, matches nothing.
// vanilla: ExecuteCommand.checkMatchingData
func stepDataConditional(src Source, ctxs []execContext, expected bool, rest []string, tokens []Token, at int) ([]execContext, int, *condAsk, error) {
	switch rest[2] {
	case "block":
		coords := rest[3:6]
		return dataConditionalOverPath(src, ctxs, expected, rest[6:], tokens, ownedData(func(view Source) (nbt.CompoundTag, condAnswer) {
			x, y, z, ok := resolveBlockPos(view, coords)
			if !ok {
				src.Debug("execute if data position did not parse", "args", coords)
				return nil, condAnswer{}
			}
			if !view.BlockLoaded(x, y, z) {
				return nil, condAnswer{throwKey: posUnloadedKey}
			}
			if !view.InWorldBounds(x, y, z) {
				return nil, condAnswer{throwKey: posOutOfWorldKey}
			}
			tag, has := view.BlockEntityTagAt(x, y, z)
			if !has {
				return nil, condAnswer{throwKey: dataBlockInvalidKey}
			}
			return tag, condAnswer{}
		}), 6)
	case "entity":
		word, span := selectorArgAt(tokens, at+3)
		return dataConditionalOverPath(src, ctxs, expected, rest[3+span:], tokens, ownedData(func(view Source) (nbt.CompoundTag, condAnswer) {
			bodies, notFound := resolveRequiredEntities(view, word)
			if notFound != "" {
				return nil, condAnswer{throwKey: notFound}
			}
			if len(bodies) == 0 {
				return nil, condAnswer{throwKey: argumentEntityNotFoundKey}
			}
			tagged, ok := bodies[0].(DataTarget)
			if !ok {
				return nil, condAnswer{}
			}
			tag, has := tagged.DataTag()
			if !has {
				return nil, condAnswer{}
			}
			return tag, condAnswer{}
		}), 3+span)
	default:
		id := rest[3]
		return dataConditionalOverPath(src, ctxs, expected, rest[4:], tokens, func(view Source, count func(nbt.CompoundTag) int) (int, condAnswer) {
			n := 0
			view.ReadCommandStorage(id, func(storage nbt.CompoundTag) { n = count(storage) })
			return n, condAnswer{}
		}, 4)
	}
}

// ownedData adapts a read of data the caller already holds (a block's or a
// body's) to the count-inside-the-read form dataConditionalOverPath takes.
func ownedData(read func(Source) (nbt.CompoundTag, condAnswer)) func(Source, func(nbt.CompoundTag) int) (int, condAnswer) {
	return func(view Source, count func(nbt.CompoundTag) int) (int, condAnswer) {
		data, answer := read(view)
		if answer.throwKey != "" {
			return 0, answer
		}
		return count(data), condAnswer{}
	}
}

// dataConditionalOverPath reads each context's source data (a refusing read
// answering its refusal) and counts the path's matches. base is the words
// consumed before the path.
// vanilla: ExecuteCommand.checkMatchingData
func dataConditionalOverPath(src Source, ctxs []execContext, expected bool, rest []string, tokens []Token, read func(src Source, count func(nbt.CompoundTag) int) (int, condAnswer), base int) ([]execContext, int, *condAsk, error) {
	span := pathSpanOf(rest)
	path, err := parseNBTPath(wordsInTokens(tokens, rest, 0, span))
	if err != nil {
		src.Debug("execute if data path did not parse", "path", rest[:span])
		next, ask := expectOver(src, ctxs, expected, true, func(Source) condAnswer {
			return condAnswer{}
		})
		return next, base + span, ask, nil
	}
	next, ask := expectOver(src, ctxs, expected, true, func(view Source) condAnswer {
		count, answer := read(view, func(data nbt.CompoundTag) int { return path.countMatching(data) })
		if answer.throwKey != "" {
			return answer
		}
		return condAnswer{passed: count > 0, count: count}
	})
	return next, base + span, ask, nil
}

// stepScoreConditional compares a holder's score with another's or a range;
// a holder without a score fails without an error.
// vanilla: ExecuteCommand.checkScore
func stepScoreConditional(src Source, ctxs []execContext, expected bool, rest []string, tokens []Token, at int) ([]execContext, int, *condAsk, error) {
	targetWord, s1 := selectorArgAt(tokens, at+2)
	targetObj := rest[2+s1]
	cmp := rest[3+s1]
	if cmp == "matches" {
		scoreRange := readIntRange(rest[4+s1])
		if scoreRange == nil {
			src.Debug("execute if score range did not parse", "range", rest[4+s1])
			return nil, 5 + s1, nil, nil
		}
		next, ask := expectOver(src, ctxs, expected, false, func(view Source) condAnswer {
			target, ans, ok := scoreRef(view, targetWord, targetObj)
			if !ok {
				return ans
			}
			score, has := view.Scoreboard().Score(target, targetObj)
			return condAnswer{passed: has && scoreRange.Matches(float64(score.Value))}
		})
		return next, 5 + s1, ask, nil
	}
	sourceWord, s2 := selectorArgAt(tokens, at+4+s1)
	sourceObj := rest[4+s1+s2]
	compare := scoreOperator(cmp)
	next, ask := expectOver(src, ctxs, expected, false, func(view Source) condAnswer {
		// Both references are resolved before either score is read, so a
		// missing score never hides the source's refusal.
		target, ans, ok := scoreRef(view, targetWord, targetObj)
		if !ok {
			return ans
		}
		source, ans, ok := scoreRef(view, sourceWord, sourceObj)
		if !ok {
			return ans
		}
		a, okA := view.Scoreboard().Score(target, targetObj)
		b, okB := view.Scoreboard().Score(source, sourceObj)
		return condAnswer{passed: okA && okB && compare(a.Value, b.Value)}
	})
	return next, 5 + s1 + s2, ask, nil
}

// scoreRef resolves the word's first holder and checks the objective; false
// when either throws, ans holding the refusal.
// vanilla: ScoreHolderArgument.getName, ObjectiveArgument.getObjective
func scoreRef(view Source, word, objective string) (holder string, ans condAnswer, ok bool) {
	board := view.Scoreboard()
	if strings.HasPrefix(word, "#") {
		// vanilla: ScoreHolderArgument.getName; a fake player's name needs no lookup
		if _, found := board.Objective(objective); !found {
			return "", condAnswer{throwKey: objectiveNotFoundKey, throwWith: []protocol.Text{{Text: objective}}}, false
		}
		return word, condAnswer{}, true
	}
	holders, key := scoreHolders(view, board, word, false)
	if key != "" {
		return "", condAnswer{throwKey: key}, false
	}
	if _, found := board.Objective(objective); !found {
		return "", condAnswer{throwKey: objectiveNotFoundKey, throwWith: []protocol.Text{{Text: objective}}}, false
	}
	if len(holders) == 0 {
		return "", condAnswer{}, false
	}
	return holders[0].Name, condAnswer{}, true
}

// scoreOperator is the comparison a score test's operator literal names.
func scoreOperator(op string) func(a, b int32) bool {
	switch op {
	case "=":
		return func(a, b int32) bool { return a == b }
	case "<":
		return func(a, b int32) bool { return a < b }
	case "<=":
		return func(a, b int32) bool { return a <= b }
	case ">":
		return func(a, b int32) bool { return a > b }
	default:
		return func(a, b int32) bool { return a >= b }
	}
}

// stepItemsConditional answers the items test, which counts every item of
// the stacks in the named slots of bodies or a container block that match a
// predicate, and the slots test, which counts the named slots that exist.
// vanilla: ExecuteCommand.countItems, countSlots
func stepItemsConditional(src Source, ctxs []execContext, expected bool, rest []string, tokens []Token, at int) ([]execContext, int, *condAsk, error) {
	countSlots := rest[1] == "slots"
	block := rest[2] == "block"
	var word string
	slotsAt := 6
	if !block {
		var span int
		word, span = selectorArgAt(tokens, at+3)
		slotsAt = 3 + span
	}
	sourceSpan := min(spanCount(tokens[at+slotsAt:], tokenText, true, false), len(tokens)-at-slotsAt)
	source, _, perr := parseSlotSource(joinSelector(tokens[at+slotsAt : at+slotsAt+sourceSpan]).text)
	test, predSpan := itemsConditionalTest(countSlots, tokens, at+slotsAt+sourceSpan)
	arity := slotsAt + sourceSpan + predSpan
	if !block {
		next, ask := expectOver(src, ctxs, expected, true, func(view Source) condAnswer {
			found, notFound := resolveRequiredEntities(view, word)
			if notFound != "" {
				return condAnswer{throwKey: notFound}
			}
			if perr != nil {
				return condAnswer{}
			}
			count := 0
			for _, entity := range found {
				for _, stack := range slotSourceStacks(view, source, func(name string) []rules.SlotAccess { return entitySlotAccess(entity, name) }) {
					count += test(stack)
				}
			}
			return condAnswer{passed: count > 0, count: count}
		})
		return next, arity, ask, nil
	}
	coords := rest[3:6]
	next, ask := expectOver(src, ctxs, expected, true, func(view Source) condAnswer {
		x, y, z, ok := resolveBlockPos(view, coords)
		if !ok {
			src.Debug("execute items or slots position did not parse", "args", coords)
			return condAnswer{}
		}
		if !view.BlockLoaded(x, y, z) {
			return condAnswer{throwKey: posUnloadedKey}
		}
		size := view.ContainerSizeAt(x, y, z)
		if size == 0 {
			// vanilla: BlockItemAccessor.getSlots, ItemCommands.ERROR_SOURCE_NOT_A_CONTAINER
			return condAnswer{throwKey: itemSourceNoContainerKey, throwWith: []protocol.Text{
				entityCount(int(x)), entityCount(int(y)), entityCount(int(z)),
			}}
		}
		if perr != nil {
			return condAnswer{}
		}
		count := 0
		for _, stack := range slotSourceStacks(view, source, func(name string) []rules.SlotAccess {
			return blockSlotAccess(view, x, y, z, size, name)
		}) {
			count += test(stack)
		}
		return condAnswer{passed: count > 0, count: count}
	})
	return next, arity, ask, nil
}

// itemsConditionalTest is what one existing slot adds to the count, and the
// tokens the item predicate spans: 1 per slot for the slots test, the
// matching stack's item count for the items test.
func itemsConditionalTest(countSlots bool, tokens []Token, at int) (func(protocol.ItemStack) int, int) {
	if countSlots {
		return func(protocol.ItemStack) int { return 1 }, 0
	}
	span := max(itemStackSpanCount(tokens[at:]), 1)
	if at+span > len(tokens) {
		span = len(tokens) - at
	}
	match, perr := compileItemPredicate(joinSelector(tokens[at : at+span]))
	if perr != nil {
		match = neverClear
	}
	return func(stack protocol.ItemStack) int {
		if match(stack) {
			return int(stack.Count)
		}
		return 0
	}, span
}

// itemPredicateText reads the item predicate argument at the token index and
// joins the words it spans back the way the predicate reader would have read
// them.
func itemPredicateText(tokens []Token, at int) (string, int) {
	span := max(itemStackSpanCount(tokens[at:]), 1)
	if at+span > len(tokens) {
		span = len(tokens) - at
	}
	return joinSelector(tokens[at : at+span]).text, span
}

// stepStore reads one store: each kind resolves its target in each context,
// refusing one that does not exist, and hangs a store on it.
func stepStore(src Source, ctxs []execContext, forked bool, rest []string, tokens []Token, at int) ([]execContext, int, error) {
	kind := rest[2]
	switch kind {
	case "score":
		word, span := selectorArgAt(tokens, at+3)
		objective := rest[3+span]
		storeResult := rest[1] == "result"
		next := make([]execContext, 0, len(ctxs))
		for _, ctx := range ctxs {
			view, ok := ctx.view(src)
			if !ok {
				continue
			}
			board := view.Scoreboard()
			holders, key := scoreHolders(view, board, word, true)
			if key != "" {
				if err := refuseStep(view, forked, key); err != nil {
					return nil, 0, err
				}
				continue
			}
			// vanilla: ObjectiveArgument.getObjective, not the writable
			// lookup: a read-only objective throws only once written.
			if _, ok := board.Objective(objective); !ok {
				if err := refuseStep(view, forked, objectiveNotFoundKey, protocol.Text{Text: objective}); err != nil {
					return nil, 0, err
				}
				continue
			}
			ctx.stores = append(slices.Clip(ctx.stores), func(success bool, result int32) error {
				if errors.Is(board.Store(boardHolders(holders), objective, storedValue(storeResult, success, result)), scoreboard.ErrReadOnlyScore) {
					return errStoreThrew
				}
				return nil
			})
			next = append(next, ctx)
		}
		return next, 4 + span, nil
	case "bossbar":
		id, ok := parseResourceLocation(rest[3])
		if !ok {
			src.Debug("execute store bossbar id did not parse", "id", rest[3])
			return nil, 5, nil
		}
		intoValue := len(rest) > 4 && rest[4] == "value"
		next := make([]execContext, 0, len(ctxs))
		for _, ctx := range ctxs {
			view, ok := ctx.view(src)
			if !ok {
				continue
			}
			if !slices.Contains(view.BossBarIDs(), id) {
				if err := refuseStep(view, forked, bossbarUnknownKey, protocol.Text{Text: id}); err != nil {
					return nil, 0, err
				}
				continue
			}
			storeResult := rest[1] == "result"
			ctx.stores = append(slices.Clip(ctx.stores), func(success bool, result int32) error {
				value := storedValue(storeResult, success, result)
				if intoValue {
					view.SetBossBarValue(id, value)
				} else {
					view.SetBossBarMax(id, value)
				}
				return nil
			})
			next = append(next, ctx)
		}
		return next, 5, nil
	case "block":
		coords := rest[3:6]
		pathSpan := pathSpanOf(rest[6:])
		next := make([]execContext, 0, len(ctxs))
		for _, ctx := range ctxs {
			view, ok := ctx.view(src)
			if !ok {
				continue
			}
			x, y, z, ok := resolveBlockPos(view, coords)
			if !ok {
				src.Debug("execute store position did not parse", "args", coords)
				continue
			}
			key := ""
			switch {
			case !view.BlockLoaded(x, y, z):
				key = posUnloadedKey
			case !view.InWorldBounds(x, y, z):
				key = posOutOfWorldKey
			default:
				if _, has := view.BlockEntityTagAt(x, y, z); !has {
					key = dataBlockInvalidKey
				}
			}
			if key != "" {
				if err := refuseStep(view, forked, key); err != nil {
					return nil, 0, err
				}
				continue
			}
			target := dataTarget{src: view, kind: dataTargetBlock, x: x, y: y, z: z}
			if next, ok = withNBTStore(next, ctx, target, rest, tokens, 6, pathSpan); !ok {
				src.Debug("execute store path or scale did not parse", "args", rest[6:])
			}
		}
		return next, 8 + pathSpan, nil
	case "entity":
		word, span := selectorArgAt(tokens, at+3)
		pathSpan := pathSpanOf(rest[3+span:])
		next := make([]execContext, 0, len(ctxs))
		for _, ctx := range ctxs {
			view, ok := ctx.view(src)
			if !ok {
				continue
			}
			bodies, notFound := resolveRequiredEntities(view, word)
			if notFound != "" {
				if err := refuseStep(view, forked, notFound); err != nil {
					return nil, 0, err
				}
				continue
			}
			target := dataTarget{src: view, kind: dataTargetEntity, entity: bodies[0]}
			if writer, ok := bodies[0].(DataWriter); ok {
				target.writeEntity = writer.SetDataTag
			}
			if next, ok = withNBTStore(next, ctx, target, rest, tokens, 3+span, pathSpan); !ok {
				src.Debug("execute store path or scale did not parse", "args", rest[3+span:])
			}
		}
		return next, 5 + span + pathSpan, nil
	default:
		// The paths every store reads may span the words their quoted
		// keys and their matchers opened across.
		pathSpan := pathSpanOf(rest[4:])
		id, _, _ := readIdentifier(rest[3])
		next := make([]execContext, 0, len(ctxs))
		for _, ctx := range ctxs {
			view, ok := ctx.view(src)
			if !ok {
				continue
			}
			target := dataTarget{src: view, kind: dataTargetStorage, id: id}
			if next, ok = withNBTStore(next, ctx, target, rest, tokens, 4, pathSpan); !ok {
				src.Debug("execute store path or scale did not parse", "args", rest[4:])
			}
		}
		return next, 6 + pathSpan, nil
	}
}

// withNBTStore appends ctx to next carrying one more store, the NBT kind's
// write into target: the path at rest[at:at+pathSpan], then the type word
// and the scale. ok is false where those did not read, ctx then dropped.
func withNBTStore(next []execContext, ctx execContext, target dataTarget, rest []string, tokens []Token, at, pathSpan int) ([]execContext, bool) {
	if at+pathSpan+2 > len(rest) {
		return next, false
	}
	path, err := parseNBTPath(wordsInTokens(tokens, rest, at, pathSpan))
	if err != nil {
		return next, false
	}
	scale, perr := readDoubleWord(rest[at+pathSpan+1])
	if perr != nil {
		return next, false
	}
	typeWord := rest[at+pathSpan]
	storeResult := rest[1] == "result"
	// A fresh backing array: the contexts a fork split share the stores
	// they were split with.
	ctx.stores = append(slices.Clip(ctx.stores), func(success bool, result int32) error {
		value := storedValue(storeResult, success, result)
		target.storeData(path, scaledTag(typeWord, float64(value)*scale))
		return nil
	})
	return append(next, ctx), true
}

// storeData sets tag at path in the target's compound and writes it back; an
// unsettable path or a body with no writable form drops the write silently.
// vanilla: ExecuteCommand.storeData
func (t dataTarget) storeData(path nbtPath, tag nbt.Tag) {
	if t.kind == dataTargetStorage {
		_ = t.modifyData(path, func(path nbtPath, data nbt.CompoundTag) bool {
			changed, refusal := path.set(data, tag)
			return changed > 0 && refusal == nil
		})
		return
	}

	if t.kind == dataTargetEntity {
		if _, ok := t.entity.(DataTarget); !ok || t.writeEntity == nil {
			return
		}
	}
	data := t.getData()
	if data == nil {
		return
	}
	if _, refusal := path.set(data, tag); refusal != nil {
		return
	}
	if t.kind == dataTargetEntity {
		t.writeEntity(data)
		return
	}
	_ = t.setData(data)
}

// scaledTag casts the store's value to its type word's tag as Java casts a
// double: truncated, saturating at int or long bounds, byte and short
// wrapping the int.
// vanilla: ExecuteCommand.wrapStores
func scaledTag(typeWord string, v float64) nbt.Tag {
	switch typeWord {
	case "byte":
		return nbt.Byte(int8(javaDoubleToInt(v)))
	case "short":
		return nbt.Short(int16(javaDoubleToInt(v)))
	case "long":
		return nbt.Long(world.JavaLong(v))
	case "float":
		return nbt.Float(float32(v))
	case "double":
		return nbt.Double(v)
	}
	return nbt.Int(javaDoubleToInt(v))
}

// javaDoubleToInt is Java's (int) cast of a double: NaN is 0, the rest
// truncated toward zero and clamped to int's range.
func javaDoubleToInt(v float64) int32 {
	switch {
	case math.IsNaN(v):
		return 0
	case v >= math.MaxInt32:
		return math.MaxInt32
	case v <= math.MinInt32:
		return math.MinInt32
	}
	return int32(v)
}

// storedValue is what an execute store writes: the command's result, or
// 1/0 for its success.
func storedValue(storeResult, success bool, result int32) int32 {
	if storeResult {
		return result
	}
	if success {
		return 1
	}
	return 0
}

// sameBlockEntity reports whether two cells' block entities hold the same
// data, positions aside; no block entity matches only none.
// vanilla: ExecuteCommand.checkRegions
func sameBlockEntity(view Source, x, y, z, dx, dy, dz int32) bool {
	src, has := view.BlockEntityTagAt(x, y, z)
	if !has {
		// Vanilla only inspects the destination when the source has an
		// entity, and equal states keep a stray destination entity from
		// mattering.
		return true
	}
	dst, dstHas := view.BlockEntityTagAt(dx, dy, dz)
	if !dstHas {
		return false
	}
	return reflect.DeepEqual(withoutPosition(src.Entries()), withoutPosition(dst.Entries()))
}

func withoutPosition(c nbt.Compound) nbt.Compound {
	out := make(nbt.Compound, len(c))
	for k, v := range c {
		if k != "x" && k != "y" && k != "z" {
			out[k] = v
		}
	}
	return out
}
