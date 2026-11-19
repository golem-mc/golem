package command

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
)

// TeamMsg is the /teammsg command, open to every player; Tm is its alias.
// vanilla: TeamMsgCommand
var (
	TeamMsg = Command{
		Name:   "teammsg",
		Level:  0,
		Build:  teamMsgNode,
		Run:    discardResult(runTeamMsg),
		Result: runTeamMsg,
	}
	Tm = Command{
		Name:  "tm",
		Level: 0,
		Build: func() *Node { return &Node{Type: NodeLiteral, Name: "tm", RedirectName: "teammsg"} },
	}
)

func teamMsgNode() *Node {
	return &Node{Type: NodeLiteral, Name: "teammsg", Children: []*Node{{
		Type: NodeArgument, Name: "message", Parser: ParserMessage, Greedy: true, Executable: true}}}
}

// runTeamMsg runs /teammsg <message>: the runner, when a player, reads the
// outgoing form and every other online player on its team the incoming one.
// It answers how many players it went to.
// vanilla: TeamMsgCommand.register, TeamMsgCommand.sendMessage
func runTeamMsg(src Source, input string, _ []string) (int32, error) {
	// The message is everything after the one space behind the command
	// word, which is teammsg or tm.
	_, message, _ := strings.Cut(input, " ")
	if message == "" {
		src.Debug("incomplete teammsg command")
		return 0, nil
	}
	self := src.SelfEntity()
	if self == nil {
		return 0, refuse(src, "permissions.requires.entity")
	}
	board := src.Scoreboard()
	name, ok := board.PlayerTeam(entityHolder(self).Name)
	if !ok {
		return 0, refuse(src, "commands.teammsg.failed.noteam")
	}
	var receivers []Target
	for _, p := range src.OnlineTargets() {
		if p.UUID() == self.UUID() {
			receivers = append(receivers, p)
		} else if t, _ := board.PlayerTeam(p.Name()); t == name {
			receivers = append(receivers, p)
		}
	}
	if len(receivers) == 0 {
		return 0, nil
	}
	team, _ := board.Team(name)
	// Clicking the name suggests /teammsg. vanilla: TeamMsgCommand.SUGGEST_STYLE
	to := teamName(team)
	to.ClickCommand = "/teammsg "
	to.HoverText = &protocol.Text{Translate: "chat.type.team.hover"}
	resolved := resolveChatMessage(src, message)
	from := src.DisplayName()
	argument := src.ChatArgument("message")
	for _, p := range receivers {
		p.TeamMessage(p.UUID() == self.UUID(), from, argument, to, resolved)
	}
	return int32(len(receivers)), nil
}
