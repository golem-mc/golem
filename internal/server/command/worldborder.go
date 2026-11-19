package command

import (
	"fmt"
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// Worldborder is the /worldborder command.
var Worldborder = Command{
	Name:   "worldborder",
	Level:  PermLevelGamemasters,
	Build:  worldborderNode,
	Run:    discardResult(runWorldborder),
	Result: runWorldborder,
}

// WorldBorder is the border of the dimension the runner stands in. Each
// setter changes it and tells the players in that dimension.
// vanilla: WorldBorder
type WorldBorder interface {
	State() world.BorderState
	// DamageSettings are the damage per block and the buffer, which no
	// packet carries.
	DamageSettings() (perBlock, buffer float64)
	SetCenter(x, z float64)
	SetSize(d float64)
	// LerpSize moves the side from one length to another over ticks.
	LerpSize(from, to float64, ticks int64)
	SetDamagePerBlock(d float64)
	SetSafeZone(d float64)
	SetWarningBlocks(n int32)
	// SetWarningTime takes ticks.
	SetWarningTime(n int32)
}

const (
	// vanilla: WorldBorder.MAX_SIZE, WorldBorder.MAX_CENTER_COORDINATE as
	// Java prints them.
	worldborderMaxSizeText   = "5.9999968E7"
	worldborderMaxCenterText = "2.9999984E7"

	keyBorderCenterFailed   = "commands.worldborder.center.failed"
	keyBorderNoChange       = "commands.worldborder.set.failed.nochange"
	keyBorderTooSmall       = "commands.worldborder.set.failed.small"
	keyBorderTooBig         = "commands.worldborder.set.failed.big"
	keyBorderTooFar         = "commands.worldborder.set.failed.far"
	keyBorderWarnTimeFailed = "commands.worldborder.warning.time.failed"
	keyBorderWarnDistFailed = "commands.worldborder.warning.distance.failed"
	keyBorderBufferFailed   = "commands.worldborder.damage.buffer.failed"
	keyBorderAmountFailed   = "commands.worldborder.damage.amount.failed"
)

// worldborderNode builds the /worldborder command tree, matching the vanilla
// report.
func worldborderNode() *Node {
	timeArg := func() *Node {
		return &Node{
			Type:       NodeArgument,
			Name:       "time",
			Executable: true,
			Parser:     ParserTime,
			Check:      checkTimeMin(0),
			Properties: func(b *protocol.Buffer) { b.PutI32(0) },
		}
	}
	distance := func() *Node {
		return &Node{
			Type:       NodeArgument,
			Name:       "distance",
			Executable: true,
			Parser:     ParserDouble,
			Check:      checkBorderDistance,
			Properties: func(b *protocol.Buffer) {
				b.PutU8(0x03)
				b.PutF64(-world.BorderMaxSize)
				b.PutF64(world.BorderMaxSize)
			},
			Children: []*Node{timeArg()},
		}
	}
	floatArg := func(name string) *Node {
		return &Node{
			Type:       NodeArgument,
			Name:       name,
			Executable: true,
			Parser:     ParserFloat,
			Float:      &FloatArg{Min: 0, HasMin: true},
		}
	}
	lit := func(name string, executable bool, children ...*Node) *Node {
		return &Node{Type: NodeLiteral, Name: name, Executable: executable, Children: children}
	}
	return &Node{
		Type:     NodeLiteral,
		Name:     "worldborder",
		Required: PermLevelGamemasters,
		Children: []*Node{
			lit("add", false, distance()),
			lit("set", false, distance()),
			lit("center", false, &Node{Type: NodeArgument, Name: "pos", Executable: true, Parser: ParserVec2}),
			lit("damage", false,
				lit("amount", false, floatArg("damagePerBlock")),
				lit("buffer", false, floatArg("distance"))),
			lit("get", true),
			lit("warning", false,
				lit("distance", false, &Node{
					Type: NodeArgument, Name: "distance", Executable: true,
					Parser: ParserInteger, Int: &IntArg{Min: 0, HasMin: true},
				}),
				lit("time", false, timeArg())),
		},
	}
}

// runWorldborder runs /worldborder against the runner's dimension's border.
// vanilla: WorldBorderCommand
func runWorldborder(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		src.Debug("incomplete worldborder command")
		return 0, nil
	}
	border := src.WorldBorder()
	rest := args[1:]
	switch args[0] {
	case "get":
		// Java's %.0f rounds an exact half up.
		size := math.Floor(border.State().Size + 0.5)
		return resultOf(javaDoubleToInt(size), src.Feedback("commands.worldborder.get", text(javaFixed(size, 0))))
	case "set", "add":
		if len(rest) == 0 {
			src.Debug("incomplete worldborder size command")
			return 0, nil
		}
		d, perr := readDoubleWord(rest[0])
		if perr != nil {
			src.Debug("worldborder distance did not parse", "arg", rest[0])
			return 0, nil
		}
		var ticks int64
		if len(rest) > 1 {
			var ok bool
			if ticks, ok = ParseTimeArg(rest[1]); !ok {
				src.Debug("worldborder time did not parse", "arg", rest[1])
				return 0, nil
			}
		}
		current := border.State()
		if args[0] == "add" {
			d += current.Size
			// An added time, even zero, stretches the lerp already running.
			// vanilla: WorldBorderCommand.register
			if len(rest) > 1 {
				ticks += current.LerpTicks
			}
		}
		return worldborderSetSize(src, border, d, ticks)
	case "center":
		if len(rest) < 2 {
			src.Debug("incomplete worldborder center command")
			return 0, nil
		}
		cx, cz, ok := readVec2(rest[:2])
		if !ok {
			src.Debug("worldborder centre did not parse", "args", rest[:2])
			return 0, nil
		}
		at := src.Position()
		// The vec2 argument is read as floats.
		x, z := float64(float32(cx.resolve(at.X))), float64(float32(cz.resolve(at.Z)))
		st := border.State()
		if st.CenterX == x && st.CenterZ == z {
			return 0, refuse(src, keyBorderCenterFailed)
		}
		if math.Abs(x) > world.BorderMaxCenter || math.Abs(z) > world.BorderMaxCenter {
			return 0, refuse(src, keyBorderTooFar, text(worldborderMaxCenterText))
		}
		border.SetCenter(x, z)
		return resultOf(0, feedbackToAdmins(src, "commands.worldborder.center.success",
			text(javaFixed(x, 2)), text(javaFixed(z, 2))))
	case "damage":
		return worldborderDamage(src, border, rest)
	case "warning":
		return worldborderWarning(src, border, rest)
	}
	src.Debug("unknown worldborder subcommand", "arg", args[0])
	return 0, nil
}

