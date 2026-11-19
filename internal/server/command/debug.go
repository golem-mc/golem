package command

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
)

// DebugCommand is the /debug command: the tick profiler and function traces.
var DebugCommand = Command{
	Name:  "debug",
	Level: PermLevelAdmins,
	Build: debugNode,
	Run:   runDebug,
}

// The translation keys /debug answers with.
const (
	debugStartedKey        = "commands.debug.started"
	debugStoppedKey        = "commands.debug.stopped"
	debugNotRunningKey     = "commands.debug.notRunning"
	debugAlreadyRunningKey = "commands.debug.alreadyRunning"
	debugTraceFailedKey    = "commands.debug.function.traceFailed"
	debugTraceSingleKey    = "commands.debug.function.success.single"
	debugTraceMultipleKey  = "commands.debug.function.success.multiple"
	debugNoRecursionKey    = "commands.debug.function.noRecursion"
	debugTraceTimeLayout   = "2006-01-02_15.04.05"
)

// debugNode builds the /debug tree.
// vanilla: DebugCommand.register
func debugNode() *Node {
	name := &Node{Type: NodeArgument, Name: "name", Parser: ParserFunction, Executable: true,
		Check: checkFunctionSyntax, Suggests: AskServerSuggestion, Completions: functionCompletions}
	return &Node{Type: NodeLiteral, Name: "debug", Required: PermLevelAdmins, Children: []*Node{
		{Type: NodeLiteral, Name: "start", Executable: true},
		{Type: NodeLiteral, Name: "stop", Executable: true},
		{Type: NodeLiteral, Name: "function", Required: PermLevelAdmins, Children: []*Node{name}},
	}}
}

func runDebug(src Source, input string, args []string) error {
	if len(args) == 0 {
		return nil
	}
	switch args[0] {
	case "start":
		if !src.Metrics().StartProfile(time.Now()) {
			return refuse(src, debugAlreadyRunningKey)
		}
		return feedbackToAdmins(src, debugStartedKey)
	case "stop":
		elapsed, ticks, ok := src.Metrics().StopProfile(time.Now())
		if !ok {
			return refuse(src, debugNotRunningKey)
		}
		seconds := elapsed.Seconds()
		tps := float64(ticks) / seconds
		n := ticks
		if err := feedbackToAdmins(src, debugStoppedKey, protocol.Text{Text: javaFixed(seconds, 2)},
			protocol.Text{Number: &n}, protocol.Text{Text: javaFixed(tps, 2)}); err != nil {
			return err
		}
		reportResult(src, true, int32(tps))
		return nil
	case "function":
		if len(args) < 2 {
			return nil
		}
		return runDebugFunction(src, args[1])
	}
	return nil
}

// runDebugFunction runs the named functions with a tracer writing
// debug/debug-trace-<time>.txt under the server directory.
// vanilla: DebugCommand.TraceCustomExecutor.runGuarded
func runDebugFunction(src Source, word string) error {
	fns, err := namedFunctions(src, word)
	if err != nil {
		return err
	}
	if topFrame(src).run.tracer != nil {
		return refuse(src, debugNoRecursionKey)
	}
	outputName := "debug-trace-" + time.Now().Format(debugTraceTimeLayout) + ".txt"
	dir := filepath.Join(src.ServerDir(), "debug")
	tracer, ferr := openTracer(dir, outputName)
	if ferr != nil {
		src.Debug("function trace failed", "error", ferr)
		return refuse(src, debugTraceFailedKey)
	}
	f := topFrame(src)
	f.run.tracer = tracer
	fsrc, ok := src.InContext(Context{Silent: true})
	if !ok {
		tracer.close()
		return nil
	}
	count := 0
	for _, fn := range fns {
		inst, failure := fn.instantiate(nil)
		if failure != nil {
			_ = refuse(src, functionInstantiationKey, protocol.Text{Text: fn.ID}, *failure)
			continue
		}
		count += len(inst.lines)
		f.run.later(func(r *execRun) {
			tracer.println(inst.ID)
			callFunction(fsrc, inst, f, nil, false)
		})
	}
	f.run.later(func(r *execRun) {
		tracer.close()
		n := int64(count)
		var err error
		if len(fns) == 1 {
			err = feedbackToAdmins(src, debugTraceSingleKey, protocol.Text{Number: &n},
				protocol.Text{Text: fns[0].ID}, protocol.Text{Text: outputName})
		} else {
			m := int64(len(fns))
			err = feedbackToAdmins(src, debugTraceMultipleKey, protocol.Text{Number: &n},
				protocol.Text{Number: &m}, protocol.Text{Text: outputName})
		}
		if err != nil {
			r.fail(err)
		}
	})
	return f.finish(nil)
}

// functionTracer writes the trace file: one line per call, command, result
// and error, indented by call depth. All methods are no-ops on nil.
// vanilla: DebugCommand.Tracer
type functionTracer struct {
	file          *os.File
	out           *bufio.Writer
	lastIndent    int
	waitingResult bool
}

func openTracer(dir, name string) (*functionTracer, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	out, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	return &functionTracer{file: out, out: bufio.NewWriter(out)}, nil
}

// write buffers s; a write error resurfaces at Flush and is dropped there.
func (t *functionTracer) write(s string) { _, _ = t.out.WriteString(s) }

func (t *functionTracer) println(s string) {
	if t != nil {
		t.write(s + "\n")
	}
}

func (t *functionTracer) printIndent(depth int) {
	for i := 0; i < depth+1; i++ {
		t.write("    ")
	}
}

func (t *functionTracer) indentAndSave(depth int) {
	t.printIndent(depth)
	t.lastIndent = depth
}

// newLine ends a command line still waiting for its result.
func (t *functionTracer) newLine() {
	if t.waitingResult {
		t.write("\n")
		t.waitingResult = false
	}
}

func (t *functionTracer) onCommand(depth int, command string) {
	if t == nil {
		return
	}
	t.newLine()
	t.indentAndSave(depth)
	t.write("[C] " + command)
	t.waitingResult = true
}

func (t *functionTracer) onReturn(depth int, command string, result int32) {
	if t == nil {
		return
	}
	if t.waitingResult {
		t.write(" -> " + strconv.Itoa(int(result)) + "\n")
		t.waitingResult = false
		return
	}
	t.indentAndSave(depth)
	t.write("[R = " + strconv.Itoa(int(result)) + "] " + command + "\n")
}

func (t *functionTracer) onCall(depth int, id string, size int) {
	if t == nil {
		return
	}
	t.newLine()
	t.indentAndSave(depth)
	t.write("[F] " + id + " size=" + strconv.Itoa(size) + "\n")
}

// onError notes a command's failure; a refusal's message went to the
// silenced source, so only its kind is known here.
func (t *functionTracer) onError(err error) {
	if t == nil {
		return
	}
	t.newLine()
	t.indentAndSave(t.lastIndent + 1)
	t.write("[E] " + err.Error() + "\n")
}

func (t *functionTracer) close() {
	if t == nil {
		return
	}
	t.newLine()
	_ = t.out.Flush()
	_ = t.file.Close()
}
