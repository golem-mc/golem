package command

// Defaultgamemode is the /defaultgamemode command.
var Defaultgamemode = Command{
	Name:   "defaultgamemode",
	Level:  PermLevelGamemasters,
	Build:  defaultgamemodeNode,
	Run:    discardResult(runDefaultgamemode),
	Result: runDefaultgamemode,
}

// defaultgamemodeNode builds /defaultgamemode <gamemode>.
// vanilla: DefaultGameModeCommands.register
func defaultgamemodeNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "defaultgamemode",
		Required: PermLevelGamemasters,
		Children: []*Node{
			gamemodeArgNode(),
		},
	}
}

// runDefaultgamemode sets the mode new players join in.
// vanilla: DefaultGameModeCommands.setMode
func runDefaultgamemode(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete defaultgamemode command")
		return 0, nil
	}
	mode, ok := parseGameMode(args[0])
	if !ok {
		src.Debug("unknown game mode", "mode", args[0])
		return 0, nil
	}
	count := src.SetDefaultGameMode(mode)
	return resultOf(count, feedbackToAdmins(src, "commands.defaultgamemode.success", gameModeName(mode)))
}
