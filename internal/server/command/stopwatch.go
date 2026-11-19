package command

import (
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// Stopwatch is the /stopwatch command: the stopwatches that measure real
// time rather than ticks, so a paused or lagging server still moves them.
var Stopwatch = Command{
	Name:   "stopwatch",
	Level:  PermLevelGamemasters,
	Build:  stopwatchNode,
	Run:    discardResult(runStopwatch),
	Result: runStopwatch,
}

// Translation keys for the /stopwatch command's output, as the vanilla
// client's language files know them.
const (
	stopwatchAlreadyExistsKey = "commands.stopwatch.already_exists"
	stopwatchCreateKey        = "commands.stopwatch.create.success"
	stopwatchDoesNotExistKey  = "commands.stopwatch.does_not_exist"
	stopwatchQueryKey         = "commands.stopwatch.query"
	stopwatchRemoveKey        = "commands.stopwatch.remove.success"
	stopwatchRestartKey       = "commands.stopwatch.restart.success"
)

// stopwatchNode builds the /stopwatch command tree, matching
// vanilla StopwatchCommand.register: create, query with an optional scale,
// restart and remove, each over a resource-location id.
func stopwatchNode() *Node {
	create := &Node{Type: NodeArgument, Name: "id", Parser: ParserResourceLoc, Executable: true}
	// query <id> [<scale>]: the scale multiplies the value the command
	// returns rather than the one it shows, so it is read but not used here.
	query := stopwatchIDArg()
	query.Executable = true
	query.Children = []*Node{{Type: NodeArgument, Name: "scale", Parser: ParserDouble, Executable: true,
		Properties: func(b *protocol.Buffer) { b.PutU8(0) }, Check: checkDoubleArg}}
	restart := stopwatchIDArg()
	restart.Executable = true
	remove := stopwatchIDArg()
	remove.Executable = true
	return &Node{Type: NodeLiteral, Name: "stopwatch", Required: PermLevelGamemasters, Children: []*Node{
		{Type: NodeLiteral, Name: "create", Children: []*Node{create}},
		{Type: NodeLiteral, Name: "query", Children: []*Node{query}},
		{Type: NodeLiteral, Name: "restart", Children: []*Node{restart}},
		{Type: NodeLiteral, Name: "remove", Children: []*Node{remove}},
	}}
}

// stopwatchIDArg is the id argument the query, restart and remove branches
// read: the resource-location reader create reads, offering the ids that
// already exist.
func stopwatchIDArg() *Node {
	return &Node{Type: NodeArgument, Name: "id", Parser: ParserResourceLoc,
		Suggests: AskServerSuggestion, Completions: stopwatchCompletions}
}

// stopwatchCompletions are what an id argument offers: every stopwatch that
// exists, in id order.
func stopwatchCompletions(ctx any, partial string) (int, []string) {
	src, ok := ctx.(Source)
	if !ok {
		return 0, nil
	}
	return 0, ResourceSuggestions(partial, src.StopwatchIDs()...)
}

// runStopwatch dispatches the /stopwatch command to the subcommand the player
// typed; args is everything after "stopwatch".
func runStopwatch(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete stopwatch command")
		return 0, nil
	}
	switch args[0] {
	case "create":
		return stopwatchCreate(src, args[1:])
	case "query":
		return stopwatchQuery(src, args[1:])
	case "restart":
		return stopwatchRestart(src, args[1:])
	case "remove":
		return stopwatchRemove(src, args[1:])
	default:
		src.Debug("unknown stopwatch subcommand", "sub", args[0])
		return 0, nil
	}
}

// stopwatchCreate runs /stopwatch create <id>.
func stopwatchCreate(src Source, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete stopwatch create")
		return 0, nil
	}
	id, ok := parseResourceLocation(args[0])
	if !ok {
		src.Debug("stopwatch id is not a resource location", "id", args[0])
		return 0, nil
	}
	if !src.CreateStopwatch(id) {
		return 0, refuse(src, stopwatchAlreadyExistsKey, protocol.Text{Text: id})
	}
	return resultOf(1, feedbackToAdmins(src, stopwatchCreateKey, protocol.Text{Text: id}))
}

// stopwatchQuery runs /stopwatch query <id> [<scale>]. The elapsed seconds
// are printed the way Java's Double.toString does, the text the client shows
// a Double argument as; the scale only multiplies the value the command
// returns and leaves the message alone.
func stopwatchQuery(src Source, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete stopwatch query")
		return 0, nil
	}
	id, ok := parseResourceLocation(args[0])
	if !ok {
		src.Debug("stopwatch id is not a resource location", "id", args[0])
		return 0, nil
	}
	seconds, ok := src.StopwatchSeconds(id)
	if !ok {
		return 0, refuse(src, stopwatchDoesNotExistKey, protocol.Text{Text: id})
	}
	scale := 1.0
	if len(args) > 1 {
		if v, err := strconv.ParseFloat(args[1], 64); err == nil {
			scale = v
		}
	}
	return resultOf(javaDoubleToInt(seconds*scale), feedbackToAdmins(src, stopwatchQueryKey, protocol.Text{Text: id},
		protocol.Text{Text: nbt.JavaFloatText(seconds, 64)}))
}

// stopwatchRestart runs /stopwatch restart <id>.
func stopwatchRestart(src Source, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete stopwatch restart")
		return 0, nil
	}
	id, ok := parseResourceLocation(args[0])
	if !ok {
		src.Debug("stopwatch id is not a resource location", "id", args[0])
		return 0, nil
	}
	if !src.RestartStopwatch(id) {
		return 0, refuse(src, stopwatchDoesNotExistKey, protocol.Text{Text: id})
	}
	return resultOf(1, feedbackToAdmins(src, stopwatchRestartKey, protocol.Text{Text: id}))
}

// stopwatchRemove runs /stopwatch remove <id>.
func stopwatchRemove(src Source, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete stopwatch remove")
		return 0, nil
	}
	id, ok := parseResourceLocation(args[0])
	if !ok {
		src.Debug("stopwatch id is not a resource location", "id", args[0])
		return 0, nil
	}
	if !src.RemoveStopwatch(id) {
		return 0, refuse(src, stopwatchDoesNotExistKey, protocol.Text{Text: id})
	}
	return resultOf(1, feedbackToAdmins(src, stopwatchRemoveKey, protocol.Text{Text: id}))
}
