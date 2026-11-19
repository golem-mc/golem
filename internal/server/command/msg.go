package command

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
)

// Msg is the /msg command.
var Msg = Command{
	Name:   "msg",
	Level:  0,
	Build:  msgNode,
	Run:    discardResult(runMsg),
	Result: runMsg,
}

// Tell and W are the two aliases of /msg: the tree sends them as redirects
// to it and the dispatch reaches them through CanonicalName.
var (
	Tell = Command{
		Name:  "tell",
		Level: 0,
		Build: tellNode,
	}
	W = Command{
		Name:  "w",
		Level: 0,
		Build: wNode,
	}
)

// msgNode builds the /msg command tree: a set of player targets, then
// the rest of the line as the message.
func msgNode() *Node {
	return &Node{
		Type: NodeLiteral,
		Name: "msg",
		Children: []*Node{
			{
				Type:       NodeArgument,
				Name:       "targets",
				Parser:     ParserEntity,
				Properties: func(b *protocol.Buffer) { b.PutU8(0x02) }, // players, several
				EntityArg:  &EntityArg{PlayersOnly: true},
				Suggests:   AskServerSuggestion,
				Completions: func(ctx any, partial string) (int, []string) {
					return EntityCompletions(ctx, partial)
				},
				Children: []*Node{
					{
						Type:       NodeArgument,
						Name:       "message",
						Executable: true,
						Parser:     ParserMessage,
						Greedy:     true,
					},
				},
			},
		},
	}
}

// tellNode and wNode build the two aliases of /msg, which are redirects the
// same way /tp is a redirect of /teleport.
func tellNode() *Node {
	return &Node{
		Type:         NodeLiteral,
		Name:         "tell",
		RedirectName: "msg",
	}
}

func wNode() *Node {
	return &Node{
		Type:         NodeLiteral,
		Name:         "w",
		RedirectName: "msg",
	}
}

// runMsg runs /msg <targets> <message>: each target sees it, and the sender
// sees the outgoing form once per target; nobody else does. The message is
// the input after the selector, spacing kept. vanilla: MsgCommand.sendMessage
func runMsg(src Source, input string, args []string) (int32, error) {
	if len(args) < 2 {
		src.Debug("incomplete msg command")
		return 0, nil
	}
	word, _ := splitTargets(input, args)
	targets, err := resolvePlayers(src, word)
	if err != nil {
		return 0, err
	}
	// The message is one greedy argument, which read every word after the
	// target; it begins at the token behind the selector's own words,
	// keeping whatever spacing follows it.
	tokens := Tokenize(input)
	span, _ := selectorScan(args)
	if len(tokens) < 2+span {
		src.Debug("incomplete msg command")
		return 0, nil
	}
	message := input[tokens[span].Start+len(tokens[span].Text)+1:]
	resolved := resolveChatMessage(src, message)
	// Attributed to the source's display name (an execute as body's), with
	// the runner's profile, which as keeps. Without a signing context
	// (unsigned, console, command block) no profile: the system form.
	from := src.DisplayName()
	argument := src.ChatArgument("message")

	for _, target := range targets {
		// The sender reads the outgoing form, which names who it went to,
		// and the target the incoming form, which names who it came from.
		src.OutgoingMessage(from, argument, target.DisplayName(), resolved)
		target.IncomingMessage(from, argument, resolved)
	}
	return int32(len(targets)), nil
}

// greedyArgument takes what a greedy argument read out of the command input:
// everything after the words the command's fixed part consumed, spacing
// kept. Each fixed word takes exactly one space after it, as Brigadier reads
// its arguments.
func greedyArgument(input string, fixed ...string) (string, bool) {
	for _, word := range fixed {
		space := strings.IndexByte(input, ' ')
		if space < 0 || input[:space] != word {
			return "", false
		}
		input = input[space+1:]
	}
	if input == "" {
		return "", false
	}
	return input, true
}
