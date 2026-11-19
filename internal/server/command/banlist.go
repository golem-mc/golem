package command

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// BanList is the /banlist command.
var BanList = Command{
	Name:   "banlist",
	Level:  PermLevelAdmins,
	Build:  banListNode,
	Run:    discardResult(runBanList),
	Result: runBanList,
}

// banListNode builds the /banlist command tree: a bare command listing both
// ban lists, with ips and players naming which one to show.
// vanilla: BanListCommands
func banListNode() *Node {
	return &Node{
		Type:       NodeLiteral,
		Name:       "banlist",
		Required:   PermLevelAdmins,
		Executable: true,
		Children: []*Node{
			{Type: NodeLiteral, Name: "ips", Executable: true},
			{Type: NodeLiteral, Name: "players", Executable: true},
		},
	}
}

// runBanList runs /banlist [ips|players]. With no argument it shows both the
// player and the address bans, concatenated; ips or players names one list.
// Every line is the runner's alone, not broadcast to the other operators.
// vanilla: BanListCommands.showList
func runBanList(src Source, input string, args []string) (int32, error) {
	rows := src.BanList()
	switch {
	case len(args) == 0:
		rows = append(rows, src.IPBanList()...)
	case args[0] == "ips":
		rows = src.IPBanList()
	}
	if len(rows) == 0 {
		return 0, src.Feedback("commands.banlist.none")
	}
	if err := src.Feedback("commands.banlist.list", entityCount(len(rows))); err != nil {
		return 0, err
	}
	for _, row := range rows {
		if err := src.Feedback("commands.banlist.entry", row.Name, protocol.Text{Text: row.Source}, row.Reason); err != nil {
			return 0, err
		}
	}
	return int32(len(rows)), nil
}
