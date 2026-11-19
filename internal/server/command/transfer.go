package command

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// Transfer is the /transfer command.
var Transfer = Command{
	Name:   "transfer",
	Level:  PermLevelAdmins,
	Build:  transferNode,
	Run:    discardResult(runTransfer),
	Result: runTransfer,
}

// transferNode builds the /transfer tree: a hostname, then a port and the
// players behind it, each optional. The hostname left alone transfers the
// runner to vanilla's default port, the players left out is the runner.
// vanilla: TransferCommand.register
func transferNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "transfer",
		Required: PermLevelAdmins,
		Children: []*Node{
			{
				Type:       NodeArgument,
				Name:       "hostname",
				Executable: true,
				Parser:     ParserString,
				Properties: func(b *protocol.Buffer) { b.PutVarInt(1) }, // quotable phrase
				Quotable:   true,
				Children: []*Node{
					{
						Type:       NodeArgument,
						Name:       "port",
						Executable: true,
						Parser:     ParserInteger,
						Int:        &IntArg{Min: 1, Max: 65535, HasMin: true, HasMax: true},
						Children: []*Node{
							{
								Type:       NodeArgument,
								Name:       "players",
								Executable: true,
								Parser:     ParserEntity,
								Properties: func(b *protocol.Buffer) { b.PutU8(0x02) }, // players, several
								EntityArg:  &EntityArg{PlayersOnly: true},
							},
						},
					},
				},
			},
		},
	}
}

// runTransfer runs /transfer <host> [port] [players]: each target (default
// the runner) reconnects to host:port (default 25565); the packets go before
// the feedback.
// vanilla: TransferCommand.transfer
func runTransfer(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete transfer command")
		return 0, nil
	}

	// The hostname may be a quoted phrase the walk read across words.
	span := valueSpanCount(args)
	host, _, hostErr := readStringValue(wordsIn(input, args, 0, span))
	if hostErr != nil {
		return 0, nil
	}
	rest := args[span:]
	port := int32(25565)
	if len(rest) > 0 {
		value, ok := parseInteger(rest[0])
		if !ok {
			return 0, nil
		}
		port = int32(value)
		rest = rest[1:]
	}
	word, _ := splitTargets(input, rest)
	targets, err := resolvePlayersOrSelf(src, word)
	if err != nil {
		return 0, err
	}

	for _, target := range targets {
		target.Transfer(host, port)
	}
	hostArg := protocol.Text{Text: host}
	port64 := int64(port)
	portArg := protocol.Text{Number: &port64}
	if len(targets) == 1 {
		return resultOf(1, feedbackToAdmins(src, "commands.transfer.success.single",
			targets[0].DisplayName(), hostArg, portArg))
	}
	return resultOf(int32(len(targets)), feedbackToAdmins(src, "commands.transfer.success.multiple",
		entityCount(len(targets)), hostArg, portArg))
}
