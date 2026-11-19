package command

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// Kick is the /kick command.
var Kick = Command{
	Name:   "kick",
	Level:  PermLevelAdmins,
	Build:  kickNode,
	Run:    discardResult(runKick),
	Result: runKick,
}

// kickNode builds the /kick command tree: player targets, then an optional
// reason.
// vanilla: KickCommand
func kickNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "kick",
		Required: PermLevelAdmins,
		Children: []*Node{
			{
				Type:       NodeArgument,
				Name:       "targets",
				Executable: true,
				Parser:     ParserEntity,
				Properties: func(b *protocol.Buffer) { b.PutU8(0x02) }, // players, several
				EntityArg:  &EntityArg{PlayersOnly: true},
				Completions: func(ctx any, partial string) (int, []string) {
					return EntityCompletions(ctx, partial)
				},
				Children: []*Node{
					{
						Type:       NodeArgument,
						Name:       "reason",
						Executable: true,
						Parser:     ParserMessage,
						Greedy:     true,
					},
				},
			},
		},
	}
}

// runKick runs /kick <targets> [reason]. Each target is disconnected with the
// reason — the default "Kicked by an operator" where none was given — and the
// feedback names it.
// vanilla: KickCommand.kickPlayers
func runKick(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete kick command")
		return 0, nil
	}
	word, _ := splitTargets(input, args)
	targets, err := resolvePlayers(src, word)
	if err != nil {
		return 0, err
	}
	reason := protocol.Text{Translate: "multiplayer.disconnect.kicked"}
	if text, ok := targetMessage(input, args); ok {
		reason = resolveChatMessage(src, text)
	}
	for _, target := range targets {
		// Feedback first: the target's departure notice could otherwise
		// reach the runner ahead of the message naming it.
		if err := feedbackToAdmins(src, "commands.kick.success", target.DisplayName(), reason); err != nil {
			return 0, err
		}
		target.Disconnect(reason)
	}
	return int32(len(targets)), nil
}
