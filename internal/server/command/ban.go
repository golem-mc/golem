package command

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// Ban is the /ban command.
var Ban = Command{
	Name:   "ban",
	Level:  PermLevelAdmins,
	Build:  banNode,
	Run:    discardResult(runBan),
	Result: runBan,
}

// banNode builds the /ban command tree: player profiles, then an optional
// reason.
// vanilla: BanPlayerCommands
func banNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "ban",
		Required: PermLevelAdmins,
		Children: []*Node{
			{
				Type:       NodeArgument,
				Name:       "targets",
				Executable: true,
				Parser:     ParserGameProfile,
				EntityArg:  profileTargets(),
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

// runBan runs /ban <targets> [reason], banning and disconnecting each player
// ("Banned by an operator." by default). Already-banned players are skipped;
// it fails only when nobody is left.
// vanilla: BanPlayerCommands.banPlayers
func runBan(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete ban command")
		return 0, nil
	}
	word, _ := splitTargets(input, args)
	profiles, err := resolveProfiles(src, word)
	if err != nil {
		return 0, err
	}
	reason, hasReason := targetMessage(input, args)
	if hasReason {
		reason = resolveChatMessage(src, reason).Plain()
	}
	banned := 0
	for _, profile := range profiles {
		target, added := src.BanPlayer(profile, reason)
		if !added {
			continue
		}
		banned++
		shown := banReasonText(reason, hasReason)
		if err := feedbackToAdmins(src, "commands.ban.success", protocol.Text{Text: profile.Name}, shown); err != nil {
			return 0, err
		}
		if target != nil {
			target.Disconnect(protocol.Text{Translate: "multiplayer.disconnect.banned"})
		}
	}
	if banned == 0 {
		return 0, refuse(src, "commands.ban.failed")
	}
	return int32(banned), nil
}

// banReasonText is the reason component a ban feedback and a ban list entry
// carry: the text given, or vanilla's "Banned by an operator." default.
func banReasonText(reason string, hasReason bool) protocol.Text {
	if !hasReason {
		return protocol.Text{Translate: "multiplayer.disconnect.banned.reason.default"}
	}
	return protocol.Text{Text: reason}
}
