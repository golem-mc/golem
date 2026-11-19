package command

import (
	"strconv"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/metrics"
)

// Mspt is Paper's /mspt, not a vanilla command: the average, least and
// greatest tick duration over the last 5 seconds, 10 seconds and minute.
var Mspt = Command{
	Name:  "mspt",
	Level: PermLevelModerators,
	Build: func() *Node {
		return &Node{Type: NodeLiteral, Name: "mspt", Executable: true, Required: PermLevelModerators}
	},
	Run: runMspt,
}

// runMspt answers with Paper's two lines, the same components in the same
// colours.
// Paper: io.papermc.paper.command.PaperMSPTCommand
func runMspt(src Source, input string, args []string) error {
	m := src.Metrics()
	gray := func(s string) protocol.Text { return protocol.Text{Text: s, Color: "gray"} }
	head := protocol.Text{Text: "Server tick times ", Color: "gold", Extra: []protocol.Text{
		{Color: "yellow", Extra: []protocol.Text{
			{Text: "("}, gray("avg"), {Text: "/"}, gray("min"), {Text: "/"}, gray("max"), {Text: ")"},
		}},
		{Text: " from last 5s"}, gray(","), {Text: " 10s"}, gray(","), {Text: " 1m"},
		{Text: ":", Color: "yellow"},
	}}
	if err := src.Message(head); err != nil {
		return err
	}

	times := protocol.Text{Color: "gray"}
	for i, w := range []time.Duration{5 * time.Second, 10 * time.Second, time.Minute} {
		if i > 0 {
			times.Extra = append(times.Extra, protocol.Text{Text: ", ", Color: "yellow"})
		}
		s := metrics.Summarize(m.Durations(w))
		times.Extra = append(times.Extra,
			msptText(s.Mean/1e6), protocol.Text{Text: "/"},
			msptText(float64(s.Min)/1e6), protocol.Text{Text: "/"},
			msptText(float64(s.Max)/1e6))
	}
	return src.Message(protocol.Text{Text: "◴ ", Color: "gold", Extra: []protocol.Text{times}})
}

// msptText is one duration in milliseconds: DecimalFormat("########0.0"),
// red from 50 and yellow from 40.
func msptText(ms float64) protocol.Text {
	color := "green"
	switch {
	case ms >= 50:
		color = "red"
	case ms >= 40:
		color = "yellow"
	}
	return protocol.Text{Text: strconv.FormatFloat(ms, 'f', 1, 64), Color: color}
}
