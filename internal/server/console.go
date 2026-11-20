package server

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
)

// RunLine runs one console line as the console source (owner level, at the
// overworld spawn). The leading slash is optional; parse errors are
// answered as for a player. vanilla: Commands.performPrefixedCommand
func (s *Server) RunLine(line string) []protocol.Text {
	return s.runPluginLine(s.consoleSource(), line, nil, pluginruntime.ServerCommand)
}

// RunRconLine runs one remote console line as vanilla's Rcon source: owner
// level, at the overworld spawn, under the name Rcon.
// vanilla: RconConsoleSource
func (s *Server) RunRconLine(line string) []protocol.Text {
	return s.runPluginLine(s.rconSource(), line, nil, pluginruntime.RemoteServerCommand)
}

// runLine runs one line as src. A blank line is refused as vanilla's
// dispatcher does, at the cursor with no input. vanilla: Commands.performCommand
func (s *Server) runLine(src *consoleSource, line string) []protocol.Text {
	return s.runStrippedLine(src, strings.TrimPrefix(line, "/"))
}

// runStrippedLine is runLine for a line whose one leading slash is already
// removed, so "//set" reaches the command "/set" once.
func (s *Server) runStrippedLine(src *consoleSource, line string) []protocol.Text {
	if _, err := runSourceCommand(src, line, command.Tokenize(line), src.failure); err != nil && !errors.Is(err, command.ErrRefused) {
		slog.Error("console command failed", "input", line, "error", err)
	}
	return src.out
}

// Suggest completes text against the owner-level command tree.
func (s *Server) Suggest(text string) (int, []string) {
	src := s.consoleSource()
	if !s.pluginCommands.active.Load() {
		return command.Suggest(src.CommandTree(), text, src)
	}
	var at int
	var matches []string
	s.plugins.Call(func() { at, matches = command.Suggest(src.CommandTree(), text, src) })
	return at, matches
}

// Stop is RequestStop, under the name the console calls (Ctrl-C).
func (s *Server) Stop() { s.RequestStop() }

// RequestStop stops the listener so Listen returns; only the first call
// counts.
func (s *Server) RequestStop() {
	s.stopOnce.Do(func() { close(s.stopReq) })
}

// StopRequested returns the channel RequestStop closes.
func (s *Server) StopRequested() <-chan struct{} { return s.stopReq }
