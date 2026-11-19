package command

// Stop is the /stop command.
var Stop = Command{
	Name:  "stop",
	Level: PermLevelOwners,
	Build: stopNode,
	Run:   runStop,
}

// commandStopKey is the line the vanilla server answers with as it stops.
const commandStopKey = "commands.stop.stopping"

// stopNode builds the /stop command tree, which runs bare.
func stopNode() *Node {
	return &Node{
		Type:       NodeLiteral,
		Name:       "stop",
		Executable: true,
		Required:   PermLevelOwners,
	}
}

// runStop runs /stop, which says the server is stopping and then stops it:
// the listener stops taking connections, the world is saved and the
// process ends, cleanly enough for a restart to put everyone back where
// they were.
func runStop(src Source, input string, args []string) error {
	if err := feedbackToAdmins(src, commandStopKey); err != nil {
		return err
	}
	src.StopServer()
	return nil
}
