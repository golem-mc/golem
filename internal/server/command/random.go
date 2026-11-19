package command

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// Random is the /random command.
var Random = Command{
	Name:   "random",
	Build:  randomNode,
	Run:    discardResult(runRandom),
	Result: runRandom,
}

// The keys the /random command answers with.
// vanilla: RandomCommand
const (
	randomSampleKey     = "commands.random.sample.success"
	randomRollKey       = "commands.random.roll"
	randomResetKey      = "commands.random.reset.success"
	randomResetAllKey   = "commands.random.reset.all.success"
	randomRangeTooSmall = "commands.random.error.range_too_small"
	randomRangeTooLarge = "commands.random.error.range_too_large"
)

// randomNode builds /random as the vanilla report has it: value and roll draw a
// range for anyone, with an operator-only sequence name; reset (operators)
// restarts one or all sequences with a salt and the two derivation flags.
// vanilla: RandomCommand.register
func randomNode() *Node {
	// A sequence's tab completion offers the streams that exist, vanilla's
	// own suggestion. vanilla: RandomCommand.suggestRandomSequence
	sequence := func(children ...*Node) *Node {
		node := &Node{
			Type:       NodeArgument,
			Name:       "sequence",
			Executable: true,
			Parser:     ParserResourceLoc,
			Required:   PermLevelGamemasters,
			Suggests:   AskServerSuggestion,
			Completions: func(ctx any, partial string) (int, []string) {
				src, ok := ctx.(Source)
				if !ok {
					return 0, nil
				}
				return 0, Suggestions(partial, src.RandomSequenceIDs()...)
			},
		}
		node.Children = children
		return node
	}
	draw := func(name string) *Node {
		return &Node{Type: NodeLiteral, Name: name, Children: []*Node{{
			Type:       NodeArgument,
			Name:       "range",
			Executable: true,
			Parser:     ParserIntRange,
			Check:      checkIntRangeArg,
			Children:   []*Node{sequence()},
		}}}
	}
	// The salt the reset forms take, and behind it the two flags its
	// optional words spell. vanilla: RandomCommand.register
	seed := func() *Node {
		return &Node{
			Type:       NodeArgument,
			Name:       "seed",
			Executable: true,
			Parser:     ParserInteger,
			Children: []*Node{{
				Type:       NodeArgument,
				Name:       "includeWorldSeed",
				Executable: true,
				Parser:     ParserBool,
				Children: []*Node{{
					Type:       NodeArgument,
					Name:       "includeSequenceId",
					Executable: true,
					Parser:     ParserBool,
				}},
			}},
		}
	}
	resetSequence := sequence(seed())
	return &Node{
		Type: NodeLiteral,
		Name: "random",
		Children: []*Node{
			draw("value"),
			draw("roll"),
			{Type: NodeLiteral, Name: "reset", Required: PermLevelGamemasters, Children: []*Node{
				{Type: NodeLiteral, Name: "*", Executable: true, Children: []*Node{seed()}},
				resetSequence,
			}},
		},
	}
}

// checkIntRangeArg is the reading the range argument's word goes through
// while the walk reads it, MinMaxBounds.fromReader: the word has to name a
// number at one end at least, the minimum may not stand above the maximum,
// and nothing of the word may outlive the reading.
// vanilla: MinMaxBounds.fromReader
func checkIntRangeArg(word string) *ParseError {
	r := selReader{input: word}
	if _, err := r.parseRange(parseIntEnd, keyIntInvalid, false); err != nil {
		return err
	}
	if r.canRead() {
		return &ParseError{Key: keyExpectedSeparator, Cursor: r.pos}
	}
	return nil
}

// readIntRange reads the range a draw names as its two ends, which the
// walk's own check has already refused a malformed word before.
// vanilla: RangeArgument$Ints.getRange
func readIntRange(word string) *bounds {
	r := selReader{input: word}
	b, _ := r.parseRange(parseIntEnd, keyIntInvalid, false)
	return b
}

