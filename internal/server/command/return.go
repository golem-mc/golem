package command

import (
	"strconv"
)

// Return is the /return command: it ends the function it runs in, handing
// back a value, a failure, or the result of the command it runs.
var Return = Command{
	Name:  "return",
	Level: PermLevelGamemasters,
	Build: returnNode,
	Run:   runReturn,
}

// returnNode builds the /return tree.
// vanilla: ReturnCommand.register
func returnNode() *Node {
	return &Node{Type: NodeLiteral, Name: "return", Required: PermLevelGamemasters, Children: []*Node{
		{Type: NodeArgument, Name: "value", Parser: ParserInteger, Executable: true, Int: &IntArg{}},
		{Type: NodeLiteral, Name: "fail", Executable: true},
		{Type: NodeLiteral, Name: "run", RedirectRoot: true},
	}}
}

// runReturn runs a /return outside any function, where it ends only itself.
func runReturn(src Source, input string, args []string) error {
	f := topFrame(src)
	return f.finish(runReturnIn(src, input, args, f, sinkStore(src)))
}

// runReturnIn runs /return in frame f: the value or failure goes to cb and
// f's caller, and f ends. A return run ends f after its command, whose
// result is f's return value.
// vanilla: ReturnCommand.ReturnValueCustomExecutor, ReturnFailCustomExecutor,
// ReturnFromCommandCustomModifier
func runReturnIn(src Source, input string, args []string, f *frame, cb storeFn) error {
	if len(args) == 0 {
		src.Debug("return without a value")
		return nil
	}
	switch args[0] {
	case "run":
		if len(args) < 2 {
			src.Debug("return run without a command")
			return nil
		}
		tokens := Tokenize(input)
		// The tail's words sit two past the command word.
		tailInput := input[tokens[2].Start:]
		err := runCommandIn(src, tailInput, args[1:], f, cb, true)
		// Queued, so a function the command called runs before f ends.
		f.run.later(func(*execRun) { f.discard() })
		return err
	case "fail":
		return finishReturn(f, cb, false, 0)
	}
	value, err := strconv.ParseInt(args[0], 10, 32)
	if err != nil {
		src.Debug("return value did not parse", "value", args[0])
		return nil
	}
	return finishReturn(f, cb, true, int32(value))
}

// finishReturn hands a return's outcome to the sender's callback and the
// frame's caller, and ends the frame.
func finishReturn(f *frame, cb storeFn, success bool, value int32) error {
	f.discard()
	if cb != nil {
		if err := cb(success, value); err != nil {
			return err
		}
	}
	return f.returnValue(success, value)
}
