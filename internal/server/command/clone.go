package command

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Clone is the /clone command.
var Clone = Command{
	Name:   "clone",
	Level:  PermLevelGamemasters,
	Build:  cloneNode,
	Run:    discardResult(runClone),
	Result: runClone,
}

// The translation keys the /clone command answers with: what it copied, the
// three ways it refuses before anything is written — a destination that
// overlaps its source, a region over the cap, a region whose chunks are not
// loaded — and the refusal a clone that copied nothing answers with.
const (
	cloneSuccessKey = "commands.clone.success"
	cloneFailedKey  = "commands.clone.failed"
	cloneOverlapKey = "commands.clone.overlap"
	cloneToobigKey  = "commands.clone.toobig"
)

// cloneFilterMode is how a clone picks the cells it copies: all of them, the
// ones that are not air, or only the ones a block predicate picks.
type cloneFilterMode int

const (
	cloneReplace cloneFilterMode = iota
	cloneMasked
	cloneFiltered
)

// cloneNode builds /clone as the vanilla report has it: source corners and the
// destination, optionally after from <dimension> and with to <dimension>,
// then replace, masked or filtered <filter> (each also with strict, which
// wakes nothing) and force, move or normal.
func cloneNode() *Node {
	fromArg := dimensionArg("sourceDimension")
	fromArg.Children = []*Node{cloneBegin()}
	return &Node{
		Type:     NodeLiteral,
		Name:     "clone",
		Required: PermLevelGamemasters,
		Children: []*Node{
			cloneBegin(),
			{Type: NodeLiteral, Name: "from", Children: []*Node{fromArg}},
		},
	}
}

// cloneBegin is the begin corner, from which the end corner takes over.
func cloneBegin() *Node {
	return &Node{
		Type:     NodeArgument,
		Name:     "begin",
		Parser:   ParserBlockPos,
		Children: []*Node{cloneEnd()},
	}
}

// cloneEnd is the end corner, from which the destination — beside it or
// behind a to <dimension> — takes over.
func cloneEnd() *Node {
	endArg := &Node{Type: NodeArgument, Name: "end", Parser: ParserBlockPos}
	toArg := dimensionArg("targetDimension")
	toArg.Children = []*Node{cloneDestination()}
	endArg.Children = []*Node{
		cloneDestination(),
		{Type: NodeLiteral, Name: "to", Children: []*Node{toArg}},
	}
	return endArg
}

// cloneDestination is the destination corner carrying the mode suffix: the
// plain command runs replace and normal, each filtration word names the
// clone modes under it, and the strict literal carries the same suffix
// again over a copy that wakes nothing.
func cloneDestination() *Node {
	return cloneModeWords(&Node{
		Type:       NodeArgument,
		Name:       "destination",
		Executable: true,
		Parser:     ParserBlockPos,
	})
}

// cloneFilterModeArgs is the filtration the handler settled on: which of
// the three modes it is, and the predicate for the filtered one.
type cloneFilterModeArgs struct {
	mode cloneFilterMode
	pred blockPredicate
}

