package command

import (
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
)

// List is the /list command.
var List = Command{
	Name:   "list",
	Level:  0,
	Build:  listNode,
	Run:    discardResult(runList),
	Result: runList,
}

// Translation keys for the /list command's output, as the vanilla client's
// language files know them. Sending a translation rather than a finished
// sentence lets each player read it in the language they play in.
const (
	listPlayersKey   = "commands.list.players"
	listNameAndIDKey = "commands.list.nameAndId"
)

// listNode builds the /list command tree, matching the vanilla
// report: the command runs bare, and takes a uuids literal that asks for
// names with UUIDs.
func listNode() *Node {
	return &Node{
		Type:       NodeLiteral,
		Name:       "list",
		Executable: true,
		Children: []*Node{
			{
				Type:       NodeLiteral,
				Name:       "uuids",
				Executable: true,
			},
		},
	}
}

// runList runs /list: commands.list.players with the count, the max and the
// comma-separated names, each with its UUID in parentheses under "uuids".
// vanilla: ListPlayersCommand
func runList(src Source, input string, args []string) (int32, error) {
	showUUIDs := len(args) > 0 && args[0] == "uuids"
	targets := src.OnlineTargets()
	return resultOf(int32(len(targets)), src.Feedback(listPlayersKey,
		protocol.Text{Text: strconv.Itoa(len(targets))},
		protocol.Text{Text: strconv.Itoa(src.MaxPlayers())},
		playerListText(targets, showUUIDs)))
}

// playerListText builds the text component for the list of player names, the
// way ComponentUtils.formatList does in vanilla: empty for no players, the
// name alone for one, and a run of names separated by a gray ", " for more.
func playerListText(targets []Target, showUUIDs bool) protocol.Text {
	switch len(targets) {
	case 0:
		return protocol.Text{Text: ""}
	case 1:
		return playerNameText(targets[0], showUUIDs)
	}
	extra := make([]protocol.Text, 0, len(targets)*2-1)
	for i, p := range targets {
		if i > 0 {
			extra = append(extra, protocol.Text{Text: ", ", Color: protocol.ColorGray})
		}
		extra = append(extra, playerNameText(p, showUUIDs))
	}
	return protocol.Text{Extra: extra}
}

// playerNameText builds the text component for a single player: the display
// name, or with showUUIDs a translation of "commands.list.nameAndId" carrying
// the plain name and the UUID, which the client renders as "Name (uuid)".
// vanilla: ListPlayersCommand.listPlayers
func playerNameText(p Target, showUUIDs bool) protocol.Text {
	if showUUIDs {
		return protocol.Text{
			Translate: listNameAndIDKey,
			With: []protocol.Text{
				{Text: p.Profile().Name},
				{Text: p.Profile().UUID.String()},
			},
		}
	}
	return p.DisplayName()
}
