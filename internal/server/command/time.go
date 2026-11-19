package command

import (
	"math"
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Time is the /time command.
var Time = Command{
	Name:   "time",
	Level:  PermLevelGamemasters,
	Build:  timeNode,
	Run:    discardResult(runTime),
	Result: runTime,
}

// Translation keys for the /time command's output, as the vanilla client's
// language files know them.
const (
	timeSetAbsoluteKey      = "commands.time.set.absolute"
	timeSetTimeMarkerKey    = "commands.time.set.time_marker"
	timeQueryGametimeKey    = "commands.time.query.gametime"
	timeQueryAbsoluteKey    = "commands.time.query.absolute"
	timeQueryTimelineKey    = "commands.time.query.timeline"
	timeQueryTimelineRepKey = "commands.time.query.timeline.repetitions"
	timeWrongTimelineKey    = "commands.time.wrong_timeline_for_clock"
	timePauseKey            = "commands.time.pause"
	timeResumeKey           = "commands.time.resume"
	timeRateKey             = "commands.time.rate"

	timeMarkerNotFoundKey      = "commands.time.no_time_marker_found"
	timeNoDefaultClockKey      = "commands.time.no_default_clock"
	timeAlreadyAtTimeKey       = "commands.time.set.already_at_time"
	timeAlreadyAtTimeMarkerKey = "commands.time.set.already_at_time_marker"
	timeAlreadyPausedKey       = "commands.time.pause.already_paused"
	timeAlreadyRunningKey      = "commands.time.pause.already_running"
	timeRateAlreadySameKey     = "commands.time.rate.already_same"
)

// timeMarkers maps the "day" timeline's show_in_commands markers to their
// ticks within its 24000-tick period, for /time set <marker> to move the
// overworld clock to. No other clock runs that timeline.
// vanilla: ClockTimeMarker.resolveTimeToMoveTo
var timeMarkers = map[string]int64{
	"minecraft:day":      1000,
	"minecraft:midnight": 18000,
	"minecraft:night":    13000,
	"minecraft:noon":     6000,
}

// timeline is one timeline of the minecraft:timeline registry: the clock it
// answers for and the period it repeats over, 0 for the registry's entries
// that ship without one.
type timeline struct {
	clock  string
	period int64
}

// timelines maps minecraft:timeline entries to their clock and period: a
// query answers the clock's total modulo the period, and repetitions by
// dividing. From the data pack's data/minecraft/timeline/*.json; moon's
// 192000 is four overworld days.
var timelines = map[string]timeline{
	"minecraft:day":               {clock: overworldClock, period: ticksPerDay},
	"minecraft:early_game":        {clock: overworldClock},
	"minecraft:moon":              {clock: overworldClock, period: 192000},
	"minecraft:villager_schedule": {clock: overworldClock, period: ticksPerDay},
}

// overworldClock and theEndClock are the built-in world clocks.
const (
	overworldClock = "minecraft:overworld"
	theEndClock    = "minecraft:the_end"
)

// ticksPerDay is the length of a Minecraft day in game ticks, matching the
// constant in internal/server/world/world.go.
const ticksPerDay = 24000

// timeNode builds the /time command tree, matching the vanilla
// TimeCommand.register and the commands.json report: set, add, query,
// pause, resume and rate, plus a /time of <clock> branch.
func timeNode() *Node {
	// addClockNodes builds the subtree under the given parent, which the
	// vanilla server reuses under /time and under /time of <clock>.
	addClockNodes := func(parent *Node) {
		// set <time> (minecraft:time, min=0)
		setTimeTimeArg := &Node{
			Type:       NodeArgument,
			Name:       "time",
			Executable: true,
			Parser:     ParserTime,
			Check:      checkTimeMin(0),
			Properties: func(b *protocol.Buffer) { b.PutI32(0) },
		}
		// set <timemarker> (minecraft:resource_location, ask_server suggestions)
		setTimemarkerArg := &Node{
			Type:        NodeArgument,
			Name:        "timemarker",
			Executable:  true,
			Parser:      ParserResourceLoc,
			Suggests:    AskServerSuggestion,
			Completions: timeMarkerCompletions,
		}
		setNode := &Node{
			Type:     NodeLiteral,
			Name:     "set",
			Children: []*Node{setTimeTimeArg, setTimemarkerArg},
		}

		// add <time> (minecraft:time, min=INT_MIN) — a direct child of the
		// parent, not of set, matching the vanilla command tree.
		addTimeArg := &Node{
			Type:       NodeArgument,
			Name:       "time",
			Executable: true,
			Parser:     ParserTime,
			Properties: func(b *protocol.Buffer) { b.PutI32(-2147483648) },
		}
		addNode := &Node{
			Type:     NodeLiteral,
			Name:     "add",
			Children: []*Node{addTimeArg},
		}

		pauseNode := &Node{Type: NodeLiteral, Name: "pause", Executable: true}
		resumeNode := &Node{Type: NodeLiteral, Name: "resume", Executable: true}

		// rate <rate> (brigadier:float, min=1e-5, max=1000)
		rateArg := &Node{
			Type:       NodeArgument,
			Name:       "rate",
			Executable: true,
			Parser:     ParserFloat,
			Properties: func(b *protocol.Buffer) {
				b.PutU8(0x03)  // flags: has min | has max
				b.PutF32(1e-5) // min
				b.PutF32(1000) // max
			},
		}
		rateNode := &Node{
			Type:     NodeLiteral,
			Name:     "rate",
			Children: []*Node{rateArg},
		}

		// query time (literal, executable)
		queryTimeNode := &Node{Type: NodeLiteral, Name: "time", Executable: true}

		// query <timeline> (minecraft:resource, registry=minecraft:timeline).
		timelineArg := &Node{
			Type:        NodeArgument,
			Name:        "timeline",
			Executable:  true,
			Parser:      ParserResource,
			Properties:  func(b *protocol.Buffer) { b.PutString("minecraft:timeline") },
			Suggests:    AskServerSuggestion,
			Resource:    &ResourceArg{Registry: "minecraft:timeline"},
			Completions: timelineCompletions,
		}
		timelineArg.Children = []*Node{
			{Type: NodeLiteral, Name: "repetition", Executable: true},
		}
		queryNode := &Node{
			Type:     NodeLiteral,
			Name:     "query",
			Children: []*Node{queryTimeNode, timelineArg},
		}

		parent.Children = append(parent.Children,
			setNode, addNode, pauseNode, resumeNode, rateNode, queryNode)
	}

	timeNode := &Node{
		Type:     NodeLiteral,
		Name:     "time",
		Required: PermLevelGamemasters,
	}
	addClockNodes(timeNode)

	// query gametime is only under /time, not under /time of <clock>.
	queryNode := timeNode.Children[5] // the "query" literal added by addClockNodes
	queryNode.Children = append(queryNode.Children,
		&Node{Type: NodeLiteral, Name: "gametime", Executable: true})

	// time of <clock> (minecraft:resource, registry=minecraft:world_clock)
	clockArg := &Node{
		Type:        NodeArgument,
		Name:        "clock",
		Parser:      ParserResource,
		Properties:  func(b *protocol.Buffer) { b.PutString("minecraft:world_clock") },
		Suggests:    AskServerSuggestion,
		Resource:    &ResourceArg{Registry: "minecraft:world_clock"},
		Completions: ResourceCompletions("minecraft:world_clock"),
	}
	addClockNodes(clockArg)
	ofNode := &Node{
		Type:     NodeLiteral,
		Name:     "of",
		Children: []*Node{clockArg},
	}
	timeNode.Children = append(timeNode.Children, ofNode)

	return timeNode
}

// runTime dispatches the /time command to the subcommand the player typed;
// args is everything after "time". Without /time of naming a clock, the
// command turns the dimension type's default clock, if present.
func runTime(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete time command")
		return 0, nil
	}
	if args[0] == "of" {
		return timeOf(src, input, args[1:])
	}
	// vanilla: TimeCommand.queryGameTime, registered outside the clock nodes
	if len(args) == 2 && args[0] == "query" && args[1] == "gametime" {
		age := src.GameTime()
		return resultOf(wrapTime(age), src.Feedback(timeQueryGametimeKey, longFeedback(age)))
	}
	// vanilla: TimeCommand.getDefaultClock
	clock, ok := src.DefaultClock()
	if !ok {
		return 0, refuse(src, timeNoDefaultClockKey, protocol.Text{Text: src.DimensionTypeName()})
	}
	return timeCommand(src, input, args, clock)
}

