package command

// Say is the /say command.
var Say = Command{
	Name:  "say",
	Level: PermLevelGamemasters,
	Build: sayNode,
	Run:   runSay,
}

// sayNode builds the /say command tree: the rest of the line is the message.
func sayNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "say",
		Required: PermLevelGamemasters,
		Children: []*Node{
			{
				Type:       NodeArgument,
				Name:       "message",
				Executable: true,
				Parser:     ParserMessage,
				Greedy:     true,
			},
		},
	}
}

// runSay runs /say <message>, which announces the message to everyone in
// the world over the say_command chat type, the way the vanilla server
// broadcasts what its console and its operators say.
func runSay(src Source, input string, args []string) error {
	message, ok := greedyArgument(input, "say")
	if !ok {
		src.Debug("incomplete say command")
		return nil
	}
	src.Debug("say", "message", message)
	src.BroadcastSay(src.ChatArgument("message"), resolveChatMessage(src, message))
	return nil
}
