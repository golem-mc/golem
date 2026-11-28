package command

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Swing is the /swing command.
var Swing = Command{
	Name:   "swing",
	Level:  PermLevelGamemasters,
	Build:  swingNode,
	Run:    discardResult(runSwing),
	Result: runSwing,
}

// swingFailedKey is the refusal for targets that are none of them living.
const swingFailedKey = "commands.swing.failed.notliving"

// swingAnimationTypes are the names of the swing animation types, indexed by
// the id the packet carries.
// vanilla: SwingAnimationType
var swingAnimationTypes = []string{"none", "whack", "stab"}

// swingNode builds /swing [targets [mainhand|offhand [animation [duration]]]].
// vanilla: SwingCommand.register
func swingNode() *Node {
	hand := func(name string) *Node {
		duration := &Node{
			Type: NodeArgument, Name: "duration", Parser: ParserTime, Executable: true,
			Check:      checkTimeMin(1),
			Properties: func(b *protocol.Buffer) { b.PutI32(1) },
		}
		animation := &Node{
			Type: NodeArgument, Name: "animation", Parser: ParserSwingAnimation, Executable: true,
			Check: checkSwingAnimationArg, Children: []*Node{duration},
		}
		return &Node{Type: NodeLiteral, Name: name, Executable: true, Children: []*Node{animation}}
	}
	return &Node{
		Type:       NodeLiteral,
		Name:       "swing",
		Executable: true,
		Required:   PermLevelGamemasters,
		Children: []*Node{
			{
				Type:       NodeArgument,
				Name:       "targets",
				Executable: true,
				Parser:     ParserEntity,
				Properties: func(b *protocol.Buffer) { b.PutU8(0x00) }, // entities, several
				EntityArg:  entityTargets(),
				Suggests:   AskServerSuggestion,
				Completions: func(ctx any, partial string) (int, []string) {
					return EntityCompletions(ctx, partial)
				},
				Children: []*Node{hand("mainhand"), hand("offhand")},
			},
		},
	}
}

// checkSwingAnimationArg refuses a word that names no swing animation type.
// vanilla: SwingAnimationArgument.parse
func checkSwingAnimationArg(word string) *ParseError {
	if slices.Contains(swingAnimationTypes, word) {
		return nil
	}
	return &ParseError{Key: "argument.swing_animation.invalid", With: []string{word}, Cursor: len(word)}
}

// runSwing runs /swing [targets] [mainhand|offhand] [animation] [duration]:
// every living target swings the hand and the count of those that did is the
// feedback.
// vanilla: SwingCommand.swing
func runSwing(src Source, input string, args []string) (int32, error) {
	word, rest := splitTargets(input, args)
	targets, err := resolveEntitiesOrSelf(src, word)
	if err != nil {
		return 0, err
	}
	offHand := len(rest) > 0 && rest[0] == "offhand"
	anim := registry.DefaultSwingAnimation
	if len(rest) > 1 {
		anim.Type = int32(slices.Index(swingAnimationTypes, rest[1]))
	}
	if len(rest) > 2 {
		// The walk has held the duration to at least one tick.
		ticks, _ := ParseTimeArg(rest[2])
		anim.Duration = int32(ticks)
	}

	var swung []Entity
	for _, target := range targets {
		if target.Swing(offHand, anim) {
			swung = append(swung, target)
		}
	}
	if len(swung) == 0 {
		return 0, refuse(src, swingFailedKey)
	}

	if !wantsFeedback(src) {
		return int32(len(swung)), nil
	}
	key := "commands.swing.success.single"
	with := []protocol.Text{formatDisplayNames(swung)}
	if len(swung) > 1 {
		key = "commands.swing.success.multiple"
		with = []protocol.Text{entityCount(len(swung))}
	}
	return resultOf(int32(len(swung)), feedbackToAdmins(src, key, with...))
}