// timeCommand runs one /time subcommand against the named clock.
func timeCommand(src Source, input string, args []string, clock string) (int32, error) {
	switch args[0] {
	case "set":
		return timeSet(src, args[1:], clock)
	case "add":
		return timeAdd(src, args[1:], clock)
	case "query":
		return timeQuery(src, args[1:], clock)
	case "pause":
		return timePause(src, clock)
	case "resume":
		return timeResume(src, clock)
	case "rate":
		return timeRate(src, args[1:], clock)
	default:
		src.Debug("unknown time subcommand", "sub", args[0])
		return 0, nil
	}
}

// timeSet runs /time set <value> or <marker>. The two are sibling branches,
// time first, so a word reaches the time branch for any value its minimum 0
// passes and the marker branch otherwise.
func timeSet(src Source, args []string, clock string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete time set")
		return 0, nil
	}

	if ticks, ok := ParseTimeArg(args[0]); ok && ticks >= 0 {
		if !src.SetClockTime(clock, ticks) {
			return 0, refuse(src, timeAlreadyAtTimeKey, protocol.Text{Text: clock}, intFeedback(ticks))
		}
		return resultOf(int32(ticks), timeFeedback(src, true, timeSetAbsoluteKey, clock, intFeedback(ticks)))
	}

	// Commands show only the day timeline's markers, which ride the
	// overworld clock; for another clock the ClockManager lookup refuses.
	// vanilla: TimeCommand.setTimeToTimeMarker
	if clock == overworldClock {
		if ticks, ok := resolveTimeMarker(args[0]); ok {
			if !src.MoveClockToMarker(clock, ticks, ticksPerDay) {
				return 0, refuse(src, timeAlreadyAtTimeMarkerKey,
					protocol.Text{Text: clock}, protocol.Text{Text: markerName(args[0])})
			}
			_, total, _ := src.ClockTime(clock)
			return resultOf(wrapTime(total), feedbackToAdmins(src, timeSetTimeMarkerKey,
				protocol.Text{Text: clock}, protocol.Text{Text: markerName(args[0])}))
		}
	}
	return 0, refuse(src, timeMarkerNotFoundKey,
		protocol.Text{Text: markerName(args[0])}, protocol.Text{Text: clock})
}

