package command

import (
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
)

// SetIdleTimeout is the /setidletimeout command.
var SetIdleTimeout = Command{
	Name:  "setidletimeout",
	Level: PermLevelAdmins,
	Build: func() *Node {
		return &Node{
			Type:     NodeLiteral,
			Name:     "setidletimeout",
			Required: PermLevelAdmins,
			Children: []*Node{{
				Type:       NodeArgument,
				Name:       "minutes",
				Executable: true,
				Parser:     ParserInteger,
				Int:        &IntArg{Min: 0, HasMin: true},
			}},
		}
	},
	Run:    discardResult(runSetIdleTimeout),
	Result: runSetIdleTimeout,
}

// runSetIdleTimeout sets the minutes a player may idle before the kick;
// zero turns the kick off.
// vanilla: SetPlayerIdleTimeoutCommand
func runSetIdleTimeout(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete setidletimeout command")
		return 0, nil
	}
	minutes, err := strconv.Atoi(args[0])
	if err != nil {
		src.Debug("bad setidletimeout minutes", "arg", args[0])
		return 0, nil
	}
	src.SetIdleTimeout(minutes)
	if minutes > 0 {
		return resultOf(int32(minutes), feedbackToAdmins(src, "commands.setidletimeout.success",
			protocol.Text{Text: strconv.Itoa(minutes)}))
	}
	return resultOf(0, feedbackToAdmins(src, "commands.setidletimeout.success.disabled"))
}
