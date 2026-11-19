package command

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// Kill is the /kill command.
var Kill = Command{
	Name:   "kill",
	Level:  PermLevelGamemasters,
	Build:  killNode,
	Run:    discardResult(runKill),
	Result: runKill,
}

// killNode builds the /kill command tree, matching the vanilla
// report: the command runs bare, killing the body the runner's @s names,
// and takes a set of entities to kill instead.
func killNode() *Node {
	return &Node{
		Type:       NodeLiteral,
		Name:       "kill",
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
			},
		},
	}
}

// runKill runs /kill [targets] (default the runner's @s): living bodies die
// to generic-kill damage at its largest, anything else (items, arrows,
// projectiles, orbs) is discarded, and the death message is "killed by
// magic". It answers how many it killed.
// vanilla: KillCommand.kill
func runKill(src Source, input string, args []string) (int32, error) {
	word, _ := splitTargets(input, args)
	targets, err := resolveEntitiesOrSelf(src, word)
	if err != nil {
		return 0, err
	}

	for _, target := range targets {
		target.Kill()
	}

	key := "commands.kill.success.single"
	with := []protocol.Text{formatDisplayNames(targets)}
	if len(targets) > 1 {
		// The vanilla feedback for several counts them rather than naming
		// them.
		key = "commands.kill.success.multiple"
		with = []protocol.Text{entityCount(len(targets))}
	}
	return resultOf(int32(len(targets)), feedbackToAdmins(src, key, with...))
}
