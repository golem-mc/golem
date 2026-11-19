package command

// Me is the /me command.
var Me = Command{
	Name:  "me",
	Level: 0,
	Build: meNode,
	Run:   runMe,
}

// meNode builds the /me command tree: the rest of the line is the action.
func meNode() *Node {
	return &Node{
		Type: NodeLiteral,
		Name: "me",
		Children: []*Node{
			{
				Type:       NodeArgument,
				Name:       "action",
				Executable: true,
				Parser:     ParserMessage,
				Greedy:     true,
			},
		},
	}
}

// runMe runs /me <action>, which tells the world what the player is doing
// over the emote_command chat type, whose layout renders it the way an
// action line is rendered.
func runMe(src Source, input string, args []string) error {
	action, ok := greedyArgument(input, "me")
	if !ok {
		src.Debug("incomplete me command")
		return nil
	}
	src.BroadcastMe(src.ChatArgument("action"), resolveChatMessage(src, action))
	return nil
}