// timeAdd runs /time add <value> against the clock.
func timeAdd(src Source, args []string, clock string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete time add")
		return 0, nil
	}
	ticks, ok := ParseTimeArg(args[0])
	if !ok {
		src.Debug("invalid time argument", "arg", args[0])
		return 0, nil
	}
	src.AddClockTime(clock, ticks)
	_, total, _ := src.ClockTime(clock)
	// add shares set's feedback key, reporting the total the clock now
	// carries.
	return resultOf(wrapTime(total), timeFeedback(src, true, timeSetAbsoluteKey, clock, longFeedback(total)))
}

// timeQuery runs /time query gametime, /time query time, or
// /time query <timeline> [repetition].
func timeQuery(src Source, args []string, clock string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete time query")
		return 0, nil
	}

	switch args[0] {
	case "gametime":
		worldAge, _, _ := src.ClockTime(clock)
		return resultOf(wrapTime(worldAge), src.Feedback(timeQueryGametimeKey, longFeedback(worldAge)))
	case "time":
		_, total, _ := src.ClockTime(clock)
		return resultOf(wrapTime(total), timeFeedback(src, false, timeQueryAbsoluteKey, clock, longFeedback(total)))
	default:
		return timeQueryTimeline(src, args, clock)
	}
}

// timeQueryTimeline runs /time query <timeline> [repetition]: the clock's
// total modulo the period, or the periods it spans; a timeline of another
// clock fails with wrong_timeline_for_clock.
func timeQueryTimeline(src Source, args []string, clock string) (int32, error) {
	// The walk's resource argument read the word with this same reader, so
	// the name the feedback carries is the id it accepted; the walk has
	// already refused what it refuses.
	name, ok := parseResourceLocation(args[0])
	if !ok {
		src.Debug("timeline name is not a resource location", "name", args[0])
		return 0, nil
	}
	repetition := len(args) > 1 && args[1] == "repetition"

	t, ok := timelines[name]
	if !ok {
		src.Debug("timeline the registry has but this server does not", "timeline", name)
		return 0, nil
	}
	_, total, ok := src.ClockTime(clock)
	if !ok {
		src.Debug("clock this server keeps no state for", "clock", clock)
		return 0, nil
	}
	if t.clock != clock {
		return 0, refuse(src, timeWrongTimelineKey,
			protocol.Text{Text: name}, protocol.Text{Text: clock})
	}

	// A timeline without a period answers the raw total, and counts no
	// repetitions.
	value := total
	if repetition {
		value = 0
		if t.period > 0 {
			value = total / t.period
		}
	} else if t.period > 0 {
		value = total % t.period
	}

	key := timeQueryTimelineKey
	if repetition {
		key = timeQueryTimelineRepKey
	}
	return resultOf(wrapTime(value), timeFeedback(src, false, key, name, longFeedback(value)))
}

// timePause runs /time pause against the clock.
// vanilla: TimeCommand.setPaused
func timePause(src Source, clock string) (int32, error) {
	if !src.SetClockPaused(clock, true) {
		return 0, refuse(src, timeAlreadyPausedKey, protocol.Text{Text: clock})
	}
	return resultOf(1, feedbackToAdmins(src, timePauseKey, protocol.Text{Text: clock}))
}

// timeResume runs /time resume against the clock.
func timeResume(src Source, clock string) (int32, error) {
	if !src.SetClockPaused(clock, false) {
		return 0, refuse(src, timeAlreadyRunningKey, protocol.Text{Text: clock})
	}
	return resultOf(1, feedbackToAdmins(src, timeResumeKey, protocol.Text{Text: clock}))
}

