package command

import (
	"strconv"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/metrics"
)

// Metrics is /metrics, not a vanilla command: spark's tick-duration and CPU
// lines, then the Go runtime's memory, collector and goroutines.
var Metrics = Command{
	Name:  "metrics",
	Level: PermLevelModerators,
	Build: func() *Node {
		return &Node{Type: NodeLiteral, Name: "metrics", Executable: true, Required: PermLevelModerators}
	},
	Run: runMetrics,
}

// runMetrics answers with the report. The tick and CPU lines follow spark's
// /spark tps (spark: HealthModule, StatisticFormatter); the windows are
// spark's, measured by time as Paper's are.
func runMetrics(src Source, input string, args []string) error {
	m := src.Metrics()
	now := time.Now()
	gold := func(s string) protocol.Text { return protocol.Text{Text: s, Color: "gold"} }
	slash := protocol.Text{Text: "/", Color: "gray"}

	durations := protocol.Text{Text: " "}
	for i, w := range []time.Duration{10 * time.Second, time.Minute} {
		if i > 0 {
			durations.Extra = append(durations.Extra, protocol.Text{Text: ";  "})
		}
		d := m.Durations(w)
		target := float64(m.Target().Nanoseconds())
		durations.Extra = append(durations.Extra,
			durationText(d, 0, target), slash,
			durationText(d, 0.5, target), slash,
			durationText(d, 0.95, target), slash,
			durationText(d, 1, target))
	}

	cpu := protocol.Text{Text: " "}
	for i, w := range []time.Duration{10 * time.Second, time.Minute, 15 * time.Minute} {
		if i > 0 {
			cpu.Extra = append(cpu.Extra, protocol.Text{Text: ", "})
		}
		cpu.Extra = append(cpu.Extra, cpuText(m.ProcessCPU(w, now)))
	}
	cpu.Extra = append(cpu.Extra, protocol.Text{Text: "  (process)", Color: "dark_gray"})

	mem := metrics.ReadMemory()
	lines := []protocol.Text{
		gold("Tick durations (min/med/95%ile/max ms) from last 10s, 1m:"),
		durations,
		gold("CPU usage from last 10s, 1m, 15m:"),
		cpu,
		{Extra: []protocol.Text{gold("Memory: "), {Text: mib(mem.HeapInUse) + " heap in use, " +
			mib(mem.HeapGoal) + " heap goal, " + mib(mem.Total) + " from the OS"}}},
		{Extra: []protocol.Text{gold("GC: "), {Text: strconv.FormatUint(mem.GCCycles, 10) + " cycles, pauses median " +
			millis(mem.GCPauseP50) + " max " + millis(mem.GCPauseMax) + ", " +
			strconv.FormatFloat(mem.GCCPU*100, 'f', 1, 64) + "% of CPU"}}},
		{Extra: []protocol.Text{gold("Goroutines: "), {Text: strconv.FormatUint(mem.Goroutines, 10)}}},
	}
	for _, line := range lines {
		if err := src.Message(line); err != nil {
			return err
		}
	}
	return nil
}

// durationText is the p-th percentile of the sorted durations in
// milliseconds, coloured against the target the way spark colours it.
func durationText(sorted []int64, p, target float64) protocol.Text {
	v := float64(metrics.Percentile(sorted, p))
	color := "green"
	switch {
	case v >= target:
		color = "red"
	case v >= target*0.8:
		color = "yellow"
	}
	return protocol.Text{Text: javaOneDecimal(v / 1e6), Color: color}
}

func cpuText(share float64, ok bool) protocol.Text {
	if !ok {
		return protocol.Text{Text: "n/a", Color: "gray"}
	}
	color := "green"
	switch {
	case share > 0.9:
		color = "red"
	case share > 0.65:
		color = "yellow"
	}
	return protocol.Text{Text: strconv.FormatFloat(share*100, 'f', 1, 64) + "%", Color: color}
}

func mib(b uint64) string {
	return strconv.FormatFloat(float64(b)/(1<<20), 'f', 1, 64) + " MiB"
}

func millis(seconds float64) string {
	return strconv.FormatFloat(seconds*1e3, 'f', 3, 64) + " ms"
}
