package plugins_test

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/console"
	"github.com/golem-mc/golem/plugin"
)

// A plugin command that panics fails with command.failed, and the console
// names the plugin and prints the stack under the line, down to the
// plugin's own frame. Not parallel: it swaps the process's default logger.
func TestAPanickingPluginCommandLogsItsStack(t *testing.T) {
	var log lockedBuffer
	old := slog.Default()
	slog.SetDefault(slog.New(console.NewLogHandler(&log, slog.LevelInfo, false)))
	t.Cleanup(func() { slog.SetDefault(old) })

	boom := plugin.Command{Name: "boom", Run: func(*plugin.CommandContext) (int, error) {
		var m map[string]int
		m["x"] = 1 // assignment to entry in nil map
		return 1, nil
	}}
	srv, _ := bukkitHelpServer(t, boom)
	if out := srv.RunLine("boom"); !strings.Contains(fmt.Sprint(out), "command.failed") {
		t.Fatalf("boom answered %v, want command.failed", out)
	}
	out := log.String()
	line := `Error in command plugin=Warps input=boom error="panic: assignment to entry in nil map"`
	i := strings.Index(out, line)
	if i < 0 {
		t.Fatalf("no panic line %q in\n%s", line, out)
	}
	stack := out[i+len(line):]
	if !strings.HasPrefix(stack, "\n\tgoroutine ") || !strings.Contains(stack, "TestAPanickingPluginCommandLogsItsStack") {
		t.Fatalf("no stack down to the command under the line:\n%s", stack)
	}
}