// timeRate runs /time rate <value> against the clock.
func timeRate(src Source, args []string, clock string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete time rate")
		return 0, nil
	}
	rate, err := strconv.ParseFloat(args[0], 32)
	if err != nil {
		src.Debug("invalid rate", "arg", args[0])
		return 0, nil
	}
	if !src.SetClockRate(clock, float32(rate)) {
		return 0, refuse(src, timeRateAlreadySameKey, protocol.Text{Text: clock}, floatFeedback(float32(rate)))
	}
	// The rate is carried as a Java float, which the client renders always
	// with a decimal point, so a rate of one reads "1.0" in front of the
	// "x" the translation key appends.
	return resultOf(1, timeFeedback(src, true, timeRateKey, clock, floatFeedback(float32(rate))))
}

// timeOf runs /time of <clock> <subcommand>. The walk's resource argument
// has already refused a clock the minecraft:world_clock registry does not
// have; the id it accepted is what the subcommands' feedback names and what
// the world's per-clock state rides.
func timeOf(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete time of")
		return 0, nil
	}
	clock, ok := parseResourceLocation(args[0])
	if !ok {
		src.Debug("clock is not a resource location", "clock", args[0])
		return 0, nil
	}
	return timeCommand(src, input, args[1:], clock)
}

// wrapTime folds a tick count into the int an /execute store writes.
// vanilla: TimeCommand.wrapTime
func wrapTime(ticks int64) int32 { return int32(ticks % math.MaxInt32) }

// completionClock is the clock a /time argument's suggestions answer for:
// the one /time of named, carried by the walk's argument state, or else the
// runner's dimension's own. A clock the walk could not read yet offers
// nothing, so ok is false for it.
func completionClock(ctx any) (string, bool) {
	switch state := ctx.(type) {
	case *suggestState:
		if word, typed := state.arg("clock"); typed {
			return parseResourceLocation(word)
		}
		return state.DefaultClock()
	default:
		if src, is := ctx.(Source); is {
			return src.DefaultClock()
		}
	}
	return "", false
}

// timelineCompletions are what the timeline argument of a /time query
// offers: the timelines that answer for the clock the command runs
// against, so a clock that runs no timeline offers none of them.
// vanilla: TimeCommand.suggestTimelines
func timelineCompletions(ctx any, partial string) (int, []string) {
	clock, ok := completionClock(ctx)
	if !ok {
		return 0, nil
	}
	r, found := registry.Get("minecraft:timeline")
	if !found {
		return 0, nil
	}
	candidates := make([]string, 0, len(timelines))
	for _, name := range r.Entries {
		if timelines[name].clock == clock {
			candidates = append(candidates, name)
		}
	}
	return 0, ResourceSuggestions(partial, candidates...)
}

// timeMarkerCompletions offers the clock's command markers: the day
// timeline's four for the overworld, none for any other.
// vanilla: TimeCommand.suggestTimeMarkers
func timeMarkerCompletions(ctx any, partial string) (int, []string) {
	clock, ok := completionClock(ctx)
	if !ok || clock != overworldClock {
		return 0, nil
	}
	candidates := make([]string, 0, len(timeMarkers))
	for name := range timeMarkers {
		candidates = append(candidates, name)
	}
	return 0, ResourceSuggestions(partial, candidates...)
}

// intFeedback is a feedback argument carrying a whole number as an int tag,
// /time set's own argument's type.
func intFeedback(v int64) protocol.Text { return protocol.Text{Number: &v} }

// longFeedback is the wider whole number the queries answer with.
func longFeedback(v int64) protocol.Text { return protocol.Text{Long: &v} }

// floatFeedback is the shape the rate rides as.
func floatFeedback(v float32) protocol.Text { return protocol.Text{Float: &v} }

// timeFeedback sends the /time command's translated feedback naming the
// clock it ran against, its name first and the value second, the value
// carried in the typed shape the command answers in. logged is vanilla's
// sendSuccess allowLogging, which a change sets and a query leaves off.
func timeFeedback(src Source, logged bool, key string, clock string, value protocol.Text) error {
	if logged {
		return feedbackToAdmins(src, key, protocol.Text{Text: clock}, value)
	}
	return src.Feedback(key, protocol.Text{Text: clock}, value)
}

// resolveTimeMarker looks up a time marker by name, accepting both the short
// form ("noon") and the full namespaced form ("minecraft:noon").
func resolveTimeMarker(name string) (int64, bool) {
	if ticks, ok := timeMarkers[name]; ok {
		return ticks, true
	}
	if ticks, ok := timeMarkers["minecraft:"+name]; ok {
		return ticks, true
	}
	return 0, false
}

// markerName returns the display name of a time marker, normalized to the
// full namespaced form the vanilla server uses in its feedback.
func markerName(name string) string {
	return registry.Qualify(name)
}
