package command

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// Weather is the /weather command.
var Weather = Command{
	Name:   "weather",
	Level:  PermLevelGamemasters,
	Build:  weatherNode,
	Run:    discardResult(runWeather),
	Result: runWeather,
}

// The translation keys the /weather command answers with, one per kind of
// weather it can set.
const (
	weatherSetClearKey   = "commands.weather.set.clear"
	weatherSetRainKey    = "commands.weather.set.rain"
	weatherSetThunderKey = "commands.weather.set.thunder"
)

// weatherNode builds the /weather command tree, matching the vanilla
// report: a clear, rain or thunder literal, each of which may be
// followed by a duration in ticks — a minecraft:time argument of at least
// one tick, which the walk refuses below its minimum.
func weatherNode() *Node {
	node := &Node{
		Type:     NodeLiteral,
		Name:     "weather",
		Required: PermLevelGamemasters,
	}
	for _, kind := range []string{"clear", "rain", "thunder"} {
		kindNode := &Node{
			Type:       NodeLiteral,
			Name:       kind,
			Executable: true,
			Children: []*Node{
				{
					Type:       NodeArgument,
					Name:       "duration",
					Executable: true,
					Parser:     ParserTime,
					Check:      checkTimeMin(1),
					Properties: func(b *protocol.Buffer) { b.PutI32(1) },
				},
			},
		}
		node.Children = append(node.Children, kindNode)
	}
	return node
}

// runWeather runs /weather <clear|rain|thunder> [duration], setting only the
// flags and timers (the tick fades the sky and tells players); without a
// duration the timer is rolled as vanilla rolls it.
// vanilla: WeatherCommand
func runWeather(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete weather command")
		return 0, nil
	}
	kind := args[0]

	// The duration is a minecraft:time argument like /time takes, so "6000"
	// and "5m"-style values both parse; the walk has already refused one
	// below the argument's minimum 1.
	var duration int32
	if len(args) > 1 {
		ticks, ok := ParseTimeArg(args[1])
		if !ok {
			src.Debug("invalid weather duration", "duration", args[1])
			return 0, nil
		}
		duration = int32(ticks)
	}
	orRoll := func(dice world.Dice) int32 {
		if duration > 0 {
			return duration
		}
		// vanilla: source.getLevel().getRandom()
		return src.RollDice(dice)
	}

	var key string
	switch kind {
	case "clear":
		key = weatherSetClearKey
		src.SetWeather(false, false, 0, 0, orRoll(world.RainDelay))
	case "rain":
		key = weatherSetRainKey
		t := orRoll(world.RainDuration)
		src.SetWeather(true, false, t, t, 0)
	case "thunder":
		key = weatherSetThunderKey
		t := orRoll(world.ThunderDuration)
		src.SetWeather(true, true, t, t, 0)
	default:
		src.Debug("unknown weather", "weather", kind)
		return 0, nil
	}
	if err := feedbackToAdmins(src, key); err != nil {
		return 0, err
	}
	src.Info("weather set", "weather", kind, "duration", int64(duration), "by", src.Name())
	// The typed duration, -1 where none was given.
	// vanilla: WeatherCommand.setClear
	if len(args) < 2 {
		return -1, nil
	}
	return duration, nil
}
