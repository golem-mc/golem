package command

// SaveAll is the /save-all command.
var SaveAll = Command{
	Name:  "save-all",
	Level: PermLevelOwners,
	Build: saveAllNode,
	Run:   runSaveAll,
}

// The translation keys the /save-all command answers with.
// vanilla: SaveAllCommand
const (
	saveAllSavingKey  = "commands.save.saving"
	saveAllSuccessKey = "commands.save.success"
	saveAllFailedKey  = "commands.save.failed"
)

// saveAllNode builds the /save-all command tree, matching the vanilla
// report: the command runs bare, and takes the flush literal.
// vanilla: SaveAllCommand.register
func saveAllNode() *Node {
	return &Node{
		Type:       NodeLiteral,
		Name:       "save-all",
		Executable: true,
		Required:   PermLevelOwners,
		Children: []*Node{{
			Type:       NodeLiteral,
			Name:       "flush",
			Executable: true,
			Required:   PermLevelOwners,
		}},
	}
}

// runSaveAll runs /save-all. The tree offers only the bare command and the
// flush literal, so args is empty or "flush". flush names vanilla's immediate
// flush, which this server's synchronous save always performs.
// vanilla: SaveAllCommand
func runSaveAll(src Source, input string, args []string) error {
	if err := src.Feedback(saveAllSavingKey); err != nil {
		return err
	}
	if !src.SaveAll(len(args) > 0) {
		return refuse(src, saveAllFailedKey)
	}
	return feedbackToAdmins(src, saveAllSuccessKey)
}
