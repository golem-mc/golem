package command

import (
	"fmt"
	"math"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/scoreboard"
)

// Waypoint is the /waypoint command.
var Waypoint = Command{
	Name:   "waypoint",
	Level:  PermLevelGamemasters,
	Build:  waypointNode,
	Run:    discardResult(runWaypoint),
	Result: runWaypoint,
}

// ParserTeamColor and ParserHexColor are minecraft:team_color and
// minecraft:hex_color.
const (
	ParserTeamColor = 16
	ParserHexColor  = 17
)

const (
	waypointInvalidKey  = "argument.waypoint.invalid"
	waypointColorKey    = "argument.color.invalid"
	waypointHexKey      = "argument.hexcolor.invalid"
	waypointListEmpty   = "commands.waypoint.list.empty"
	waypointListSuccess = "commands.waypoint.list.success"
)

// WaypointTarget is an entity that carries a locator-bar waypoint.
type WaypointTarget interface {
	Entity

	// WaypointIcon is the icon the waypoint is drawn with.
	WaypointIcon() player.LocatorIcon

	// SetWaypointIcon changes the icon and re-tracks the waypoint for the
	// viewers holding it.
	SetWaypointIcon(icon player.LocatorIcon)

	// CarriesWaypoint says whether the entity has a waypoint to edit at all.
	CarriesWaypoint() bool

	// TransmitsWaypoint says whether the entity is on the world's list of
	// waypoints.
	TransmitsWaypoint() bool
}

func teamColorByName(name string) (int32, bool) {
	i, ok := scoreboard.LookupTeamColor(name)
	if !ok {
		return 0, false
	}
	return scoreboard.TeamColorRGB(i), true
}

// parseHexColor reads FF0 or FF0000 as an opaque ARGB colour.
// vanilla: HexColorArgument.parse
func parseHexColor(word string) (int32, bool) {
	if len(word) != 3 && len(word) != 6 {
		return 0, false
	}
	var digits [6]int32
	for i := range len(word) {
		c := word[i]
		var d int32
		switch {
		case c >= '0' && c <= '9':
			d = int32(c - '0')
		case c >= 'a' && c <= 'f':
			d = int32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			d = int32(c-'A') + 10
		default:
			return 0, false
		}
		digits[i] = d
	}
	var r, g, b int32
	if len(word) == 3 {
		r, g, b = digits[0]*17, digits[1]*17, digits[2]*17
	} else {
		r, g, b = digits[0]<<4|digits[1], digits[2]<<4|digits[3], digits[4]<<4|digits[5]
	}
	return int32(-1<<24) | r<<16 | g<<8 | b, true
}

func checkTeamColorArg(word string) *ParseError {
	if _, ok := teamColorByName(word); ok {
		return nil
	}
	return &ParseError{Key: waypointColorKey, With: []string{word}, Cursor: len(word)}
}

func checkHexColorArg(word string) *ParseError {
	if _, ok := parseHexColor(word); ok {
		return nil
	}
	return &ParseError{Key: waypointHexKey, With: []string{word}, Cursor: len(word)}
}

// checkIdentifierArg refuses a word that is no resource location.
// vanilla: IdentifierArgument.parse
func checkIdentifierArg(word string) *ParseError {
	if _, n, ok := readIdentifier(word); !ok || n != len(word) {
		return &ParseError{Key: "argument.id.invalid", Cursor: 0}
	}
	return nil
}

// waypointNode builds the /waypoint tree, matching the vanilla report.
func waypointNode() *Node {
	lit := func(name string, executable bool, children ...*Node) *Node {
		return &Node{Type: NodeLiteral, Name: name, Executable: executable, Children: children}
	}
	color := &Node{
		Type: NodeArgument, Name: "color", Executable: true, Parser: ParserTeamColor,
		Check:    checkTeamColorArg,
		Suggests: AskServerSuggestion,
		Completions: func(_ any, partial string) (int, []string) {
			var names []string
			for _, name := range scoreboard.TeamColors() {
				if strings.HasPrefix(name, partial) {
					names = append(names, name)
				}
			}
			return 0, names
		},
	}
	hex := &Node{
		Type: NodeArgument, Name: "color", Executable: true, Parser: ParserHexColor,
		Check: checkHexColorArg,
	}
	style := &Node{
		Type: NodeArgument, Name: "style", Executable: true, Parser: ParserResourceLoc,
		Check: checkIdentifierArg,
	}
	target := &Node{
		Type:       NodeArgument,
		Name:       "waypoint",
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x01) }, // one entity
		EntityArg:  &EntityArg{Single: true},
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
		Children: []*Node{
			lit("color", false, color, lit("hex", false, hex), lit("reset", true)),
			lit("style", false, lit("reset", true), lit("set", false, style)),
		},
	}
	return &Node{
		Type: NodeLiteral, Name: "waypoint", Required: PermLevelGamemasters,
		Children: []*Node{lit("list", true), lit("modify", false, target)},
	}
}

