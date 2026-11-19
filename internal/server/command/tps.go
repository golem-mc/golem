package command

import (
	"strconv"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// Tps is Paper's /tps, not a vanilla command: the ticks per second over the
// last 1, 5 and 15 minutes.
var Tps = Command{
	Name:  "tps",
	Level: PermLevelModerators,
	Build: func() *Node {
		return &Node{Type: NodeLiteral, Name: "tps", Executable: true, Required: PermLevelModerators}
	},
	Run: runTps,
}

// runTps answers with Paper's line, the same components in the same colours.
// Paper: org.spigotmc.TicksPerSecondCommand
func runTps(src Source, input string, args []string) error {
	m := src.Metrics()
	values := protocol.Text{}
	for i, w := range []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute} {
		if i > 0 {
			values.Extra = append(values.Extra, protocol.Text{Text: ", "})
		}
		values.Extra = append(values.Extra, tpsText(m.TPS(w)))
	}
	return src.Message(protocol.Text{Extra: []protocol.Text{
		{Text: "TPS from last 1m, 5m, 15m: ", Color: "gold"},
		values,
	}})
}

// tpsText is one TPS figure: DecimalFormat("########0.0"), which rounds half
// even as Go does, green above 18 and yellow above 16.
func tpsText(tps float64) protocol.Text {
	color := "red"
	switch {
	case tps > 18:
		color = "green"
	case tps > 16:
		color = "yellow"
	}
	return protocol.Text{Text: strconv.FormatFloat(tps, 'f', 1, 64), Color: color}
}