// runClone runs /clone [from <dimension>] <begin> <end> [to <dimension>]
// <destination> [strict] [replace|masked|filtered <filter>]
// [force|move|normal]. Positions resolve against the runner and are checked
// in their own dimensions. The feedback counts every copied cell, unchanged
// ones included, as vanilla's barrier write does.
// vanilla: CloneCommands.clone
func runClone(src Source, input string, args []string) (int32, error) {
	i := 0
	fromSrc := src
	if len(args) > 1 && args[0] == "from" {
		if !dimensionKnown(src, args[1]) {
			return 0, refuse(src, dimensionInvalidKey, protocol.Text{Text: registry.Qualify(args[1])})
		}
		var ok bool
		fromSrc, ok = src.InContext(Context{Dim: args[1], HasDim: true})
		if !ok {
			src.Debug("clone from-dimension is not one the server runs", "dimension", args[1])
			return 0, nil
		}
		i = 2
	}
	if len(args) < i+9 {
		src.Debug("incomplete clone command")
		return 0, nil
	}

	// The corners pass the two checks the vanilla block position arguments
	// run, each in its own dimension, before anything is counted.
	bx, by, bz, ok := resolveBlockPos(src, args[i:i+3])
	if !ok {
		src.Debug("clone begin did not parse", "args", args[i:i+3])
		return 0, nil
	}
	if key := loadedBlockPosRefusal(fromSrc, bx, by, bz); key != "" {
		return 0, refuse(src, key)
	}
	ex, ey, ez, ok := resolveBlockPos(src, args[i+3:i+6])
	if !ok {
		src.Debug("clone end did not parse", "args", args[i+3:i+6])
		return 0, nil
	}
	if key := loadedBlockPosRefusal(fromSrc, ex, ey, ez); key != "" {
		return 0, refuse(src, key)
	}
	i += 6

	toSrc := src
	if args[i] == "to" {
		if !dimensionKnown(src, args[i+1]) {
			return 0, refuse(src, dimensionInvalidKey, protocol.Text{Text: registry.Qualify(args[i+1])})
		}
		var ok bool
		toSrc, ok = src.InContext(Context{Dim: args[i+1], HasDim: true})
		if !ok {
			src.Debug("clone to-dimension is not one the server runs", "dimension", args[i+1])
			return 0, nil
		}
		i += 2
	}
	if len(args) < i+3 {
		src.Debug("incomplete clone command")
		return 0, nil
	}
	dx0, dy0, dz0, ok := resolveBlockPos(src, args[i:i+3])
	if !ok {
		src.Debug("clone destination did not parse", "args", args[i:i+3])
		return 0, nil
	}
	// The destination corner passes the loaded check in the dimension it
	// lands in, which the whole destination box is held to below.
	if key := loadedBlockPosRefusal(toSrc, dx0, dy0, dz0); key != "" {
		return 0, refuse(src, key)
	}
	i += 3

	// The mode words. The tree has already ordered them: the strict mode
	// stands before the filtration it rides, and the clone modes after.
	strict := false
	if i < len(args) && args[i] == "strict" {
		strict = true
		i++
	}
	filter := cloneFilterModeArgs{mode: cloneReplace}
	if i < len(args) {
		switch args[i] {
		case "replace":
			i++
		case "masked":
			filter.mode = cloneMasked
			i++
		case "filtered":
			filter.mode = cloneFiltered
			if i+1 >= len(args) {
				src.Debug("incomplete clone command")
				return 0, nil
			}
			span := blockSpanCount(args[i+1:])
			word := wordsIn(input, args, i+1, span)
			pred, parsed := parseBlockPredicate(word)
			if !parsed {
				src.Debug("clone filter did not parse", "filter", word)
				return 0, nil
			}
			filter.pred = pred
			i += 1 + span
		default:
			src.Debug("clone filtration is not one the tree offers", "word", args[i])
			return 0, nil
		}
	}
	cloneMode := "normal"
	if i < len(args) {
		if args[i] != "force" && args[i] != "move" && args[i] != "normal" {
			src.Debug("clone mode is not one the tree offers", "mode", args[i])
			return 0, nil
		}
		cloneMode = args[i]
		i++
	}
	if i != len(args) {
		src.Debug("clone words left over", "args", args[i:])
		return 0, nil
	}

	// The region is the box the corners span, whichever order they name
	// them in, and the destination box spans its own size from the corner.
	loX, hiX := min(bx, ex), max(bx, ex)
	loY, hiY := min(by, ey), max(by, ey)
	loZ, hiZ := min(bz, ez), max(bz, ez)
	dx1, dy1, dz1 := dx0+hiX-loX, dy0+hiY-loY, dz0+hiZ-loZ

	// Only the normal mode refuses a destination that overlaps its source,
	// and only where the two sit in the same dimension.
	if cloneMode == "normal" && fromSrc.DimensionName() == toSrc.DimensionName() &&
		cloneBoxesOverlap(loX, hiX, loY, hiY, loZ, hiZ, dx0, dx1, dy0, dy1, dz0, dz1) {
		return 0, refuse(src, cloneOverlapKey)
	}
	limit := int64(src.IntRule(maxFillRule))
	area := int64(hiX-loX+1) * int64(hiY-loY+1) * int64(hiZ-loZ+1)
	if area > limit {
		return 0, refuse(src, cloneToobigKey,
			protocol.Text{Number: &limit},
			protocol.Text{Long: &area})
	}
	// The whole of both boxes must sit in loaded chunks, the check the
	// vanilla command runs once its corners have passed.
	if !fromSrc.BlockBoxLoaded(loX, loZ, hiX, hiZ) || !toSrc.BlockBoxLoaded(dx0, dz0, dx1, dz1) {
		return 0, refuse(src, keyPosUnloaded)
	}

	// The cells: every one the filter picks, carrying the state its source
	// holds and the tag its block entity keeps for a block that has one.
	offX, offY, offZ := dx0-loX, dy0-loY, dz0-loZ
	var changes []world.BlockChange
	var tags []nbt.Compound
	var clear []world.BlockChange
	for x := loX; x <= hiX; x++ {
		for z := loZ; z <= hiZ; z++ {
			for y := loY; y <= hiY; y++ {
				state := fromSrc.BlockAt(x, y, z)
				if !cloneCellPicked(fromSrc, x, y, z, state, filter) {
					continue
				}
				var tag nbt.Compound
				if name, _, known := registry.BlockStateName(state); known {
					if _, keeps := registry.BlockEntityTypes[name]; keeps {
						if read, has := fromSrc.BlockEntityTagAt(x, y, z); has {
							tag = read.Entries()
						}
					}
				}
				changes = append(changes, world.BlockChange{
					X: x + offX, Y: y + offY, Z: z + offZ, State: state,
				})
				tags = append(tags, tag)
				if cloneMode == "move" {
					clear = append(clear, world.BlockChange{X: x, Y: y, Z: z, State: airState})
				}
			}
		}
	}

	// A move clears its source before the copy lands; the tags and the
	// ticks' blocks were read before the clear, so a container moves with
	// what it holds and a tick with its block.
	ticks := fromSrc.BlockTicksIn(loX, loY, loZ, hiX, hiY, hiZ)
	if cloneMode == "move" {
		fromSrc.FillCells(clear, nil, nil, nil, strict, false, false)
	}
	count := toSrc.CloneCells(changes, tags, strict)
	if count == 0 {
		return 0, refuse(src, cloneFailedKey)
	}
	// vanilla: LevelTicks.copyAreaFrom
	toSrc.ScheduleBlockTicks(ticks, offX, offY, offZ)
	src.Info("region cloned", "count", count,
		"from", []int32{bx, by, bz}, "to", []int32{dx0, dy0, dz0}, "by", src.Name())
	copied := int64(count)
	return resultOf(int32(count), feedbackToAdmins(src, cloneSuccessKey, protocol.Text{Number: &copied}))
}

