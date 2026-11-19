package commands

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// consoleRefusal is the reply RunLine collects for a parse refusal: the
// message in red, then the context line, which mirrors the one a player is
// sent.
func consoleRefusal(key, input string, cursor int) []protocol.Text {
	context := protocol.Text{ClickCommand: "/" + input, Color: protocol.ColorGray}
	if cursor > 10 {
		context.Extra = append(context.Extra, protocol.Text{Text: "..."})
	}
	if pre := max(0, cursor-10); pre < cursor {
		context.Extra = append(context.Extra, protocol.Text{Text: input[pre:cursor]})
	}
	if cursor < len(input) {
		context.Extra = append(context.Extra, protocol.Text{
			Text:      input[cursor:],
			Color:     protocol.ColorRed,
			Underline: true,
		})
	}
	context.Extra = append(context.Extra, protocol.Text{
		Translate: "command.context.here",
		Color:     protocol.ColorRed,
		Italic:    true,
	})
	return []protocol.Text{
		{Color: protocol.ColorRed, Extra: []protocol.Text{{Translate: key}}},
		{Color: protocol.ColorRed, Extra: []protocol.Text{context}},
	}
}

// TestConsoleSpellsTheTrailingRefusals runs the lines that end in input
// their read left behind through the console source: a word or space the
// dispatcher's separator rules refuse where the read stopped, and one
// trailing space behind a redirecting word its unknown command at the
// input's end. With more than one trailing space the dispatcher skips one as
// the separator, so the cursor sits behind it; the console never trims the
// line. vanilla: DedicatedServer.handleConsoleInputs, Commands.finishParsing
func TestConsoleSpellsTheTrailingRefusals(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SpawningTestConfig(), "Testifice", servertest.OpsListing(t, "Testifice"))

	for _, tc := range []struct {
		line   string
		key    string
		cursor int
	}{
		{"time set daytime ", "command.unknown.argument", len("time set daytime")},
		{"time set daytime   ", "command.unknown.argument", len("time set daytime") + 1},
		{"execute as @a run ", "command.unknown.command", len("execute as @a run ")},
	} {
		if reply := srv.RunLine(tc.line); !reflect.DeepEqual(reply, consoleRefusal(tc.key, tc.line, tc.cursor)) {
			t.Errorf("the console answered %q with %+v, want its %s at %d", tc.line, reply, tc.key, tc.cursor)
		}
	}
}
