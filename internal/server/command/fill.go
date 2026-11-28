package command

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Fill is the /fill command.
var Fill = Command{
	Name:   "fill",
	Level:  PermLevelGamemasters,
	Build:  fillNode,
	Run:    discardResult(runFill),
	Result: runFill,
}

// The translation keys the /fill command answers with: what it changed, what
// it could not, and the volume it refuses to fill. The bare keys the block
// position arguments fail with are shared with the other region commands and
// live with the coordinate readers.
const (
	fillSuccessKey = "commands.fill.success"
	fillFailedKey  = "commands.fill.failed"
	fillToobigKey  = "commands.fill.toobig"
)

// The volume a /fill may cover is the world's max_block_modifications game
// rule, which the vanilla server's FillCommand reads off the rules each run.
const maxFillRule = world.RuleMaxBlockMods

// how a fill treats the cells it covers. The zero is the default replace.
const (
	fillReplace fillMode = iota
	fillOutline
	fillHollow
	fillDestroy
	fillStrict
	fillKeep
)

type fillMode int

// fillNode builds the /fill command tree, matching the vanilla report:
// the two corners, the block to put there, the modes naming how the fill
// treats what is already in the region - and replace, the default, alone
// taking the filter whose predicate picks the cells it touches.
func fillNode() *Node {
	fromArg := &Node{
		Type:   NodeArgument,
		Name:   "from",
		Parser: ParserBlockPos,
	}
	toArg := &Node{
		Type:   NodeArgument,
		Name:   "to",
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
	filterArg := &Node{
		Type:       NodeArgument,
		Name:       "filter",
		Executable: true,
		Parser:     ParserBlockPredicate,
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return blockPredicateCompletions(partial)
		},
		IntegerMatches: blockPredicateIntegers,
	}
	// The sub-modes the filter's replace takes; the plain modes are the
	// same four without one.
	for _, mode := range []string{"outline", "hollow", "destroy", "strict"} {
		filterArg.Children = append(filterArg.Children, &Node{
			Type:       NodeLiteral,
			Name:       mode,
			Executable: true,
		})
	}
	blockArg.Children = []*Node{
		{Type: NodeLiteral, Name: "outline", Executable: true},
		{Type: NodeLiteral, Name: "hollow", Executable: true},
		{Type: NodeLiteral, Name: "destroy", Executable: true},
		{Type: NodeLiteral, Name: "strict", Executable: true},
		{Type: NodeLiteral, Name: "replace", Executable: true, Children: []*Node{filterArg}},
		{Type: NodeLiteral, Name: "keep", Executable: true},
	}
	toArg.Children = []*Node{blockArg}
	fromArg.Children = []*Node{toArg}
	return &Node{
		Type:     NodeLiteral,
		Name:     "fill",
		Required: PermLevelGamemasters,
		Children: []*Node{fromArg},
	}
}

