package command

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// FunctionCommand is the /function command: it runs every command of a loaded
// data pack function, silently, as the runner.
var FunctionCommand = Command{
	Name:  "function",
	Level: PermLevelGamemasters,
	Build: functionNode,
	Run:   runFunction,
}

// The translation keys /function answers with.
const (
	functionScheduledKey     = "commands.function.scheduled.single"
	functionScheduledManyKey = "commands.function.scheduled.multiple"
	functionResultKey        = "commands.function.result"
	functionNoFunctionsKey   = "commands.function.scheduled.no_functions"
	functionUnknownKey       = "arguments.function.unknown"
)

// maxFunctionLine is the longest line, continuations joined, a function file
// may hold. vanilla: CommandFunction.checkCommandLineLength
const maxFunctionLine = 2000000

// functionNode builds the /function tree: the name, the compound of
// arguments behind it, and the with clause's three data sources, each
// reading its whole compound or the one its path names.
// vanilla: FunctionCommand.register
func functionNode() *Node {
	path := func() *Node {
		node := pathArg("path")
		node.Executable = true
		return node
	}
	name := &Node{Type: NodeArgument, Name: "name", Parser: ParserFunction, Executable: true,
		Check: checkFunctionSyntax, Suggests: AskServerSuggestion, Completions: functionCompletions}
	name.Children = []*Node{
		{Type: NodeArgument, Name: "arguments", Parser: ParserNbtCompound,
			Nbt: true, Executable: true},
		{Type: NodeLiteral, Name: "with", Children: []*Node{
			{Type: NodeLiteral, Name: "entity", Children: []*Node{
				{Type: NodeArgument, Name: "source", Parser: ParserEntity,
					Properties: func(b *protocol.Buffer) { b.PutU8(1) },
					EntityArg:  &EntityArg{Single: true}, Executable: true, Children: []*Node{path()}},
			}},
			{Type: NodeLiteral, Name: "block", Children: []*Node{
				{Type: NodeArgument, Name: "source", Parser: ParserBlockPos,
					Executable: true, Children: []*Node{path()}},
			}},
			{Type: NodeLiteral, Name: "storage", Children: []*Node{
				{Type: NodeArgument, Name: "source", Parser: ParserResourceLoc,
					Executable: true, Children: []*Node{path()}},
			}},
		}},
	}
	return &Node{Type: NodeLiteral, Name: "function", Required: PermLevelGamemasters, Children: []*Node{name}}
}

// functionCompletions offers the ids of the loaded functions.
// vanilla: FunctionCommand.SUGGEST_FUNCTION
func functionCompletions(ctx any, partial string) (int, []string) {
	src, ok := ctx.(Source)
	if !ok {
		return 0, nil
	}
	return 0, ResourceSuggestions(partial, src.Functions().IDs()...)
}

// Function is one loaded data pack function: its id and its commands, each
// matched against the tree of the permission level it was loaded at. A
// file with a '$' line keeps its lines in macro, which every call
// instantiates from its arguments; lines is then unread.
type Function struct {
	ID    string
	lines []functionLine
	macro *macroFunction
	// level is the permission level the function compiled at; its macro
	// lines are matched against that level's tree on every call.
	// vanilla: MacroFunction.MacroEntry.compilationContext
	level int
}

// functionLine is one command of a function: its text and its words.
type functionLine struct {
	input string
	words []string
}

// Functions is the library of loaded functions /function runs from. A nil
// library holds none.
// vanilla: ServerFunctionLibrary
type Functions struct {
	byID map[string]*Function
	ids  []string
	tags map[string][]*Function
}

// Get returns the function of the id.
func (l *Functions) Get(id string) (*Function, bool) {
	if l == nil {
		return nil, false
	}
	fn, ok := l.byID[id]
	return fn, ok
}

// Tag returns the functions of the tag, none for a tag that is unknown or
// failed to load.
// vanilla: ServerFunctionLibrary.getTag
func (l *Functions) Tag(id string) []*Function {
	if l == nil {
		return nil
	}
	return l.tags[id]
}

