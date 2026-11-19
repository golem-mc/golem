package command

import (
	"log/slog"
	"runtime/debug"
)

// RunSafe runs cmd for src, turning a panic into vanilla's command.failed
// failure so one bad command cannot take the caller's goroutine down.
// vanilla: Commands.performCommand
func RunSafe(cmd Command, src Source, input string, args []string) (err error) {
	defer recoverCommand(src, input, &err)
	return cmd.Run(src, input, args)
}

// ExecSafe is RunSafe with the command's result value retained.
func ExecSafe(cmd Command, src Source, input string, args []string) (result int32, err error) {
	defer recoverCommand(src, input, &err)
	return cmd.Exec(src, input, args)
}

// recoverCommand, deferred, recovers a command's panic: it logs it, tells src
// command.failed and sets *err to what that refusal answers (ErrRefused, or
// the send's error). The hover with the panic's message is left out.
func recoverCommand(src Source, input string, err *error) {
	p := recover()
	if p == nil {
		return
	}
	slog.Error("command panicked", "input", input, "panic", p, "stack", string(debug.Stack()))
	*err = refuse(src, "command.failed")
}