// runFill runs /fill <from> <to> <block> [mode]. outline walls the region,
// hollow walls it and airs the inside, destroy breaks and drops what stood,
// strict wakes no neighbours, keep skips filled cells; only replace (the
// default) takes a filter, which any mode but replace and keep may follow.
// It answers the cells changed.
// vanilla: FillCommand.fillBlocks
func runFill(src Source, input string, args []string) (int32, error) {
	if len(args) < 7 {
		src.Debug("incomplete fill command")
		return 0, nil
	}
	x0, y0, z0, ok := resolveBlockPos(src, args[:3])
	if !ok {
		src.Debug("fill from position did not parse", "args", args[:3])
		return 0, nil
	}
	// The corners go through the loaded check the vanilla server's block
	// position arguments run, from before to, before anything is counted.
	if key := loadedBlockPosRefusal(src, x0, y0, z0); key != "" {
		return 0, refuse(src, key)
	}
	x1, y1, z1, ok := resolveBlockPos(src, args[3:6])
	if !ok {
		src.Debug("fill to position did not parse", "args", args[3:6])
		return 0, nil
	}
	if key := loadedBlockPosRefusal(src, x1, y1, z1); key != "" {
		return 0, refuse(src, key)
	}

	// The region is the box the corners span, whichever order they name.
	loX, hiX := min(x0, x1), max(x0, x1)
	loY, hiY := min(y0, y1), max(y0, y1)
	loZ, hiZ := min(z0, z1), max(z0, z1)
	area := int64(hiX-loX+1) * int64(hiY-loY+1) * int64(hiZ-loZ+1)
	limit := int64(src.IntRule(maxFillRule))
	if area > limit {
		return 0, refuse(src, fillToobigKey,
			protocol.Text{Number: &limit},
			protocol.Text{Long: &area})
	}

	state, defined, tag, fail := parseBlockStateDefined(args[6])
	if fail != nil {
		src.Debug("fill block did not parse", "block", args[6])
		return 0, blockParseError(input, args[6], fail)
	}
	dropInvalidBlockStates(tag, world.BlockStateNBTKeys, false)

	mode := fillReplace
	if len(args) > 7 {
		if mode, ok = fillModeWord(args[7]); !ok {
			src.Debug("fill mode is not one the tree offers", "mode", args[7])
			return 0, nil
		}
	}

	// Only replace takes a filter, and the filter takes one of the modes
	// other than replace and keep after it.
	var filter blockPredicate
	filtered := len(args) > 8
	if filtered {
		if args[7] != "replace" {
			src.Debug("fill filter outside replace", "args", args[8:])
			return 0, nil
		}
		span := blockSpanCount(args[8:])
		word := wordsIn(input, args, 8, span)
		filter, ok = parseBlockPredicate(word)
		if !ok {
			src.Debug("fill filter did not parse", "filter", word)
			return 0, nil
		}
		if modeAt := 8 + span; len(args) > modeAt {
			if mode, ok = fillModeWord(args[modeAt]); !ok {
				src.Debug("fill filter mode is not one the tree offers", "args", args[modeAt:])
				return 0, nil
			}
		}
	}

	var changes []world.BlockChange
	// x innermost, then y, then z. vanilla: BlockPos.betweenClosed
	for z := loZ; z <= hiZ; z++ {
		for y := loY; y <= hiY; y++ {
			for x := loX; x <= hiX; x++ {
				old := src.BlockAt(x, y, z)
				if filtered && !filter.matches(src, x, y, z, old) {
					continue
				}
				target := state
				switch mode {
				case fillOutline:
					if !fillShell(x, y, z, loX, hiX, loY, hiY, loZ, hiZ) {
						continue
					}
				case fillHollow:
					if !fillShell(x, y, z, loX, hiX, loY, hiY, loZ, hiZ) {
						target = airState
					}
				case fillKeep:
					// vanilla: LevelReader.isEmptyBlock, any of the three airs
					if !world.IsKnownAirState(old) {
						continue
					}
				}
				changes = append(changes, world.BlockChange{X: x, Y: y, Z: z, State: target})
			}
		}
	}
	// Cells are placed one by one, so an earlier cell's shape update can change
	// what a later one holds; the filter and keep test its current state.
	var accept func(x, y, z int32, old uint16) bool
	if filtered || mode == fillKeep {
		accept = func(x, y, z int32, old uint16) bool {
			if filtered && !filter.matches(src, x, y, z, old) {
				return false
			}
			return mode != fillKeep || world.IsKnownAirState(old)
		}
	}
	count := src.FillCells(changes, accept, defined, tag, mode == fillStrict, mode == fillDestroy, true)
	if count == 0 {
		return 0, refuse(src, fillFailedKey)
	}
	src.Debug("region filled", "count", count,
		"from", []int32{x0, y0, z0}, "to", []int32{x1, y1, z1}, "by", src.Name())
	changed := int64(count)
	return resultOf(int32(count), feedbackToAdmins(src, fillSuccessKey, protocol.Text{Number: &changed}))
}

// fillModeWord reads a mode word. The tree offers replace and keep only
// straight after the block, never after a filter.
func fillModeWord(word string) (fillMode, bool) {
	switch word {
	case "outline":
		return fillOutline, true
	case "hollow":
		return fillHollow, true
	case "destroy":
		return fillDestroy, true
	case "strict":
		return fillStrict, true
	case "keep":
		return fillKeep, true
	case "replace":
		return fillReplace, true
	}
	return fillReplace, false
}

// fillShell reports whether a cell stands on the boundary of the region,
// which is what the outline mode fills and what the hollow mode keeps of the
// block it was given.
func fillShell(x, y, z, loX, hiX, loY, hiY, loZ, hiZ int32) bool {
	return x == loX || x == hiX || y == loY || y == hiY || z == loZ || z == hiZ
}

// blockFilterMatches reports whether the cell's old block is one the
// predicate picks: the same block, carrying every property the predicate
// tests; a property the predicate does not name is left to be whatever the
// cell has.
func blockFilterMatches(name string, overrides map[string]string, old uint16) bool {
	oldName, props, ok := registry.BlockStateName(old)
	if !ok || oldName != name {
		return false
	}
	return blockFilterPropsMatch(props, overrides)
}

// blockFilterTagMatches reports whether the cell's old block is in the tag
// and carries every property the predicate tests.
func blockFilterTagMatches(tag string, overrides map[string]string, old uint16) bool {
	name, props, ok := registry.BlockStateName(old)
	if !ok || !registry.BlockInTag(name, tag) {
		return false
	}
	return blockFilterPropsMatch(props, overrides)
}

// blockFilterPropsMatch reports whether a state's encoded properties carry
// every property the predicate tests; a property the state does not have is
// one the predicate's value cannot equal.
func blockFilterPropsMatch(props string, overrides map[string]string) bool {
	if len(overrides) == 0 {
		return true
	}
	values := registry.ParseStateProps(props)
	for key, want := range overrides {
		if values[key] != want {
			return false
		}
	}
	return true
}
