package command

import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/metrics"
)

// PerfCommand is the /perf command: a 10-second CPU and tick-time profile
// saved as a zip under debug/profiling.
// vanilla: PerfCommand (dedicated servers only)
var PerfCommand = Command{
	Name:  "perf",
	Level: PermLevelOwners,
	Build: perfNode,
	Run:   runPerf,
}

// The translation keys /perf answers with.
const (
	perfStartedKey        = "commands.perf.started"
	perfStoppedKey        = "commands.perf.stopped"
	perfNotRunningKey     = "commands.perf.notRunning"
	perfAlreadyRunningKey = "commands.perf.alreadyRunning"
	perfReportSavedKey    = "commands.perf.reportSaved"
	perfReportFailedKey   = "commands.perf.reportFailed"
	perfTimeLayout        = "2006-01-02_15.04.05"
)

// perfNode builds the /perf tree.
// vanilla: PerfCommand.register
func perfNode() *Node {
	return &Node{Type: NodeLiteral, Name: "perf", Required: PermLevelOwners, Children: []*Node{
		{Type: NodeLiteral, Name: "start", Executable: true},
		{Type: NodeLiteral, Name: "stop", Executable: true},
	}}
}

func runPerf(src Source, input string, args []string) error {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "start":
		started, busy := src.Metrics().StartPerf(time.Now(), func() { _ = finishPerf(src, false) })
		if busy {
			return refuse(src, perfReportFailedKey)
		}
		if !started {
			return refuse(src, perfAlreadyRunningKey)
		}
		return feedbackToAdmins(src, perfStartedKey)
	case "stop":
		return finishPerf(src, true)
	}
	return nil
}

// finishPerf ends the run, tells the admins and saves the report. The
// automatic stop has no command to answer, so it only informs.
// vanilla: PerfCommand.stopProfilingRun
func finishPerf(src Source, command bool) error {
	rep, ok := src.Metrics().StopPerf(time.Now())
	if !ok {
		if command {
			return refuse(src, perfNotRunningKey)
		}
		return nil
	}
	seconds := rep.Elapsed.Seconds()
	tps := float64(rep.Ticks) / seconds
	n := rep.Ticks
	stopped := []protocol.Text{{Text: javaFixed(seconds, 2)}, {Number: &n}, {Text: javaFixed(tps, 2)}}
	say := func(key string, with ...protocol.Text) error {
		if command {
			return feedbackToAdmins(src, key, with...)
		}
		src.InformAdmins(protocol.Text{Translate: key, With: with})
		return nil
	}
	if err := say(perfStoppedKey, stopped...); err != nil {
		return err
	}
	name := "perf-" + time.Now().Format(perfTimeLayout) + ".zip"
	if err := writePerfReport(filepath.Join(src.ServerDir(), "debug", "profiling"), name, rep); err != nil {
		src.Debug("perf report failed", "error", err)
		if command {
			return refuse(src, perfReportFailedKey)
		}
		src.InformAdmins(protocol.Text{Translate: perfReportFailedKey})
		return nil
	}
	if err := say(perfReportSavedKey, protocol.Text{Text: filepath.Join("debug", "profiling", name)}); err != nil {
		return err
	}
	if command {
		reportResult(src, true, int32(tps))
	}
	return nil
}

// writePerfReport writes dir/name: summary.txt with the tick times and
// cpu.pprof with the window's CPU profile.
func writePerfReport(dir, name string, rep metrics.PerfReport) (err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	zw := zip.NewWriter(f)
	var sum strings.Builder
	var total, worst int64
	for _, s := range rep.Samples {
		total += s
		worst = max(worst, s)
	}
	fmt.Fprintf(&sum, "duration: %.2f s\nticks: %d\nticks per second: %.2f\n",
		rep.Elapsed.Seconds(), rep.Ticks, float64(rep.Ticks)/rep.Elapsed.Seconds())
	if n := int64(len(rep.Samples)); n > 0 {
		fmt.Fprintf(&sum, "average tick: %.3f ms\nworst tick: %.3f ms\n", float64(total)/float64(n)/1e6, float64(worst)/1e6)
	}
	for _, e := range []struct {
		name string
		body []byte
	}{{"summary.txt", []byte(sum.String())}, {"cpu.pprof", rep.CPUProfile}} {
		w, werr := zw.Create(e.name)
		if werr != nil {
			return werr
		}
		if _, werr = w.Write(e.body); werr != nil {
			return werr
		}
	}
	return zw.Close()
}
