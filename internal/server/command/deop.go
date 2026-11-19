package command

// Deop is the /deop command.
var Deop = Command{
	Name:   "deop",
	Level:  PermLevelAdmins,
	Build:  deopNode,
	Run:    discardResult(runDeop),
	Result: runDeop,
}

// deopNode builds the /deop command tree, the twin of /op's.
func deopNode() *Node {
	return &Node{
		Type:     NodeLiteral,
		Name:     "deop",
		Required: PermLevelAdmins,
		Children: []*Node{
			{
				Type:       NodeArgument,
				Name:       "targets",
				Executable: true,
				Parser:     ParserGameProfile,
				EntityArg:  profileTargets(),
				Suggests:   AskServerSuggestion,
				Completions: func(ctx any, partial string) (int, []string) {
					return EntityCompletions(ctx, partial)
				},
			},
		},
	}
}

// runDeop runs /deop <targets>, which takes the permission level away
// again and tells the clients the same way.
func runDeop(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete deop command")
		return 0, nil
	}
	return changeOp(src, input, args, false)
}