// cloneBoxesOverlap reports whether the source box and the destination box
// share a cell, passing each as its low and high corners.
func cloneBoxesOverlap(loX, hiX, loY, hiY, loZ, hiZ int32, dLoX, dHiX, dLoY, dHiY, dLoZ, dHiZ int32) bool {
	return loX <= dHiX && dLoX <= hiX && loY <= dHiY && dLoY <= hiY && loZ <= dHiZ && dLoZ <= hiZ
}

// cloneCellPicked reports whether the filter picks the cell's state: the
// replace mode picks everything, the masked mode skips air, and a predicate
// picks what blockPredicate.matches picks in the source cell at the position.
func cloneCellPicked(from Source, x, y, z int32, state uint16, filter cloneFilterModeArgs) bool {
	if int(state) >= registry.StateCount {
		return false
	}
	switch filter.mode {
	case cloneMasked:
		return !world.IsKnownAirState(state)
	case cloneFiltered:
		return filter.pred.matches(from, x, y, z, state)
	}
	return true
}

// cloneModeWords hangs the filtration words off dest: replace and masked
// stand on their own, filtered takes the predicate argument, and the strict
// literal carries the same three over again for a copy that wakes nothing.
// Each filtration also names the force, move and normal clone modes under
// it, and executes alone, which runs the normal clone mode.
func cloneModeWords(dest *Node) *Node {
	dest.Children = []*Node{
		cloneFiltration("replace", nil),
		cloneFiltration("masked", nil),
		cloneFiltration("filtered", cloneFilterArg()),
		{
			Type:       NodeLiteral,
			Name:       "strict",
			Executable: true,
			Children: []*Node{
				cloneFiltration("replace", nil),
				cloneFiltration("masked", nil),
				cloneFiltration("filtered", cloneFilterArg()),
			},
		},
	}
	return dest
}

// cloneFiltration is one filtration word: executable on its own, and
// naming the three clone modes under it. The filtered one reads its
// predicate first, which executes the same way.
func cloneFiltration(name string, filter *Node) *Node {
	word := &Node{Type: NodeLiteral, Name: name}
	if filter == nil {
		word.Executable = true
		word.Children = cloneModeLits()
		return word
	}
	word.Children = []*Node{filter}
	return word
}

// cloneFilterArg is the block predicate the filtered mode reads: a block,
// a block with property tests, or a # tag in front of either - the words
// the /fill filter suggests too.
func cloneFilterArg() *Node {
	return &Node{
		Type:       NodeArgument,
		Name:       "filter",
		Executable: true,
		Parser:     ParserBlockPredicate,
		Suggests:   AskServerSuggestion,
		Children:   cloneModeLits(),
		Completions: func(ctx any, partial string) (int, []string) {
			return blockPredicateCompletions(partial)
		},
		IntegerMatches: blockPredicateIntegers,
	}
}

// cloneModeLits are the three clone mode words every filtration names, each
// executable on its own.
func cloneModeLits() []*Node {
	lits := make([]*Node, 0, 3)
	for _, mode := range []string{"force", "move", "normal"} {
		lits = append(lits, &Node{Type: NodeLiteral, Name: mode, Executable: true})
	}
	return lits
}
