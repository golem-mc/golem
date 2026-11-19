package command

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// Pardon is the /pardon command.
var Pardon = Command{
	Name:   "pardon",
	Level:  PermLevelAdmins,
	Build:  pardonNode,
	Run:    discardResult(runPardon),
	Result: runPardon,
}

// pardonNode builds the /pardon command tree: player profiles, suggested from
// the banned players.
// vanilla: PardonCommand
func pardonNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "pardon",
		Required: PermLevelAdmins,
		Children: []*Node{
			{
				Type:       NodeArgument,
				Name:       "targets",
				Executable: true,
				Parser:     ParserGameProfile,
				EntityArg:  profileTargets(),
				Suggests:   AskServerSuggestion,
				Completions: func(ctx any, partial string) (int, []string) {
					src, ok := ctx.(Source)
					if !ok {
						return 0, nil
					}
					var names []string
					for _, profile := range src.BannedPlayers() {
						names = append(names, profile.Name)
					}
					return 0, Suggestions(partial, names...)
				},
			},
		},
	}
}

// runPardon runs /pardon <targets>, lifting each player's ban. A target that
// is not banned is skipped, and the command fails only when that leaves
// nobody to pardon.
// vanilla: PardonCommand.pardonPlayers
func runPardon(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete pardon command")
		return 0, nil
	}
	word, _ := splitTargets(input, args)
	profiles, err := resolveProfiles(src, word)
	if err != nil {
		return 0, err
	}
	pardoned := 0
	for _, profile := range profiles {
		if !src.PardonPlayer(profile) {
			continue
		}
		pardoned++
		if err := feedbackToAdmins(src, "commands.pardon.success", protocol.Text{Text: profile.Name}); err != nil {
			return 0, err
		}
	}
	if pardoned == 0 {
		return 0, refuse(src, "commands.pardon.failed")
	}
	return int32(pardoned), nil
}