// LoadTags builds the function tags: entries in order, named tags spliced
// in, repeats dropped. A tag with a missing required entry or in a cycle is
// left out and reported through fail.
// vanilla: TagLoader.build, TagEntry.build
func (l *Functions) LoadTags(tags datapack.FunctionTags, fail func(id string, err error)) {
	l.tags = make(map[string][]*Function, len(tags))
	const building, failed = 1, 2
	state := make(map[string]int, len(tags))
	var build func(id string) bool
	build = func(id string) bool {
		switch state[id] {
		case building, failed:
			return false
		}
		if _, ok := l.tags[id]; ok {
			return true
		}
		state[id] = building
		var out []*Function
		seen := map[*Function]bool{}
		add := func(fn *Function) {
			if !seen[fn] {
				seen[fn] = true
				out = append(out, fn)
			}
		}
		var missing []string
		for _, e := range tags[id] {
			switch {
			case e.Tag:
				_, known := tags[e.ID]
				if known && build(e.ID) {
					for _, fn := range l.tags[e.ID] {
						add(fn)
					}
				} else if e.Required {
					missing = append(missing, "#"+e.ID)
				}
			default:
				if fn, ok := l.byID[e.ID]; ok {
					add(fn)
				} else if e.Required {
					missing = append(missing, e.ID)
				}
			}
		}
		if len(missing) > 0 {
			state[id] = failed
			fail(id, fmt.Errorf("missing references: %s", strings.Join(missing, ", ")))
			return false
		}
		state[id] = 0
		l.tags[id] = out
		return true
	}
	for _, id := range slices.Sorted(maps.Keys(tags)) {
		build(id)
	}
}

// IDs returns the ids of every loaded function, sorted.
func (l *Functions) IDs() []string {
	if l == nil {
		return nil
	}
	return l.ids
}

// CompileFunctions matches every line of every function file against the
// tree of permission level level (function-permission-level). A file with
// a line that does not parse is left out of the library and reported
// through fail.
// vanilla: ServerFunctionLibrary.reload
func CompileFunctions(files map[string][]string, level int, layers *registry.PackLayers, fail func(id string, err error)) *Functions {
	lineTokens.Clear()
	argCompounds.Clear()
	argCompoundCount.Store(0)
	lib := &Functions{byID: make(map[string]*Function, len(files))}
	if len(files) == 0 {
		return lib
	}
	tree := functionCompileTree(level, layers)
	for id, lines := range files {
		fn, err := compileFunction(tree, level, id, lines)
		if err != nil {
			fail(id, err)
			continue
		}
		lib.byID[id] = fn
		lib.ids = append(lib.ids, id)
	}
	slices.Sort(lib.ids)
	return lib
}

// functionCompileTree binds checks on a fresh graph so live commands keep
// reading the published registries while the candidate is being validated.
// vanilla: ReloadableServerResources.loadResources
func functionCompileTree(level int, layers *registry.PackLayers) *Node {
	tree := buildForLevel(level)
	check := predicateArgumentCheck(layers)
	var bind func(*Node)
	bind = func(node *Node) {
		if node.Parser == ParserLootPredicate {
			node.Check = check
		}
		for _, child := range node.Children {
			bind(child)
		}
	}
	bind(tree)
	return tree
}

