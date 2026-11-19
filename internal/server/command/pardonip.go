package command

import (
	"net"

	"github.com/golem-mc/golem/internal/protocol"
)

// PardonIP is the /pardon-ip command.
var PardonIP = Command{
	Name:   "pardon-ip",
	Level:  PermLevelAdmins,
	Build:  pardonIPNode,
	Run:    discardResult(runPardonIP),
	Result: runPardonIP,
}

// pardonIPNode builds the /pardon-ip command tree: an address, suggested from
// the banned addresses.
// vanilla: PardonIpCommand
func pardonIPNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "pardon-ip",
		Required: PermLevelAdmins,
		Children: []*Node{
			{
				Type:       NodeArgument,
				Name:       "target",
				Executable: true,
				Parser:     ParserString,
				Properties: func(b *protocol.Buffer) { b.PutVarInt(0) }, // single word
				Suggests:   AskServerSuggestion,
				Completions: func(ctx any, partial string) (int, []string) {
					src, ok := ctx.(Source)
					if !ok {
						return 0, nil
					}
					return 0, Suggestions(partial, src.BannedIPs()...)
				},
			},
		},
	}
}

// runPardonIP runs /pardon-ip <target>, lifting the address's ban. A target
// that is not an address, or an address that is not banned, refuses the
// command.
// vanilla: PardonIpCommand.unban
func runPardonIP(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete pardon-ip command")
		return 0, nil
	}
	target := args[0]
	if net.ParseIP(target) == nil {
		return 0, refuse(src, "commands.pardonip.invalid")
	}
	if !src.PardonIP(target) {
		return 0, refuse(src, "commands.pardonip.failed")
	}
	return resultOf(1, feedbackToAdmins(src, "commands.pardonip.success", protocol.Text{Text: target}))
}
