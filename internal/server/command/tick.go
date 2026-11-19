package command

import (
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/metrics"
)

// Tick is the /tick command: query, rate, freeze, unfreeze, step and
// sprint, turning the main loop's ticking state.
var Tick = Command{
	Name:   "tick",
	Level:  PermLevelAdmins,
	Build:  tickNode,
	Run:    discardResult(runTick),
	Result: runTick,
}

// Translation keys for the /tick command's output, as the vanilla client's
// language files know them.
const (
	tickStatusRunningKey     = "commands.tick.status.running"
	tickStatusLaggingKey     = "commands.tick.status.lagging"
	tickStatusFrozenKey      = "commands.tick.status.frozen"
	tickStatusSprintingKey   = "commands.tick.status.sprinting"
	tickRateRunningKey       = "commands.tick.query.rate.running"
	tickRateSprintingKey     = "commands.tick.query.rate.sprinting"
	tickPercentilesKey       = "commands.tick.query.percentiles"
	tickRateSuccessKey       = "commands.tick.rate.success"
	tickStepSuccessKey       = "commands.tick.step.success"
	tickStepFailKey          = "commands.tick.step.fail"
	tickStepStopSuccessKey   = "commands.tick.step.stop.success"
	tickStepStopFailKey      = "commands.tick.step.stop.fail"
	tickSprintStopSuccessKey = "commands.tick.sprint.stop.success"
	tickSprintStopFailKey    = "commands.tick.sprint.stop.fail"
)

// tickNode builds the /tick command tree, matching the vanilla TickCommand
// registration: query, rate, step (bare and with a time, plus stop),
// sprint (with a time, plus stop), unfreeze and freeze.
func tickNode() *Node {
	// rate <rate>: a brigadier float, 1.0 to 10000.0, vanilla's
	// MAX_TICKRATE the ceiling.
	// vanilla: TickCommand.register
	rateArg := &Node{
		Type:        NodeArgument,
		Name:        "rate",
		Executable:  true,
		Parser:      ParserFloat,
		Float:       &FloatArg{Min: 1, Max: 10000, HasMin: true, HasMax: true},
		Suggests:    AskServerSuggestion,
		Completions: func(_ any, partial string) (int, []string) { return 0, Suggestions(partial, "20") },
	}

	// step [<time> | stop]: one step by default, a time argument's ticks
	// when given, a stop cancelling the steps left. The time argument is
	// shared with sprint, whose suggestions differ.
	// vanilla: TickCommand.register, SharedSuggestionProvider.suggest
	stepTimeArg := func(suggests ...string) *Node {
		return &Node{
			Type:        NodeArgument,
			Name:        "time",
			Executable:  true,
			Parser:      ParserTime,
			Check:       checkTimeMin(1),
			Properties:  func(b *protocol.Buffer) { b.PutI32(1) },
			Suggests:    AskServerSuggestion,
			Completions: func(_ any, partial string) (int, []string) { return 0, Suggestions(partial, suggests...) },
		}
	}
	stepNode := &Node{
		Type:       NodeLiteral,
		Name:       "step",
		Executable: true,
		Children: []*Node{
			{Type: NodeLiteral, Name: "stop", Executable: true},
			stepTimeArg("1t", "1s"),
		},
	}
	sprintNode := &Node{
		Type: NodeLiteral,
		Name: "sprint",
		Children: []*Node{
			{Type: NodeLiteral, Name: "stop", Executable: true},
			stepTimeArg("60s", "1d", "3d"),
		},
	}

	return &Node{
		Type:     NodeLiteral,
		Name:     "tick",
		Required: PermLevelAdmins,
		Children: []*Node{
			{Type: NodeLiteral, Name: "query", Executable: true},
			{Type: NodeLiteral, Name: "rate", Children: []*Node{rateArg}},
			stepNode,
			sprintNode,
			{Type: NodeLiteral, Name: "unfreeze", Executable: true},
			{Type: NodeLiteral, Name: "freeze", Executable: true},
		},
	}
}

// runTick runs one /tick subcommand, answering what vanilla's returns: the
// rate as an int for query and rate, 1 or 0 for freeze or unfreeze, else 1.
// vanilla: TickCommand
func runTick(src Source, _ string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete tick command")
		return 0, nil
	}
	switch args[0] {
	case "query":
		return tickQuery(src)
	case "rate":
		return tickRateSet(src, args[1:])
	case "step":
		return tickStep(src, args[1:])
	case "sprint":
		return tickSprint(src, args[1:])
	case "freeze":
		return resultOf(1, tickFreeze(src, true))
	case "unfreeze":
		return resultOf(0, tickFreeze(src, false))
	default:
		src.Debug("unknown tick subcommand", "sub", args[0])
		return 0, nil
	}
}

// tickFreeze holds the game in or lets it out of a freeze, answering with
// the status line the ticking state now carries.
// vanilla: TickCommand.setFreeze
func tickFreeze(src Source, freeze bool) error {
	src.TickRate().SetFrozen(freeze)
	status := tickStatusRunningKey
	if freeze {
		status = tickStatusFrozenKey
	}
	return feedbackToAdmins(src, status)
}

