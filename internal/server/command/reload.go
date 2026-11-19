package command

// Reload is the /reload command.
var Reload = Command{
	Name:   "reload",
	Level:  PermLevelGamemasters,
	Build:  reloadNode,
	Run:    discardResult(runReload),
	Result: runReload,
}

// reloadNode builds the /reload tree.
// vanilla: ReloadCommand.register
func reloadNode() *Node {
	return &Node{Type: NodeLiteral, Name: "reload", Required: PermLevelGamemasters, Executable: true}
}

// runReload rereads the data packs. The success line goes out before the
// reload runs, the failure line after it; the result is 0 either way.
// vanilla: ReloadCommand.register
func runReload(src Source, _ string, _ []string) (int32, error) {
	if err := feedbackToAdmins(src, "commands.reload.success"); err != nil {
		return 0, err
	}
	return 0, reportReload(src, src.DataPacks().ReloadAll())
}
