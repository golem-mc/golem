package server

import (
	"log/slog"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// consoleSourceName is vanilla's name for the server's command source.
const consoleSourceName = "Server"

// rconSourceName is vanilla's name for the remote console's command source.
const rconSourceName = "Rcon"

// consoleSource is a console-like source: the server console (named Server,
// whose admin echo is never logged) or vanilla's remote console (named
// Rcon, echo per broadcast-rcon-to-ops). It stands at the overworld spawn
// with owner permission. Answers are collected for the caller to print;
// messages meant for a body are dropped.
// vanilla: net.minecraft.server.rcon.RconConsoleSource
type consoleSource struct {
	worldScope
	// name is the source's name as feedback and chat spell it.
	name string
	// rcon marks the remote console, whose admin echo follows
	// broadcast-rcon-to-ops and is logged, unlike the console's.
	rcon bool
	// level is the permission level the source runs commands at.
	level int
	// spawn is the world spawn the source stands at, read with its
	// dimension so a concurrent /setworldspawn cannot split them.
	spawn world.SpawnPos
	// out collects feedback, refusals and parse errors in order.
	out []protocol.Text
}

func (s *Server) consoleSource() *consoleSource {
	return s.newConsoleSource(consoleSourceName, false)
}

// rconSource is vanilla's Rcon command source for remote console lines.
// vanilla: RconConsoleSource.createCommandSourceStack
func (s *Server) rconSource() *consoleSource {
	return s.newConsoleSource(rconSourceName, true)
}

// gameLoopSource is scheduled functions' source: the server at gamemaster
// level. vanilla: ServerFunctionManager.getGameLoopSender
func (s *Server) gameLoopSource() *consoleSource {
	src := s.consoleSource()
	src.level = command.PermLevelGamemasters
	return src
}

// newConsoleSource builds a console-like source at the world spawn as it is
// now, bound to itself so logs name it.
// vanilla: MinecraftServer.createCommandSourceStack
func (s *Server) newConsoleSource(name string, rcon bool) *consoleSource {
	dim, spawn := s.worldSpawn()
	src := &consoleSource{name: name, rcon: rcon, level: command.PermLevelOwners, spawn: spawn}
	src.worldScope = worldScope{srv: s, in: func() *dimension { return dim }, by: src.Profile}
	return src
}

// position is the world spawn's block corner.
func (src *consoleSource) position() player.Position {
	return player.Position{X: float64(src.spawn.X), Y: float64(src.spawn.Y), Z: float64(src.spawn.Z)}
}

// Name is the runner's name as feedback spells it.
func (src *consoleSource) Name() string { return src.name }

// DisplayName is the plain word Server, or Rcon for the remote console.
func (src *consoleSource) DisplayName() protocol.Text {
	return protocol.Text{Text: src.name}
}

// Profile is the nil UUID with the source's name.
func (src *consoleSource) Profile() player.Profile {
	return player.Profile{UUID: uuid.Nil, Name: src.name}
}

func (src *consoleSource) ChatArgument(string) *chatsecurity.Message { return nil }

// Position is where the console stands.
func (src *consoleSource) Position() player.Position { return src.position() }

// EyePosition is the position; the console has no body.
func (src *consoleSource) EyePosition() player.Position { return src.position() }

// PermLevel is the owner level unless the source was built for the game loop.
func (src *consoleSource) PermLevel() int { return src.level }

// Debug and Info log to the server log, which the console shows.
func (src *consoleSource) Debug(msg string, args ...any) { slog.Debug(msg, args...) }

func (src *consoleSource) Info(msg string, args ...any) { slog.Info(msg, args...) }

// Feedback collects a translated line; the console gets every success
// regardless of send_command_feedback.
func (src *consoleSource) Feedback(key string, with ...protocol.Text) error {
	return src.FeedbackText(protocol.Text{Translate: key, With: with})
}

func (src *consoleSource) FeedbackText(text protocol.Text) error {
	src.out = append(src.out, text)
	return nil
}

// InformAdmins broadcasts a success to operators. The remote console's echo
// follows broadcast-rcon-to-ops and is logged (it is not the server); the
// console's follows broadcast-console-to-ops and is not.
// vanilla: RconConsoleSource.shouldInformAdmins, DedicatedServer.shouldInformAdmins
func (src *consoleSource) InformAdmins(message protocol.Text) {
	src.informAdminsAs(src.DisplayName(), message)
}

func (src *consoleSource) informAdminsAs(name, message protocol.Text) {
	if src.rcon && !src.srv.Config.BroadcastRconToOps {
		return
	}
	if !src.rcon && !src.srv.Config.BroadcastConsoleToOps {
		return
	}
	src.srv.informAdmins(uuid.Nil, src.rcon, name, message)
}

// Message collects a finished message, as /help sends its lines.
func (src *consoleSource) Message(text protocol.Text) error {
	src.out = append(src.out, text)
	return nil
}

// Error collects the bare message a command fails with.
func (src *consoleSource) Error(key string) error {
	src.failure(protocol.Text{Translate: key})
	return nil
}

// ErrorWith collects a message with its %s arguments.
func (src *consoleSource) ErrorWith(key string, with ...protocol.Text) error {
	src.failure(protocol.Text{Translate: key, With: with})
	return nil
}

// failure collects a failure in red, as a player is sent one.
// vanilla: CommandSourceStack.sendFailure
func (src *consoleSource) failure(message protocol.Text) {
	src.out = append(src.out, protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{message}})
}

// fail collects a parse failure's message and context line, the latter
// omitted for a refusal with no input.
func (src *consoleSource) fail(key string, with []string, input string, cursor int, noContext bool) {
	message := protocol.Text{Translate: key}
	for _, value := range with {
		message.With = append(message.With, protocol.Text{Text: value})
	}
	src.failure(message)
	if noContext {
		return
	}
	src.failure(commandErrorContext(input, cursor))
}

// CommandTree is the tree of the source's level.
func (src *consoleSource) CommandTree() *command.Node {
	return src.srv.pluginCommandTree(src, src.level)
}

// SelfEntity is nil: @s names nobody from the console.
func (src *consoleSource) SelfEntity() command.Entity { return nil }

// AnchorEyes is false: the console anchors at its position.
func (src *consoleSource) AnchorEyes() bool { return false }

// InContext returns the source an /execute chain's tail runs as.
func (src *consoleSource) InContext(ctx command.Context) (command.Source, bool) {
	return execOver(src, src.worldScope, ctx)
}

// OutgoingMessage collects the console's /msg as the outgoing chat type's
// line, which names only the target, so sender and raw text go unused.
// vanilla: CommandSourceStack.sendChatMessage
func (src *consoleSource) OutgoingMessage(_ protocol.Text, _ *chatsecurity.Message, to protocol.Text, resolved protocol.Text) {
	src.out = append(src.out, protocol.Text{
		Translate: "commands.message.display.outgoing",
		With:      []protocol.Text{to, resolved},
	})
}

// Silent reports whether nothing reaches the source's output.
func (src *consoleSource) Silent() bool { return false }