// runWaypoint runs /waypoint list and /waypoint modify, answering the
// number of waypoints a list names and 0 for a modify.
// vanilla: WaypointCommand
func runWaypoint(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		return 0, nil
	}
	if args[0] == "list" {
		return waypointList(src)
	}
	if args[0] != "modify" {
		return 0, nil
	}
	word, rest := splitTargets(input, args[1:])
	if word == "" || len(rest) == 0 {
		return 0, nil
	}
	found, err := resolveEntities(src, word)
	if err != nil {
		return 0, err
	}
	target, ok := found[0].(WaypointTarget)
	if !ok || !target.CarriesWaypoint() {
		return 0, refuse(src, waypointInvalidKey)
	}
	icon := target.WaypointIcon()
	var feedback protocol.Text
	switch rest[0] {
	case "color":
		switch {
		case len(rest) == 2 && rest[1] == "reset":
			icon.Color, icon.HasColor = 0, false
			feedback = protocol.Text{Translate: "commands.waypoint.modify.color.reset"}
		case len(rest) == 3 && rest[1] == "hex":
			rgb, _ := parseHexColor(rest[2])
			icon.Color, icon.HasColor = rgb, true
			feedback = protocol.Text{Translate: "commands.waypoint.modify.color", With: []protocol.Text{{
				Text: fmt.Sprintf("%06X", rgb&0xFFFFFF), Color: fmt.Sprintf("#%06x", rgb&0xFFFFFF),
			}}}
		case len(rest) == 2:
			rgb, _ := teamColorByName(rest[1])
			icon.Color, icon.HasColor = rgb, true
			feedback = protocol.Text{Translate: "commands.waypoint.modify.color", With: []protocol.Text{{
				Text: rest[1], Color: rest[1],
			}}}
		default:
			return 0, nil
		}
	case "style":
		switch {
		case len(rest) == 2 && rest[1] == "reset":
			icon.Style = player.DefaultWaypointStyle
		case len(rest) == 3 && rest[1] == "set":
			name, _ := parseResourceLocation(rest[2])
			icon.Style = name
		default:
			return 0, nil
		}
		feedback = protocol.Text{Translate: "commands.waypoint.modify.style"}
	default:
		return 0, nil
	}
	target.SetWaypointIcon(icon)
	return 0, src.Feedback(feedback.Translate, feedback.With...)
}

// waypointList names the waypoints the source's dimension tracks.
// vanilla: WaypointCommand.listWaypoints
func waypointList(src Source) (int32, error) {
	dim := src.DimensionName()
	var names []protocol.Text
	var transmitters []WaypointTarget
	// Entities covers players and mobs; Accept is not applied to players, so
	// the check runs on the result.
	for _, e := range src.Entities(EntityQuery{Dimension: dim}) {
		if wt, ok := e.(WaypointTarget); ok && wt.CarriesWaypoint() && wt.TransmitsWaypoint() {
			transmitters = append(transmitters, wt)
		}
	}
	for _, wt := range transmitters {
		pos := wt.Position()
		color := "#ffffff"
		if icon := wt.WaypointIcon(); icon.HasColor {
			color = fmt.Sprintf("#%06x", icon.Color&0xFFFFFF)
		}
		hover := protocol.Text{Translate: "chat.coordinates.tooltip"}
		names = append(names, protocol.Text{
			Text:      wt.PlainName(),
			Insertion: wt.PlainName(),
			Color:     color,
			ClickCommand: fmt.Sprintf("/execute in %s run tp @s %d %d %d",
				dim, int(math.Floor(pos.X)), int(math.Floor(pos.Y)), int(math.Floor(pos.Z))),
			HoverText: &hover,
		})
	}
	dimension := protocol.Text{Text: dim}
	if len(names) == 0 {
		return 0, src.Feedback(waypointListEmpty, dimension)
	}
	count := int64(len(names))
	return int32(len(names)), src.Feedback(waypointListSuccess,
		protocol.Text{Number: &count}, dimension, formatList(names))
}
