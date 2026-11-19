package command

import (
	"net"

	"github.com/golem-mc/golem/internal/protocol"
)

// BanIP is the /ban-ip command.
var BanIP = Command{
	Name:   "ban-ip",
	Level:  PermLevelAdmins,
	Build:  banIPNode,
	Run:    discardResult(runBanIP),
	Result: runBanIP,
}

// banIPNode builds the /ban-ip command tree: an address or an online player's
// name, then an optional reason.
// vanilla: BanPlayerCommands
func banIPNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "ban-ip",
		Required: PermLevelAdmins,
		Children: []*Node{
			{
				Type:       NodeArgument,
				Name:       "target",
				Executable: true,
				Parser:     ParserString,
				Properties: func(b *protocol.Buffer) { b.PutVarInt(0) }, // single word
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

// runBanIP runs /ban-ip <target> [reason]: an address, or an online player's
// address. Everyone online behind it is disconnected and named; an unknown
// name, bad address or already-banned address refuses.
func runBanIP(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete ban-ip command")
		return 0, nil
	}
	target := args[0]
	ip := target
	if net.ParseIP(target) == nil {
		found, ok := src.OnlinePlayerIP(target)
		if !ok {
			return 0, refuse(src, "commands.banip.invalid")
		}
		ip = found
	}
	reason, hasReason := greedyArgument(input, "ban-ip", target)
	if hasReason {
		reason = resolveChatMessage(src, reason).Plain()
	}
	affected, added := src.BanIP(ip, reason)
	if !added {
		return 0, refuse(src, "commands.banip.failed")
	}
	if err := feedbackToAdmins(src, "commands.banip.success", protocol.Text{Text: ip}, banReasonText(reason, hasReason)); err != nil {
		return 0, err
	}
	if len(affected) > 0 {
		if err := feedbackToAdmins(src, "commands.banip.info", entityCount(len(affected)), formatDisplayNames(asEntities(affected))); err != nil {
			return 0, err
		}
	}
	kick := protocol.Text{Translate: "multiplayer.disconnect.ip_banned"}
	for _, target := range affected {
		target.Disconnect(kick)
	}
	return int32(len(affected)), nil
}
