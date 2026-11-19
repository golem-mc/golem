package command

import (
	"math"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// ScheduleCommand is the /schedule command: it queues a function or a
// function tag to run at a later game tick, and clears queued events.
var ScheduleCommand = Command{
	Name:   "schedule",
	Level:  PermLevelGamemasters,
	Build:  scheduleNode,
	Run:    discardResult(runSchedule),
	Result: runSchedule,
}

// The translation keys /schedule answers with.
const (
	scheduleSameTickKey        = "commands.schedule.same_tick"
	scheduleCreatedFunctionKey = "commands.schedule.created.function"
	scheduleCreatedTagKey      = "commands.schedule.created.tag"
	scheduleMacroKey           = "commands.schedule.macro"
	scheduleClearedKey         = "commands.schedule.cleared.success"
	scheduleClearFailureKey    = "commands.schedule.cleared.failure"
)

// scheduleNode builds the /schedule tree.
// vanilla: ScheduleCommand.register
func scheduleNode() *Node {
	timeArg := &Node{Type: NodeArgument, Name: "time", Parser: ParserTime, Executable: true,
		Check:      checkTimeMin(0),
		Properties: func(b *protocol.Buffer) { b.PutI32(0) },
		Children: []*Node{
			{Type: NodeLiteral, Name: "append", Executable: true},
			{Type: NodeLiteral, Name: "replace", Executable: true},
		}}
	function := &Node{Type: NodeArgument, Name: "function", Parser: ParserFunction,
		Check: checkFunctionSyntax, Suggests: AskServerSuggestion, Completions: functionCompletions,
		Children: []*Node{timeArg}}
	clearArg := &Node{Type: NodeArgument, Name: "function", Parser: ParserResourceLoc, Executable: true,
		Suggests:    AskServerSuggestion,
		Completions: scheduledCompletions}
	return &Node{Type: NodeLiteral, Name: "schedule", Required: PermLevelGamemasters, Children: []*Node{
		{Type: NodeLiteral, Name: "function", Children: []*Node{function}},
		{Type: NodeLiteral, Name: "clear", Children: []*Node{clearArg}},
	}}
}

// scheduledCompletions offers the ids of the queued events.
// vanilla: ScheduleCommand.SUGGEST_SCHEDULE
func scheduledCompletions(ctx any, partial string) (int, []string) {
	src, ok := ctx.(Source)
	if !ok {
		return 0, nil
	}
	return 0, Suggestions(partial, src.ScheduledEvents().IDs()...)
}

// runSchedule runs /schedule function and /schedule clear.
func runSchedule(src Source, _ string, args []string) (int32, error) {
	if len(args) < 2 {
		src.Debug("incomplete schedule command")
		return 0, nil
	}
	switch args[0] {
	case "function":
		if len(args) < 3 {
			src.Debug("incomplete schedule command")
			return 0, nil
		}
		replace := len(args) < 4 || args[3] != "append"
		return scheduleFunction(src, args[1], args[2], replace)
	case "clear":
		return scheduleClear(src, registry.Qualify(args[1]))
	}
	src.Debug("unknown schedule subcommand", "sub", args[0])
	return 0, nil
}

// scheduleFunction queues the function or tag to run time ticks from now.
// Replace first drops the events of the same name.
// vanilla: ScheduleCommand.schedule
func scheduleFunction(src Source, word, timeWord string, replace bool) (int32, error) {
	ticks, ok := ParseTimeArg(timeWord)
	if !ok {
		src.Debug("invalid schedule time", "time", timeWord)
		return 0, nil
	}
	tag := strings.HasPrefix(word, "#")
	id := registry.Qualify(strings.TrimPrefix(word, "#"))
	// An unknown tag is an empty one and schedules fine.
	// vanilla: ServerFunctionLibrary.getTag
	fn, known := src.Functions().Get(id)
	if !tag && !known {
		return 0, refuse(src, functionUnknownKey, protocol.Text{Text: id})
	}
	if ticks == 0 {
		return 0, refuse(src, scheduleSameTickKey)
	}
	// A macro cannot be instantiated without the arguments it substitutes,
	// and the game loop has none to give it, so scheduling one names what
	// would never run. A tag schedules regardless: at its fire time the
	// loop drops a macro member as quietly as any failed instantiation.
	// vanilla: ScheduleCommand.schedule, ServerFunctionManager.execute
	if !tag && fn.macro != nil {
		return 0, refuse(src, scheduleMacroKey)
	}
	gameTime, _, _ := src.ClockTime(world.OverworldClock)
	at := gameTime + ticks
	scheduleID, key := id, scheduleCreatedFunctionKey
	if tag {
		scheduleID, key = "#"+id, scheduleCreatedTagKey
	}
	queue := src.ScheduledEvents()
	if replace {
		queue.Remove(scheduleID)
	}
	queue.Schedule(scheduleID, at, world.TimerCallback{Tag: tag, ID: id})
	if err := feedbackToAdmins(src, key, protocol.Text{Text: id}, protocol.Text{Number: &ticks}, longFeedback(at)); err != nil {
		return 0, err
	}
	// vanilla: Math.floorMod(tickTime, Integer.MAX_VALUE)
	return int32(((at % math.MaxInt32) + math.MaxInt32) % math.MaxInt32), nil
}

// scheduleClear drops the queued events of the name and answers how many.
// vanilla: ScheduleCommand.remove
func scheduleClear(src Source, id string) (int32, error) {
	count := src.ScheduledEvents().Remove(id)
	if count == 0 {
		return 0, refuse(src, scheduleClearFailureKey, protocol.Text{Text: id})
	}
	n := int64(count)
	if err := feedbackToAdmins(src, scheduleClearedKey, protocol.Text{Number: &n}, protocol.Text{Text: id}); err != nil {
		return 0, err
	}
	return int32(count), nil
}
