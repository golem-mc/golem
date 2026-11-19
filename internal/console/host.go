package console

import "github.com/golem-mc/golem/internal/protocol"

// ConsoleHost is what the console needs from the server; main wires the
// server in, so the console knows it by this interface alone.
type ConsoleHost interface {
	// RunLine runs one line as the server itself and answers the feedback,
	// refusals and parse errors in the order they came; a line that asked
	// for nothing answers nothing.
	RunLine(line string) []protocol.Text

	// Suggest completes text, the whole line left of the cursor with its
	// leading slash (as the client's tab completion request carries it),
	// answering where the replacement starts and the candidates.
	Suggest(text string) (start int, matches []string)

	// Stop stops the server as /stop does; Ctrl-C asks for it.
	Stop()
}