// runRandom runs /random value|roll|reset: the draws a range names, the
// stream a sequence names, and the resets that restart the streams.
// vanilla: RandomCommand
func runRandom(src Source, input string, args []string) (int32, error) {
	switch args[0] {
	case "value", "roll":
		// The stream is fetched before the range is read, the order
		// vanilla's own runs in: a refused range still leaves the sequence
		// behind for the completions and the counts.
		var stream RandomSequence
		if len(args) > 2 {
			name, _, ok := readIdentifier(args[2])
			if !ok {
				src.Debug("unreadable sequence", "word", args[2])
				return 0, nil
			}
			stream = src.RandomSequence(name)
		}
		bounds := readIntRange(args[1])
		if bounds == nil {
			src.Debug("unreadable range", "word", args[1])
			return 0, nil
		}
		min := int32(math.MinInt32)
		if bounds.HasMin {
			min = int32(bounds.Min)
		}
		max := int32(math.MaxInt32)
		if bounds.HasMax {
			max = int32(bounds.Max)
		}
		// The span — the largest value less the smallest — has to hold at
		// least two values and stay under the int cap.
		// vanilla: RandomCommand.randomSample
		span := int64(max) - int64(min)
		if span == 0 {
			return 0, refuse(src, randomRangeTooSmall)
		}
		if span >= math.MaxInt32 {
			return 0, refuse(src, randomRangeTooLarge)
		}
		var value int32
		if stream != nil {
			value = stream.IntBetween(min, max)
		} else {
			// The runner's world's own dice, the level random the command
			// rolls on where no sequence is named.
			// vanilla: RandomCommand.randomSample
			value = src.RollDice(world.Dice{Min: min, Max: max})
		}
		v := int64(value)
		if args[0] == "roll" {
			// A roll is told to the whole world rather than answered
			// with. vanilla: PlayerList.broadcastSystemMessage
			lo, hi := int64(min), int64(max)
			src.BroadcastSystemMessage(protocol.Text{
				Translate: randomRollKey,
				With: []protocol.Text{
					src.DisplayName(),
					{Number: &v},
					{Number: &lo},
					{Number: &hi},
				},
			})
			return value, nil
		}
		return resultOf(value, src.Feedback(randomSampleKey, protocol.Text{Number: &v}))
	case "reset":
		return runRandomReset(src, args)
	}
	src.Debug("unknown random form", "form", args[0])
	return 0, nil
}

// runRandomReset runs /random reset (<sequence>|*) [<seed>
// [<includeWorldSeed> [<includeSequenceId>]]: either one named stream or
// all of them restart, and where the seed was named it and the flags behind
// it decide what the streams created afterwards derive from.
// vanilla: RandomCommand.resetSequence, RandomCommand.resetAllSequences[AndSetNewDefaults]
func runRandomReset(src Source, args []string) (int32, error) {
	var salt int32
	hasSalt := false
	includeWorldSeed, includeSequenceID := true, true
	if len(args) > 2 {
		n, ok := parseInteger(args[2])
		if !ok {
			src.Debug("unreadable salt", "word", args[2])
			return 0, nil
		}
		salt = int32(n)
		hasSalt = true
		if len(args) > 3 {
			if b, ok := boolArgValue(args[3:4]); ok {
				includeWorldSeed = b == "true"
			}
			if len(args) > 4 {
				if b, ok := boolArgValue(args[4:5]); ok {
					includeSequenceID = b == "true"
				}
			}
		}
	}
	if args[1] == "*" {
		count := src.ResetAllRandomSequences(salt, hasSalt, includeWorldSeed, includeSequenceID)
		c := int64(count)
		return resultOf(int32(count), src.Feedback(randomResetAllKey, protocol.Text{Number: &c}))
	}
	name, _, ok := readIdentifier(args[1])
	if !ok {
		src.Debug("unreadable sequence", "word", args[1])
		return 0, nil
	}
	src.ResetRandomSequence(name, salt, hasSalt, includeWorldSeed, includeSequenceID)
	return resultOf(1, src.Feedback(randomResetKey, protocol.Text{Text: name}))
}
