package server

import (
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

type pluginConsole struct{ host *pluginHost }

func (h *pluginHost) Console() plugin.CommandSender { return pluginConsole{h} }
func (p pluginConsole) Name() string                { return consoleSourceName }
func (p pluginConsole) OpLevel() int                { return command.PermLevelOwners }
func (p pluginConsole) SendMessage(t plugin.Text)   { slog.Info(pluginProtocolText(t).Plain()) }
func (p pluginConsole) HasPermission(node string) bool {
	return p.host.srv.plugins.HasPermission(p, node)
}

func (p pluginConsole) PermissionSet(node string) bool {
	return p.host.srv.plugins.PermissionSet(p, node)
}

func (h *pluginHost) Dispatch(sender plugin.CommandSender, line string) (int, error) {
	var src command.Source
	var failure func(protocol.Text)
	switch sender := sender.(type) {
	case pluginPlayer:
		if sender.c.srv != h.srv || !sender.IsValid() {
			return 0, fmt.Errorf("plugin: invalid command sender")
		}
		src, failure = sender.c, sender.c.sendFailure
	case pluginConsole:
		if sender.host.srv != h.srv {
			return 0, fmt.Errorf("plugin: foreign console")
		}
		console := h.srv.consoleSource()
		src, failure = console, console.failure
		defer func() {
			for _, text := range console.out {
				t, _ := pluginText(text)
				sender.SendMessage(t)
			}
		}()
	default:
		if sender == nil {
			return 0, fmt.Errorf("plugin: nil command sender")
		}
		console := h.srv.newConsoleSource(sender.Name(), false)
		console.level = min(max(sender.OpLevel(), 0), command.PermLevelOwners)
		src = pluginSenderSource{consoleSource: console, sender: sender}
		failure = func(t protocol.Text) {
			_ = pluginSenderSource{sender: sender}.send(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{t}})
		}
	}
	line = strings.TrimPrefix(line, "/")
	return runSourceCommand(src, line, command.Tokenize(line), failure)
}

type pluginSenderSource struct {
	*consoleSource
	sender plugin.CommandSender
}

func (s pluginSenderSource) CommandTree() *command.Node {
	return s.srv.pluginCommandTree(s, s.PermLevel())
}

func (s pluginSenderSource) send(t protocol.Text) error {
	text, _ := pluginText(t)
	s.sender.SendMessage(text)
	return nil
}
func (s pluginSenderSource) Feedback(key string, with ...protocol.Text) error {
	return s.send(protocol.Text{Translate: key, With: with})
}
func (s pluginSenderSource) FeedbackText(t protocol.Text) error { return s.send(t) }
func (s pluginSenderSource) Message(t protocol.Text) error      { return s.send(t) }
func (s pluginSenderSource) Error(key string) error             { return s.ErrorWith(key) }
func (s pluginSenderSource) ErrorWith(key string, with ...protocol.Text) error {
	return s.send(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{{Translate: key, With: with}}})
}
func (s pluginSenderSource) InContext(ctx command.Context) (command.Source, bool) {
	return execOver(s, s.worldScope, ctx)
}

func (p pluginPlayer) RunCommand(line string) bool {
	if !p.IsValid() {
		return false
	}
	_, err := p.host.Dispatch(p, line)
	return err == nil
}

func (c *client) commandTreePacket(level int) []byte {
	tree := c.srv.pluginCommandTree(c, level)
	if r := c.srv.plugins; r != nil && r.Has(pluginruntime.PlayerCommandSend) {
		e := &plugin.PlayerCommandSendEvent{Player: pluginPlayer{c: c}}
		r.Fire(e)
		if len(e.Hide) > 0 {
			tree = command.FilterRoots(tree, func(n *command.Node) bool {
				return !slices.Contains(e.Hide, n.Name)
			})
		}
	}
	return command.Encode(tree)
}

func (s *Server) runPluginLine(src *consoleSource, line string, address net.Addr, hook int) []protocol.Text {
	r := s.plugins
	if r == nil || (!s.pluginCommands.active.Load() && !r.Has(hook) && !r.Has(pluginruntime.PlayerCommandSend)) {
		return s.runLine(src, line)
	}
	return s.runPluginCommandLine(src, line, address, hook)
}

func (s *Server) runPluginCommandLine(src *consoleSource, line string, address net.Addr, hook int) []protocol.Text {
	r := s.plugins
	r.Call(func() {
		line = strings.TrimPrefix(line, "/")
		if r.Has(hook) {
			if hook == pluginruntime.ServerCommand {
				e := &plugin.ServerCommandEvent{Command: line}
				r.Fire(e)
				if e.Cancelled() {
					return
				}
				line = e.Command
			} else {
				e := &plugin.RemoteServerCommandEvent{Command: line, Address: address}
				r.Fire(e)
				if e.Cancelled() {
					return
				}
				line = e.Command
			}
		}
		s.runLine(src, line)
	})
	return src.out
}