func text(s string) protocol.Text { return protocol.Text{Text: s} }

// worldborderSetSize sets the border to distance at once, or over ticks.
// vanilla: WorldBorderCommand.setSize
func worldborderSetSize(src Source, border WorldBorder, distance float64, ticks int64) (int32, error) {
	current := border.State().Size
	switch {
	case current == distance:
		return 0, refuse(src, keyBorderNoChange)
	case distance < 1:
		return 0, refuse(src, keyBorderTooSmall)
	case distance > world.BorderMaxSize:
		return 0, refuse(src, keyBorderTooBig, text(worldborderMaxSizeText))
	}
	formatted := text(javaFixed(distance, 1))
	result := javaDoubleToInt(distance - current)
	if ticks <= 0 {
		border.SetSize(distance)
		return resultOf(result, feedbackToAdmins(src, "commands.worldborder.set.immediate", formatted))
	}
	border.LerpSize(current, distance, ticks)
	key := "commands.worldborder.set.shrink"
	if distance > current {
		key = "commands.worldborder.set.grow"
	}
	return resultOf(result, feedbackToAdmins(src, key, formatted, text(javaFixed(float64(ticks)/20, 2))))
}

// worldborderDamage runs /worldborder damage <amount|buffer> <value>.
// vanilla: WorldBorderCommand.setDamageAmount, setDamageBuffer
func worldborderDamage(src Source, border WorldBorder, rest []string) (int32, error) {
	if len(rest) < 2 {
		src.Debug("incomplete worldborder damage command")
		return 0, nil
	}
	v, perr := readDoubleWord(rest[1])
	if perr != nil || v < 0 {
		src.Debug("worldborder damage value did not parse", "arg", rest[1])
		return 0, nil
	}
	// The argument is a float, which the border holds widened.
	v = float64(float32(v))
	perBlock, buffer := border.DamageSettings()
	formatted := text(javaFixed(v, 2))
	switch rest[0] {
	case "amount":
		if perBlock == v {
			return 0, refuse(src, keyBorderAmountFailed)
		}
		border.SetDamagePerBlock(v)
		return resultOf(javaDoubleToInt(v), feedbackToAdmins(src, "commands.worldborder.damage.amount.success", formatted))
	case "buffer":
		if buffer == v {
			return 0, refuse(src, keyBorderBufferFailed)
		}
		border.SetSafeZone(v)
		return resultOf(javaDoubleToInt(v), feedbackToAdmins(src, "commands.worldborder.damage.buffer.success", formatted))
	}
	src.Debug("unknown worldborder damage setting", "arg", rest[0])
	return 0, nil
}

// worldborderWarning runs /worldborder warning <distance|time> <value>.
// vanilla: WorldBorderCommand.setWarningDistance, setWarningTime
func worldborderWarning(src Source, border WorldBorder, rest []string) (int32, error) {
	if len(rest) < 2 {
		src.Debug("incomplete worldborder warning command")
		return 0, nil
	}
	st := border.State()
	switch rest[0] {
	case "distance":
		var n int32
		if _, err := fmt.Sscanf(rest[1], "%d", &n); err != nil || n < 0 {
			src.Debug("worldborder warning distance did not parse", "arg", rest[1])
			return 0, nil
		}
		if st.WarningBlocks == n {
			return 0, refuse(src, keyBorderWarnDistFailed)
		}
		border.SetWarningBlocks(n)
		return resultOf(n, feedbackToAdmins(src, "commands.worldborder.warning.distance.success", text(formatInt(int64(n)))))
	case "time":
		ticks, ok := ParseTimeArg(rest[1])
		if !ok || ticks < 0 {
			src.Debug("worldborder warning time did not parse", "arg", rest[1])
			return 0, nil
		}
		if int64(st.WarningTime) == ticks {
			return 0, refuse(src, keyBorderWarnTimeFailed)
		}
		border.SetWarningTime(int32(ticks))
		return resultOf(int32(ticks), feedbackToAdmins(src, "commands.worldborder.warning.time.success",
			text(javaFixed(float64(ticks)/20, 2))))
	}
	src.Debug("unknown worldborder warning setting", "arg", rest[0])
	return 0, nil
}

// checkBorderDistance refuses a distance that is no double or lies outside
// the bounds the argument declares.
// vanilla: DoubleArgumentType.parse
func checkBorderDistance(word string) *ParseError {
	v, err := readDoubleWord(word)
	if err != nil {
		return err
	}
	switch {
	case v < -world.BorderMaxSize:
		return &ParseError{Key: keyDoubleLow, With: []string{formatJavaDouble(-world.BorderMaxSize), formatJavaDouble(v)}}
	case v > world.BorderMaxSize:
		return &ParseError{Key: keyDoubleBig, With: []string{formatJavaDouble(world.BorderMaxSize), formatJavaDouble(v)}}
	}
	return nil
}