// compileFunction reads a function file's lines: each trimmed, a trailing
// backslash joining the next line on, blank lines and # comments skipped.
// vanilla: CommandFunction.fromLines
func compileFunction(tree *Node, level int, id string, lines []string) (*Function, error) {
	fn := &Function{ID: id, level: level}
	for i := 0; i < len(lines); i++ {
		lineNumber := i + 1
		line := trimJava(lines[i])
		for strings.HasSuffix(line, `\`) {
			i++
			if i == len(lines) {
				return nil, errors.New("line continuation at end of file")
			}
			line = line[:len(line)-1] + trimJava(lines[i])
			if len(line) > maxFunctionLine {
				return nil, fmt.Errorf("command too long: %d characters", len(line))
			}
		}
		if len(line) > maxFunctionLine {
			return nil, fmt.Errorf("command too long: %d characters", len(line))
		}
		if line == "" || line[0] == '#' {
			continue
		}
		if line[0] == '/' {
			if strings.HasPrefix(line, "//") {
				return nil, fmt.Errorf("unknown or invalid command '%s' on line %d (if you intended to make a comment, use '#' not '//')", line, lineNumber)
			}
			return nil, fmt.Errorf("unknown or invalid command '%s' on line %d (did you mean '%s'? Do not use a preceding forwards slash.)", line, lineNumber, unquotedPrefix(line[1:]))
		}
		if line[0] == '$' {
			// A '$' line turns the file into a macro: its plain lines
			// stay, and every call substitutes and matches each template
			// against the arguments it resolved.
			fn.beginMacro()
			template, err := parseMacroTemplate(line[1:], fn.macro.macroArgument)
			if err != nil {
				// vanilla: FunctionBuilder.addMacro
				//nolint:staticcheck // ST1005: vanilla's message, capital and single quotes included
				return nil, fmt.Errorf("Can't parse function line %d: '%s': %w", lineNumber, line[1:], err)
			}
			fn.macro.entries = append(fn.macro.entries, macroEntry{template: template})
			continue
		}
		tokens := Tokenize(line)
		node, failure := Match(tree, line, tokens, true)
		if node == nil {
			//nolint:staticcheck // ST1005: the load-failure message spells its W as the wire has it
			return nil, fmt.Errorf("Whilst parsing command on line %d: %s", lineNumber, describeFailure(line, tokens, failure))
		}
		words := make([]string, len(tokens))
		for i, t := range tokens {
			words[i] = t.Text
		}
		storeLineTokens(line, tokens)
		fn.addLine(functionLine{input: line, words: words})
	}
	return fn, nil
}

// describeFailure spells the parse failure of a function line the way the
// exception the dispatcher rethrows reads: its message rendered through the
// client's language table, then the position, with the up-to-ten characters
// before it the client points at. An unknown command carries its cursor at
// the first word, an every-word-match that falls short of a command at the
// input's end; input a read left behind it and a stopping word are unknown
// arguments, each where its reader stands; a typed error its own.
// vanilla: CommandSyntaxException.getMessage, Commands.getParseException
func describeFailure(input string, tokens []Token, f *Failure) string {
	switch {
	case f.Err != nil:
		if f.Err.NoContext {
			return f.Err.Error()
		}
		return atPosition(input, f.Err.Cursor, f.Err.Error())
	case f.UnknownCommand && f.At == 0:
		return atPosition(input, 0, messageOf(keyUnknownCommand))
	case f.UnknownCommand:
		return atPosition(input, len(input), messageOf(keyUnknownCommand))
	case f.Trailing:
		return atPosition(input, TrailingCursor(input, tokens), messageOf(keyUnknownArgument))
	case f.At < len(tokens):
		return atPosition(input, tokens[f.At].Start, messageOf(keyUnknownArgument))
	}
	return messageOf(keyUnknownArgument)
}

// atPosition appends a cursor and its context to message the way
// CommandSyntaxException does: at most the ten characters before the
// cursor, an ellipsis standing for the ones clipped.
// vanilla: CommandSyntaxException.getMessage, getContext
func atPosition(input string, cursor int, message string) string {
	if cursor > len(input) {
		cursor = len(input)
	}
	ellipsis := ""
	if cursor > 10 {
		ellipsis = "..."
	}
	return fmt.Sprintf("%s at position %d: %s%s<--[HERE]", message, cursor, ellipsis, input[max(0, cursor-10):cursor])
}

// trimJava trims what Java's String.trim does: every character up to and
// including the space at either end.
func trimJava(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return r <= ' ' })
}

// unquotedPrefix is the run of characters StringReader.readUnquotedString
// reads from the start of s.
func unquotedPrefix(s string) string {
	n := 0
	for n < len(s) {
		c := s[n]
		if c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' ||
			c == '_' || c == '-' || c == '.' || c == '+' {
			n++
			continue
		}
		break
	}
	return s[:n]
}

// maxQueueDepth is the most steps a run keeps queued before it stops.
// vanilla: ExecutionContext.MAX_QUEUE_DEPTH
const maxQueueDepth = 10000000

// execRun is one top-level command's execution, shared by every function it
// calls, counting what it may still run under max_command_sequence_length.
// Calls queue here and run in drain, so recursion grows the heap, not the Go
// stack.
// vanilla: ExecutionContext
type execRun struct {
	quota   int
	limit   int
	stopped bool
	// overflowed is set once the queue passed maxQueueDepth; nothing more is
	// queued and drain stops.
	overflowed bool
	// maxQueue is the most steps kept queued.
	maxQueue int
	// src receives the stop messages.
	src Source
	// pushed collects what the command now running queued, in call order;
	// drain moves it onto stack once the command returns.
	pushed []func(*execRun)
	// stack holds the queued work, the next to run on top.
	stack []func(*execRun)
	// err is the first error a queued step ended with.
	err error
	// tracer, when set, records the run's calls and commands for /debug
	// function.
	tracer *functionTracer
}

// spend takes one command off the quota, reporting false, and stopping the
// whole run, once none is left.
// vanilla: ExecutionContext.runCommandQueue, incrementCost
func (r *execRun) spend() bool {
	if r.quota <= 0 {
		r.stop()
		return false
	}
	r.quota--
	return true
}

// stop ends the whole run for want of quota, saying so once.
func (r *execRun) stop() {
	if !r.stopped {
		r.stopped = true
		r.src.Info(fmt.Sprintf("Command execution stopped due to limit (executed %d commands)", r.limit))
	}
}

// later queues a step to run after the running command, behind the steps it
// queued before.
// vanilla: ExecutionContext.queueNext
func (r *execRun) later(step func(*execRun)) {
	// vanilla: ExecutionContext.queueNext, handleQueueOverflow
	if len(r.pushed)+len(r.stack) > r.maxQueue {
		r.overflowed = true
		r.pushed = nil
		r.stack = nil
	}
	if r.overflowed {
		return
	}
	r.pushed = append(r.pushed, step)
}

// flush puts the steps queued by the command that just ran on top of the
// stack, the first queued on top.
func (r *execRun) flush() {
	for _, v := range slices.Backward(r.pushed) {
		r.stack = append(r.stack, v)
	}
	clear(r.pushed)
	r.pushed = r.pushed[:0]
}

// drain runs the queued steps until none is left, returning the first error
// one of them ended with.
func (r *execRun) drain() error {
	r.flush()
	for {
		// Checked before every entry, also one that would cost nothing.
		// vanilla: ExecutionContext.runCommandQueue
		if r.quota <= 0 {
			r.stop()
			break
		}
		if len(r.stack) == 0 {
			break
		}
		step := r.stack[len(r.stack)-1]
		r.stack[len(r.stack)-1] = nil
		r.stack = r.stack[:len(r.stack)-1]
		step(r)
		if r.overflowed {
			r.src.Info(fmt.Sprintf("Command execution stopped due to command queue overflow (max %d)", r.maxQueue))
			break
		}
		r.flush()
	}
	return r.err
}

// fail records the first error a queued step ended with.
func (r *execRun) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

// frame is one level of a run (the top command or a function call). /return
// hands ret its value and discards the frame; a frame called by a return run
// shares its caller's discard flag, so one flag ends the chain.
// vanilla: Frame
type frame struct {
	run *execRun
	// ret receives the frame's return value; nil drops it.
	ret       storeFn
	discarded *bool
	// depth is how many function calls deep the frame is; the top frame is 0.
	depth int
}

// topFrame is the frame a command run from a player, the console or a
// command block starts in.
// vanilla: Commands.executeCommandInContext
func topFrame(src Source) *frame {
	limit := max(1, int(src.IntRule(world.RuleMaxCommandSequence)))
	return &frame{run: &execRun{quota: limit, limit: limit, src: src, maxQueue: maxQueueDepth}, discarded: new(bool)}
}

// finish runs the function calls a top-level command queued, once the
// command itself has run.
func (f *frame) finish(err error) error {
	if derr := f.run.drain(); err == nil {
		err = derr
	}
	return err
}

// done reports whether the frame's remaining commands are not to run.
func (f *frame) done() bool {
	return f.run.stopped || *f.discarded
}

// discard ends the frame, and the caller a return run chained it to.
func (f *frame) discard() {
	*f.discarded = true
}

// returnValue hands the frame's return value to whoever called it.
// vanilla: Frame.returnSuccess, Frame.returnFailure
func (f *frame) returnValue(success bool, result int32) error {
	if f.ret == nil {
		return nil
	}
	return f.ret(success, result)
}

// chainStores is a callback that calls a and then b, either may be nil.
// vanilla: CommandResultCallback.chain
func chainStores(a, b storeFn) storeFn {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	}
	return func(success bool, result int32) error {
		if err := a(success, result); err != nil {
			return err
		}
		return b(success, result)
	}
}

// sinkStore is the callback a top-level command reports to: the runner's
// result sink, if it keeps one.
func sinkStore(src Source) storeFn {
	return func(success bool, result int32) error {
		reportResult(src, success, result)
		return nil
	}
}

// RunFunction runs fn as src, the way the game loop runs a function: silent,
// its own command quota, nobody told of a failure. A macro that runs on no
// arguments fails quietly here too, the way the manager drops the
// instantiation.
// vanilla: ServerFunctionManager.execute
func RunFunction(src Source, fn *Function) {
	defer func() {
		// A panic drops the rest of this run's queue and tells nobody.
		if p := recover(); p != nil {
			slog.Error("Failed to execute function", "function", fn.ID, "panic", p, "stack", string(debug.Stack()))
		}
	}()
	fsrc, ok := src.InContext(Context{Silent: true})
	if !ok {
		return
	}
	inst, failure := fn.instantiate(nil)
	if failure != nil {
		return
	}
	f := topFrame(src)
	callFunction(fsrc, inst, f, nil, false)
	_ = f.finish(nil)
}

// runFunction runs a /function from a player, the console or a command block.
func runFunction(src Source, input string, args []string) error {
	f := topFrame(src)
	return f.finish(runFunctionIn(src, input, args, f, sinkStore(src), false))
}

// runFunctionIn runs a /function in frame f, cb receiving each function's
// return value. Under a return run the functions' returns end f as well, and
// f fails if none returns.
// vanilla: FunctionCommand.FunctionCustomExecutor.runGuarded, queueFunctions
func runFunctionIn(src Source, input string, args []string, f *frame, cb storeFn, ret bool) error {
	if len(args) == 0 {
		src.Debug("function without a name")
		return nil
	}
	fns, err := namedFunctions(src, args[0])
	if err != nil {
		return err
	}
	// The with clause reads before the functions are announced, and its
	// own refusals name no function.
	// vanilla: FunctionCustomExecutor.arguments
	arguments, err := functionArguments(src, input, args)
	if err != nil {
		return err
	}
	if len(fns) == 1 {
		if err := feedbackToAdmins(src, functionScheduledKey, protocol.Text{Text: fns[0].ID}); err != nil {
			return err
		}
	} else if err := feedbackToAdmins(src, functionScheduledManyKey, functionList(fns)); err != nil {
		return err
	}
	fsrc, ok := src.InContext(Context{Silent: true})
	if !ok {
		return nil
	}
	// Several functions under a caller that wants a result hand it their
	// sum, once all have run.
	// vanilla: FunctionCommand.queueFunctionsNoReturn
	inner := cb
	var sum int32
	var anyResult bool
	if !ret && len(fns) > 1 && cb != nil {
		inner = func(success bool, result int32) error {
			anyResult = true
			sum += result
			return nil
		}
	}
	if ret {
		inner = chainStores(cb, f.returnValue)
	}
	// Instantiated one by one as each queues: a member that cannot be
	// refuses naming itself once the members before it have queued, and
	// theirs are left to run. The tag's slice is the library's own, so
	// the instantiations never go back into it.
	// vanilla: instantiateAndQueueFunctions
	for _, fn := range fns {
		inst, failure := fn.instantiate(arguments)
		if failure != nil {
			return refuse(src, functionInstantiationKey, protocol.Text{Text: fn.ID}, *failure)
		}
		// The result line goes through src, so a function's own commands,
		// whose source is silent, report none.
		// vanilla: FunctionCommand.decorateOutputIfNeeded
		callback := func(success bool, result int32) error {
			n := int64(result)
			if err := feedbackToAdmins(src, functionResultKey, protocol.Text{Text: fn.ID}, protocol.Text{Number: &n}); err != nil {
				return err
			}
			if inner == nil {
				return nil
			}
			return inner(success, result)
		}
		// Queued, so the calls of an execute's contexts are all announced
		// before the first one runs.
		// vanilla: CallFunction, FallthroughTask
		f.run.later(func(r *execRun) { callFunction(fsrc, inst, f, callback, ret) })
	}
	if !ret && len(fns) > 1 && cb != nil {
		f.run.later(func(r *execRun) {
			if !anyResult {
				return
			}
			if err := cb(true, sum); err != nil {
				r.fail(err)
			}
		})
	}
	if ret {
		f.run.later(func(r *execRun) {
			if f.done() {
				return
			}
			if err := f.returnValue(false, 0); err != nil {
				r.fail(err)
				return
			}
			f.discard()
		})
	}
	return nil
}

// namedFunctions resolves a function argument: one function, or a tag's
// members, refusing an unknown name and an empty tag.
func namedFunctions(src Source, word string) ([]*Function, error) {
	if tag, ok := strings.CutPrefix(word, "#"); ok {
		id := registry.Qualify(tag)
		fns := src.Functions().Tag(id)
		if len(fns) == 0 {
			return nil, refuse(src, functionNoFunctionsKey, protocol.Text{Text: id})
		}
		return fns, nil
	}
	id := registry.Qualify(word)
	fn, ok := src.Functions().Get(id)
	if !ok {
		return nil, refuse(src, functionUnknownKey, protocol.Text{Text: id})
	}
	return []*Function{fn}, nil
}

// scheduleFunctionTest queues, for each context, the named functions as a
// return run whose first return value decides the context (non-zero for
// if, zero for unless; no return counts as zero), then queues resume over
// the contexts that passed, which may be none. The passed list is not held
// to the fork limit; the steps after it are.
// vanilla: ExecuteCommand.scheduleFunctionConditionsAndTest
func scheduleFunctionTest(src Source, ctxs []execContext, expected bool, word string, f *frame, resume func([]execContext) error) error {
	fns, err := namedFunctions(src, word)
	if err != nil {
		return err
	}
	insts := make([]*Function, len(fns))
	for k, fn := range fns {
		inst, failure := fn.instantiate(nil)
		if failure != nil {
			return refuse(src, functionInstantiationKey, protocol.Text{Text: fn.ID}, *failure)
		}
		insts[k] = inst
	}
	var passed []execContext
	for _, ctx := range ctxs {
		c := ctx.commandContext(false)
		c.Silent = true
		fsrc, ok := src.InContext(c)
		if !ok {
			continue
		}
		sub := &frame{run: f.run, discarded: new(bool)}
		sub.ret = func(success bool, result int32) error {
			if (result != 0) == expected {
				passed = append(passed, ctx)
			}
			return nil
		}
		f.run.later(func(r *execRun) {
			for _, inst := range insts {
				callFunction(fsrc, inst, sub, sub.returnValue, true)
			}
			r.later(func(r *execRun) {
				if sub.done() {
					return
				}
				_ = sub.returnValue(false, 0)
				sub.discard()
			})
		})
	}
	f.run.later(func(r *execRun) {
		if err := resume(passed); err != nil {
			r.fail(err)
		}
	})
	return nil
}

// functionList is the text a multi-function announcement names the functions
// by.
// vanilla: ComponentUtils.formatList
func functionList(fns []*Function) protocol.Text {
	var list protocol.Text
	for i, fn := range fns {
		if i > 0 {
			list.Extra = append(list.Extra, protocol.Text{Text: ", ", Color: protocol.ColorGray})
		}
		list.Extra = append(list.Extra, protocol.Text{Text: fn.ID})
	}
	return list
}

// maxArgCompounds bounds argCompounds.
const maxArgCompounds = 4096

// argCompounds holds the compound each literal-argument /function line read,
// which a tick function reads again every tick. Shared, so read-only; cleared
// on reload and whenever it passes its bound.
var (
	argCompounds     sync.Map // input -> nbt.Compound
	argCompoundCount atomic.Int32
)

// functionArguments reads the macro arguments a /function hands the
// functions it runs: the compound written after their name, or the with
// clause's source — its whole compound, or the one compound its path
// reads — none for the bare name. A plain function ignores them; a macro
// without any refuses, and a source that keeps nothing reads as the
// empty compound that misses every name.
// vanilla: FunctionCustomExecutor.arguments, FunctionCommand.getArgumentTag
func functionArguments(src Source, input string, args []string) (nbt.Compound, error) {
	rest := args[1:]
	if len(rest) == 0 {
		return nil, nil
	}
	tokens := Tokenize(input)
	if rest[0] != "with" {
		if c, ok := argCompounds.Load(input); ok {
			return c.(nbt.Compound), nil
		}
		compound, perr := nbt.ParseSNBT(wordsInTokens(tokens, args, 1, len(rest)))
		if perr == nil {
			if argCompoundCount.Load() > maxArgCompounds {
				argCompounds.Clear()
				argCompoundCount.Store(0)
			}
			argCompounds.Store(input, compound)
			argCompoundCount.Add(1)
		}
		if perr != nil {
			// A compound the dispatcher read cannot fail here; the token
			// joins were the things that changed.
			src.Debug("function arguments did not read back", "args", rest)
			return nil, nil
		}
		return compound, nil
	}
	const at = 2
	target, span, err := resolveDataTarget(src, args, at, tokens, at+1)
	if err != nil {
		return nil, err
	}
	var arguments nbt.CompoundTag
	if pathAt := at + 1 + span; pathAt < len(args) {
		path, perr := parseNBTPath(wordsInTokens(tokens, args, pathAt, len(args)-pathAt))
		if perr != nil {
			src.Debug("function with path did not parse", "path", args[pathAt:])
			return nil, nil
		}
		tag, refusal := target.getTag(path)
		if refusal != nil {
			return nil, refuse(src, refusal.key, tagTexts(refusal)...)
		}
		compound, ok := tag.(nbt.CompoundTag)
		if !ok {
			return nil, refuse(src, functionNotCompoundKey, protocol.Text{Text: nbtTypeName(tag)})
		}
		arguments = compound
	} else {
		arguments = target.getData()
	}
	if arguments == nil {
		arguments = nbt.Compound{}
	}
	return nbt.PlainCopy(arguments).(nbt.Compound), nil
}

// callFunction starts fn's commands in a new frame under f, ret receiving
// what a /return in it hands back. returnParent says a return in fn also
// ends f.
// vanilla: CallFunction.execute
func callFunction(src Source, fn *Function, f *frame, ret storeFn, returnParent bool) {
	if !f.run.spend() {
		return
	}
	callee := &frame{run: f.run, ret: ret, discarded: new(bool), depth: f.depth + 1}
	f.run.tracer.onCall(f.depth, fn.ID, len(fn.lines))
	if returnParent {
		callee.discarded = f.discarded
	}
	f.run.later(functionStep(src, fn, callee, 0))
}

// functionStep is the step that runs line i of fn and queues the rest of the
// function behind whatever that line queued.
func functionStep(src Source, fn *Function, callee *frame, i int) func(*execRun) {
	return func(r *execRun) {
		if callee.done() || i >= len(fn.lines) {
			return
		}
		line := fn.lines[i]
		// The commands of a function report to nobody but a /return.
		// vanilla: FunctionCommand.queueFunctions, clearCallbacks
		var traced storeFn
		if t := callee.run.tracer; t != nil {
			t.onCommand(callee.depth, line.input)
			traced = func(success bool, result int32) error {
				t.onReturn(callee.depth, line.input, result)
				return nil
			}
		}
		err := runCommandIn(src, line.input, line.words, callee, traced, false)
		if err != nil {
			callee.run.tracer.onError(err)
		}
		if err != nil &&
			!errors.Is(err, ErrRefused) && !errors.Is(err, errStoreThrew) {
			if _, ok := errors.AsType[*ParseError](err); !ok {
				src.Debug("function command failed", "function", fn.ID, "command", line.input, "error", err)
			}
		}
		if i+1 < len(fn.lines) {
			r.later(functionStep(src, fn, callee, i+1))
		}
	}
}

// runCommandIn runs one matched command, words[0] its name, in frame f,
// handing its result to cb. ret says it runs under a return run, whose
// result is f's return value.
// vanilla: BuildContexts.execute, ExecuteCommand.execute
func runCommandIn(src Source, input string, words []string, f *frame, cb storeFn, ret bool) error {
	cmd, ok := LookupSource(src, words[0])
	if !ok || cmd.Run == nil {
		src.Debug("function command names nothing runnable", "word", words[0])
		return nil
	}
	switch cmd.Name {
	case "execute":
		var stores []storeFn
		if cb != nil {
			stores = []storeFn{cb}
		}
		return runExecuteIn(src, input, words[1:], stores, f, ret)
	case "function":
		return runFunctionIn(src, input, words[1:], f, cb, ret)
	case "return":
		return runReturnIn(src, input, words[1:], f, cb)
	}
	if !f.run.spend() {
		return nil
	}
	result, err := cmd.Exec(src, input, words[1:])
	if ret {
		cb = chainStores(cb, f.returnValue)
	}
	if cb != nil {
		if serr := cb(err == nil, result); serr != nil {
			return serr
		}
	}
	return err
}

// storeLineTokens keeps the tokens of a line and of every tail after a "run",
// the text execute hands to the command it runs.
func storeLineTokens(line string, tokens []Token) {
	walkRunTails(line, tokens, func(l string, t []Token) { lineTokens.Store(l, slices.Clip(t)) })
}
