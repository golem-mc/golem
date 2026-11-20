package command

import (
	"errors"
	"os"
	"path/filepath"
	"runtime/trace"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// JfrCommand is the /jfr command: a recording written under debug/.
// Deliberate difference from vanilla: there is no JVM, so the recording is a
// runtime execution trace (debug/server-<time>.trace, read by `go tool trace`)
// instead of a JFR file.
// vanilla: JfrCommand (dedicated servers only)
var JfrCommand = Command{
	Name:  "jfr",
	Level: PermLevelOwners,
	Build: jfrNode,
	Run:   runJfr,
}

// The translation keys /jfr answers with.
const (
	jfrStartedKey     = "commands.jfr.started"
	jfrStartFailedKey = "commands.jfr.start.failed"
	jfrStoppedKey     = "commands.jfr.stopped"
	jfrDumpFailedKey  = "commands.jfr.dump.failed"
	jfrTimeLayout     = "2006-01-02-150405"
)

// errJfrNotRunning is vanilla's message for a stop with no recording.
var errJfrNotRunning = errors.New("Not currently profiling") //nolint:staticcheck // vanilla text

// jfrRun is the trace in progress. A process has one trace, so the recording
// is process-wide; dir says which server started it.
var jfrRun struct {
	sync.Mutex
	file *os.File
	dir  string
}

// jfrNode builds the /jfr tree.
// vanilla: JfrCommand.register
func jfrNode() *Node {
	return &Node{Type: NodeLiteral, Name: "jfr", Required: PermLevelOwners, Children: []*Node{
		{Type: NodeLiteral, Name: "start", Executable: true},
		{Type: NodeLiteral, Name: "stop", Executable: true},
	}}
}

func runJfr(src Source, input string, args []string) error {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "start":
		if err := startJfr(src.ServerDir()); err != nil {
			src.Debug("jfr start failed", "error", err)
			return refuse(src, jfrStartFailedKey)
		}
		return src.Feedback(jfrStartedKey)
	case "stop":
		path, err := stopJfr()
		if err != nil {
			return refuse(src, jfrDumpFailedKey, protocol.Text{Text: err.Error()})
		}
		// path is absolute, so the base must be too.
		rel := path
		if base, err := filepath.Abs(src.ServerDir()); err == nil {
			if r, err := filepath.Rel(base, path); err == nil {
				rel = r
			}
		}
		// vanilla: ComponentUtils.copyOnClickText with underline
		return src.Feedback(jfrStoppedKey, protocol.Text{
			Text:      rel,
			Underline: true,
			ClickCopy: path,
			HoverText: &protocol.Text{Translate: "chat.copy.click"},
		})
	}
	return nil
}

// startJfr begins a trace into a new file under dir/debug.
// vanilla: JfrProfiler.start
func startJfr(dir string) error {
	jfrRun.Lock()
	defer jfrRun.Unlock()
	if jfrRun.file != nil {
		return os.ErrExist
	}
	debug := filepath.Join(dir, "debug")
	if err := os.MkdirAll(debug, 0o755); err != nil {
		return err
	}
	abs, err := filepath.Abs(filepath.Join(debug, "server-"+time.Now().Format(jfrTimeLayout)+".trace"))
	if err != nil {
		return err
	}
	f, err := os.Create(abs)
	if err != nil {
		return err
	}
	if err := trace.Start(f); err != nil {
		_ = f.Close()
		_ = os.Remove(abs)
		return err
	}
	jfrRun.file, jfrRun.dir = f, dir
	return nil
}

// stopJfr ends the trace and returns its absolute path.
// vanilla: JfrProfiler.stop
func stopJfr() (string, error) {
	jfrRun.Lock()
	defer jfrRun.Unlock()
	f := jfrRun.file
	if f == nil {
		return "", errJfrNotRunning
	}
	jfrRun.file, jfrRun.dir = nil, ""
	trace.Stop()
	return f.Name(), f.Close()
}

// StopJfrOnExit ends the recording the server in dir started, if any, so the
// file is complete when the process exits.
// vanilla: JfrProfiler.stop on shutdown
func StopJfrOnExit(dir string) {
	jfrRun.Lock()
	mine := jfrRun.file != nil && jfrRun.dir == dir
	jfrRun.Unlock()
	if mine {
		_, _ = stopJfr()
	}
}
