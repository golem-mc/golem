package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
)

// InContext returns the source /execute runs a chain's tail as: this runner
// with the context's position, dimension and body. Permissions and feedback
// stay the runner's; modifiers change where and as whom, not who runs it.
func (c *client) InContext(ctx command.Context) (command.Source, bool) {
	return execOver(c, c.worldScope, ctx)
}

// AnchorEyes is false: a runner's own commands anchor at the feet.
func (c *client) AnchorEyes() bool { return false }

// findDimension is dimensionByName without the overworld fallback, so an
// unknown name fails an execute in step.
func (s *Server) findDimension(name string) *dimension {
	for _, d := range s.allDims() {
		if d.name == name {
			return d
		}
	}
	return nil
}

// execOver wraps runner in the context; home is the runner's own scope. ok
// is false for an unknown dimension, which the argument usually refuses
// first.
func execOver(runner command.Source, home worldScope, ctx command.Context) (command.Source, bool) {
	s := &execSource{runnerSource: runnerSource{runner}, worldScope: home}
	if ctx.HasPos {
		s.pos = ctx.Pos
		s.hasPos = true
	}
	if ctx.HasDim {
		dim := home.srv.findDimension(ctx.Dim)
		if dim == nil {
			return nil, false
		}
		s.dim = dim
		s.hasDim = true
		s.in = dim.here
		if s.in == nil {
			s.in = func() *dimension { return dim }
		}
	}
	if ctx.Executor != nil {
		s.executor = ctx.Executor
		s.hasExec = true
	}
	s.eyes = ctx.Eyes
	s.quiet = ctx.Quiet
	s.silent = ctx.Silent
	return s, true
}

// execSource is one execution's per-context view of a runner. The runner is
// embedded one level below worldScope so worldScope's methods, in the
// context's dimension, win.
type execSource struct {
	worldScope
	runnerSource
	pos      player.Position
	hasPos   bool
	dim      *dimension
	hasDim   bool
	executor command.Entity
	hasExec  bool
	eyes     bool
	quiet    bool
	silent   bool
}

type runnerSource struct{ command.Source }

// Position is the context's position, the base of relative arguments.
func (s *execSource) Position() player.Position {
	if s.hasPos {
		return s.pos
	}
	return s.Source.Position()
}

// EyePosition is the context's position plus the executor's eye height, or
// the runner's own with no context position.
func (s *execSource) EyePosition() player.Position {
	if s.hasPos {
		p := s.pos
		if s.hasExec {
			p.Y += s.executor.EyePosition().Y - s.executor.Position().Y
		}
		return p
	}
	return s.Source.EyePosition()
}

// AnchorEyes says the context is anchored at the eyes.
func (s *execSource) AnchorEyes() bool { return s.eyes }

// DisplayName is the name chat and the admin echo attribute the command to:
// the body an as or summon set, else the runner's.
// vanilla: CommandSourceStack.getDisplayName
func (s *execSource) DisplayName() protocol.Text {
	if s.hasExec {
		return s.executor.DisplayName()
	}
	return s.Source.DisplayName()
}

// BroadcastSay and BroadcastMe tell the world what the executing body said
// or does, over /say's and /me's chat types. The signing context stays the
// runner's through an as, so a signed player's command names their profile.
// vanilla: SayCommand.say, MeCommand, CommandSourceStack.withEntity
func (s *execSource) BroadcastSay(message *chatsecurity.Message, resolved protocol.Text) {
	s.srv.broadcastCommandChat("minecraft:say_command", s.DisplayName(), message, resolved, s.enqueueChatRelay)
}

func (s *execSource) BroadcastMe(message *chatsecurity.Message, resolved protocol.Text) {
	s.srv.broadcastCommandChat("minecraft:emote_command", s.DisplayName(), message, resolved, s.enqueueChatRelay)
}

// enqueueChatRelay keeps /execute's relays on the original connection's chain.
func (s *execSource) enqueueChatRelay(fn func()) {
	if runner, ok := s.Source.(interface{ enqueueChatRelay(func()) }); ok {
		runner.enqueueChatRelay(fn)
	} else {
		s.srv.plugins.Enqueue(fn)
	}
}