// tickRateSet aims the loop at the rate, answering with the rate as the
// one-decimal form the vanilla line carries.
// vanilla: TickCommand.setTickingRate
func tickRateSet(src Source, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete tick rate")
		return 0, nil
	}
	rate, err := strconv.ParseFloat(args[0], 32)
	if err != nil {
		src.Debug("invalid tick rate", "arg", args[0])
		return 0, nil
	}
	src.TickRate().SetRate(float32(rate))
	return resultOf(int32(float32(rate)), feedbackToAdmins(src, tickRateSuccessKey,
		protocol.Text{Text: javaOneDecimal(float64(float32(rate)))}))
}

// tickStep asks a frozen game to run a step, one tick by default. Steps the
// game still has are replaced, and a stop cancels them.
// vanilla: TickCommand.step, stopStepping
func tickStep(src Source, args []string) (int32, error) {
	if len(args) > 0 && args[0] == "stop" {
		if !src.TickRate().StopStep() {
			return 0, refuse(src, tickStepStopFailKey)
		}
		return resultOf(1, feedbackToAdmins(src, tickStepStopSuccessKey))
	}
	advance := int64(1)
	if len(args) > 0 {
		ticks, ok := ParseTimeArg(args[0])
		if !ok {
			src.Debug("invalid tick step", "arg", args[0])
			return 0, nil
		}
		advance = ticks
	}
	if !src.TickRate().Step(int(advance)) {
		return 0, refuse(src, tickStepFailKey)
	}
	return resultOf(1, feedbackToAdmins(src, tickStepSuccessKey, protocol.Text{Number: &advance}))
}

// tickSprint runs the time argument's ticks without pacing; stop cancels a
// running sprint. A second sprint replaces the first, whose stop line comes
// before the sprinting status.
// vanilla: TickCommand.sprint, stopSprinting
func tickSprint(src Source, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete tick sprint")
		return 0, nil
	}
	if args[0] == "stop" {
		if !src.TickRate().StopSprint() {
			return 0, refuse(src, tickSprintStopFailKey)
		}
		return resultOf(1, feedbackToAdmins(src, tickSprintStopSuccessKey))
	}
	ticks, ok := ParseTimeArg(args[0])
	if !ok {
		src.Debug("invalid tick sprint", "arg", args[0])
		return 0, nil
	}
	if src.TickRate().Sprint(int(ticks)) {
		if err := feedbackToAdmins(src, tickSprintStopSuccessKey); err != nil {
			return 0, err
		}
	}
	return resultOf(1, feedbackToAdmins(src, tickStatusSprintingKey))
}

// tickQuery answers with the status line, the rates and the percentiles of
// the last 100 tick durations, every number a String formatted with %.1f
// except the Int sample count.
// vanilla: TickCommand.tickQuery
func tickQuery(src Source) (int32, error) {
	m := src.Metrics()
	manager := src.TickRate()
	samples, average := m.TickTimes()
	target := manager.NanosecondsPerTick()
	busy := nanosToMillisString(average)
	rateString := javaOneDecimal(float64(manager.Rate()))

	if manager.Sprinting() {
		if err := src.Feedback(tickStatusSprintingKey); err != nil {
			return 0, err
		}
		if err := src.Feedback(tickRateSprintingKey,
			protocol.Text{Text: rateString}, protocol.Text{Text: busy}); err != nil {
			return 0, err
		}
	} else {
		status := tickStatusRunningKey
		switch {
		case manager.Frozen():
			status = tickStatusFrozenKey
		case target < average:
			status = tickStatusLaggingKey
		}
		if err := src.Feedback(status); err != nil {
			return 0, err
		}
		if err := src.Feedback(tickRateRunningKey,
			protocol.Text{Text: rateString}, protocol.Text{Text: busy},
			protocol.Text{Text: nanosToMillisString(target)}); err != nil {
			return 0, err
		}
	}

	sorted := samples[:]
	slices.Sort(sorted)
	p50, p95, p99 := metrics.VanillaPercentiles(sorted)
	n := int64(len(sorted))
	return resultOf(int32(manager.Rate()), src.Feedback(tickPercentilesKey,
		protocol.Text{Text: nanosToMillisString(p50)},
		protocol.Text{Text: nanosToMillisString(p95)},
		protocol.Text{Text: nanosToMillisString(p99)},
		protocol.Text{Number: &n}))
}

// nanosToMillisString is vanilla's TickCommand.nanosToMilisString: the
// nanoseconds as float milliseconds to one place.
func nanosToMillisString(nanos int64) string {
	return javaOneDecimal(float64(float32(nanos) / float32(1_000_000)))
}

// javaOneDecimal renders a double the way Java's String.format("%.1f") does:
// its shortest decimal digits rounded half up, which is not Go's
// round-half-even on the exact binary value. A Java float is widened first,
// so a float32 is passed as float64(v).
func javaOneDecimal(v float64) string {
	s := strconv.FormatFloat(v, 'f', -1, 64)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	whole, frac, _ := strings.Cut(s, ".")
	frac += "00"
	digits := []byte(whole + frac[:1])
	if frac[1] >= '5' {
		i := len(digits) - 1
		for ; i >= 0 && digits[i] == '9'; i-- {
			digits[i] = '0'
		}
		if i < 0 {
			digits = append([]byte{'1'}, digits...)
		} else {
			digits[i]++
		}
	}
	return sign + string(digits[:len(digits)-1]) + "." + string(digits[len(digits)-1:])
}
