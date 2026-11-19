package command

// SaveOff is the /save-off command.
var SaveOff = saveToggle("save-off", false, "commands.save.disabled", "commands.save.alreadyOff")

// SaveOn is the /save-on command.
var SaveOn = saveToggle("save-on", true, "commands.save.enabled", "commands.save.alreadyOn")

// saveToggle builds the command that switches the auto-save to on.
// vanilla: SaveOffCommand, SaveOnCommand
func saveToggle(name string, on bool, doneKey, alreadyKey string) Command {
	return Command{
		Name:  name,
		Level: PermLevelOwners,
		Build: func() *Node {
			return &Node{
				Type:       NodeLiteral,
				Name:       name,
				Executable: true,
				Required:   PermLevelOwners,
			}
		},
		Run: func(src Source, input string, args []string) error {
			if !src.SetSaving(on) {
				return refuse(src, alreadyKey)
			}
			return feedbackToAdmins(src, doneKey)
		},
	}
}