// OutgoingMessage sends the outgoing line to the executing player or, when
// the body is not a player, the decorated line to the runner as a system
// message. vanilla: CommandSourceStack.sendChatMessage
func (s *execSource) OutgoingMessage(from protocol.Text, message *chatsecurity.Message, to protocol.Text, resolved protocol.Text) {
	if !s.hasExec {
		s.Source.OutgoingMessage(from, message, to, resolved)
		return
	}
	if body, ok := s.executor.(command.Target); ok {
		body.OutgoingMessage(from, message, to, resolved)
		return
	}
	_ = s.Message(protocol.Text{
		Translate: "commands.message.display.outgoing",
		With:      []protocol.Text{to, resolved},
	})
}

// SelfEntity is @s: the body an as, at or summon set, or the runner's.
func (s *execSource) SelfEntity() command.Entity {
	if s.hasExec {
		return s.executor
	}
	return s.Source.SelfEntity()
}

// InContext layers a nested /execute's context over this one, the inner
// settings winning. A quiet (forked) or silent outer context stays so.
func (s *execSource) InContext(ctx command.Context) (command.Source, bool) {
	inner := ctx
	if !ctx.HasPos && s.hasPos {
		inner.Pos = s.pos
		inner.HasPos = true
	}
	if !ctx.HasDim && s.hasDim {
		inner.Dim = s.dim.name
		inner.HasDim = true
	}
	if ctx.Executor == nil && s.hasExec {
		inner.Executor = s.executor
	}
	if !ctx.Eyes {
		inner.Eyes = s.eyes
	}
	if s.quiet {
		inner.Quiet = true
	}
	if s.silent {
		inner.Silent = true
	}
	return s.Source.InContext(inner)
}

// ChatArgument retains the runner's signing context through execution modifiers.
// vanilla: CommandSourceStack.getSigningContext, used by MessageArgument
func (s *execSource) ChatArgument(name string) *chatsecurity.Message {
	return s.Source.ChatArgument(name)
}

// Result hands a result to the runner's sink, if it keeps one.
func (s *execSource) Result(success bool, result int32) {
	if sink, ok := s.Source.(command.ResultSink); ok {
		sink.Result(success, result)
	}
}

// Feedback sends the runner's success even after a fork, which changes the
// body commands act as, not who they answer to. A silent source (a
// function's) sends nothing.
func (s *execSource) Feedback(key string, with ...protocol.Text) error {
	return s.FeedbackText(protocol.Text{Translate: key, With: with})
}

func (s *execSource) FeedbackText(text protocol.Text) error {
	if s.silent {
		return nil
	}
	return s.Source.FeedbackText(text)
}

// Message sends the runner a message, nothing when silent.
func (s *execSource) Message(text protocol.Text) error {
	if s.silent {
		return nil
	}
	return s.Source.Message(text)
}

// InformAdmins echoes under the executing entity's name by the runner's
// broadcast rule (an as changes the entity, not shouldInformAdmins),
// leaving out the runner. A silent source echoes nothing.
// vanilla: CommandSourceStack.broadcastToAdmins
func (s *execSource) InformAdmins(message protocol.Text) {
	if s.silent {
		return
	}
	if e, ok := s.Source.(adminEchoer); ok {
		e.informAdminsAs(s.DisplayName(), message)
	}
}

// Error sends the runner's error, nothing when forked or silent.
func (s *execSource) Error(key string) error {
	if s.quiet || s.silent {
		return nil
	}
	return s.Source.Error(key)
}

// ErrorWith sends the runner's error with arguments, nothing when forked
// or silent.
func (s *execSource) ErrorWith(key string, with ...protocol.Text) error {
	if s.quiet || s.silent {
		return nil
	}
	return s.Source.ErrorWith(key, with...)
}

// Silent reports whether nothing reaches the source's output.
func (s *execSource) Silent() bool { return s.silent || s.Source.Silent() }

func (s *execSource) CommandOrigin() command.Source { return s.Source }
