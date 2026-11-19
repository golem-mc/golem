package server

import (
	"errors"
	"fmt"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

// Translation keys for command parse errors.
// vanilla: BrigadierExceptions, Commands.finishParsing
const (
	commandUnknownCommandKey  = "command.unknown.command"
	commandUnknownArgumentKey = "command.unknown.argument"
	commandContextHereKey     = "command.context.here"
)

// sendJoiningCommands hands the connection's tree event to the tick before writing it.
func (c *client) sendJoiningCommands() error {
	if r := c.srv.plugins; r != nil && (c.srv.pluginCommands.active.Load() || r.Has(pluginruntime.PlayerCommandSend)) {
		var data []byte
		if !r.Call(func() { data = c.commandTreePacket(c.PermLevel()) }) {
			return errors.New("server stopping")
		}
		return c.writePacket(protocol.IDPlayCommands, "commands", data)
	}
	return c.writePacket(protocol.IDPlayCommands, "commands",
		command.Encode(c.srv.pluginCommandTree(c, c.PermLevel())))
}

// maxArgumentSignatures is the signed chat command's argument signature
// array limit (8).
const maxArgumentSignatures = 8

// handleChatCommand runs a client command without its leading slash.
// Signed arguments share the typed-chat decoder and acknowledgement window.
func (c *client) handleChatCommand(r *protocol.Reader, signed bool, packet string) error {
	if err := c.awaitChatRelay(); err != nil {
		return err
	}
	input := r.ReadString(32767)
	if err := r.Err(); err != nil {
		return c.decodeFailure(packet, err)
	}
	// Only a chat-session client's packet ends in an acknowledgement; the
	// unsigned form is the command alone.
	acknowledges := true
	var timestamp, salt int64
	var entries []chatsecurity.ArgumentSignature
	var lastSeen []chatsecurity.Signature
	if signed {
		timestamp, salt = r.ReadI64(), r.ReadI64()
		count := r.ReadVarInt()
		if count < 0 || count > maxArgumentSignatures {
			return c.decodeFailure(packet,
				fmt.Errorf("signed chat command carries %d argument signatures, at most %d", count, maxArgumentSignatures))
		}
		for range count {
			entry := chatsecurity.ArgumentSignature{Name: r.ReadString(16)}
			for i := range entry.Signature {
				entry.Signature[i] = r.ReadU8()
			}
			entries = append(entries, entry)
		}
		lastSeen, acknowledges = c.readChatUpdate(r)
		if err := r.Err(); err != nil {
			return c.decodeFailure(packet, err)
		}
	}
	// A kick is already on its way: the rest of a burst already read is not run
	// or counted again.
	if c.kicked.Load() {
		return nil
	}
	// The ack is validated after the kicked guard, so a burst past the threshold
	// brings one Disconnect, not one per packet.
	if !acknowledges {
		return c.disconnectChatValidation()
	}
	// vanilla: ServerGamePacketListenerImpl.tryHandleChat
	if ch, ok := illegalChatCharacter(input); ok {
		return c.disconnect(fmt.Errorf("chat command contains %q, which is not allowed", ch), protocol.Text{Text: illegalCharactersShown})
	}

	tokens := command.Tokenize(input)
	// Every command packet counts, even an empty one: vanilla counts packets,
	// not matched commands. The count runs after the command, so a kick lands
	// after its output. vanilla: ServerGamePacketListenerImpl.handleChatCommand
	defer c.detectCommandRateSpam()
	level := c.PermLevel()
	ranges, _ := command.SignableArguments(command.ForLevel(level), input, tokens, level >= command.PermLevelGamemasters)
	var arguments chatsecurity.Arguments
	if !signed && c.srv.Config.EnforcesSecureChat() && len(ranges) > 0 {
		c.sendFailure(protocol.Text{Translate: string(chatsecurity.InvalidCommandSignature)})
		return nil
	}
	if signed && !c.srv.Config.PreventChatReports {
		var err error
		parsed := make([]chatsecurity.Argument, len(ranges))
		for i, argument := range ranges {
			parsed[i] = chatsecurity.Argument{Name: argument.Name, Value: input[argument.Start:argument.End]}
		}
		c.srv.chatOrderMu.Lock()
		if c.chatDecoder == nil {
			c.chatDecoder = chatsecurity.NewDecoder(c.player.UUID, nil, c.srv.Config.EnforcesSecureChat())
		}
		arguments, err = c.chatDecoder.CollectArguments(parsed, entries, timestamp, salt, lastSeen, time.Now())
		c.srv.chatOrderMu.Unlock()
		if err != nil {
			c.sendFailure(protocol.Text{Translate: err.Error()})
			return nil
		}
	}
	if len(tokens) > 0 {
		if r := c.srv.plugins; r != nil && (c.srv.pluginCommands.active.Load() || r.Has(pluginruntime.PlayerCommand) || r.Has(pluginruntime.PlayerCommandSend)) {
			return c.runPluginChatCommand(input, arguments)
		}
		return c.runChatCommand(command.WithChatArguments(c, arguments), input, tokens)
	}
	return nil
}

// runPluginChatCommand keeps event captures off the inactive path's stack.
func (c *client) runPluginChatCommand(input string, arguments chatsecurity.Arguments) error {
	var err error
	r := c.srv.plugins
	r.Call(func() {
		if r.Has(pluginruntime.PlayerCommand) {
			e := &plugin.PlayerCommandEvent{Player: pluginPlayer{c: c}, Command: input}
			r.Fire(e)
			if e.Cancelled() {
				return
			}
			if input != e.Command {
				arguments = chatsecurity.Arguments{}
			}
			input = e.Command
		}
		err = c.runChatCommand(command.WithChatArguments(c, arguments), input, command.Tokenize(input))
	})
	return err
}

// runChatCommand matches and dispatches the command, sending parse
// refusals.
func (c *client) runChatCommand(src command.Source, input string, tokens []command.Token) error {
	_, err := runSourceCommand(src, input, tokens, c.sendFailure)
	if errors.Is(err, command.ErrRefused) {
		return nil
	}
	return err
}

// runSourceCommand shares parsing, results and failure presentation across senders.
// vanilla: Commands.finishParsing, Commands.performCommand
func runSourceCommand(src command.Source, input string, tokens []command.Token, failure func(protocol.Text)) (int, error) {
	parseFailure := func(key string, with []string, cursor int, noContext bool) (int, error) {
		message := protocol.Text{Translate: key}
		for _, value := range with {
			message.With = append(message.With, protocol.Text{Text: value})
		}
		failure(message)
		if !noContext {
			failure(commandErrorContext(input, cursor))
		}
		return 0, command.ErrRefused
	}
	cmd, fail := command.ParseCommand(src.CommandTree(), input, tokens, src.PermLevel() >= command.PermLevelGamemasters)
	if cmd.Run == nil {
		if fail.Err != nil {
			return parseFailure(fail.Err.Key, fail.Err.With, fail.Err.Cursor, fail.Err.NoContext)
		}
		if fail.UnknownCommand {
			cursor := 0
			if fail.At > 0 {
				cursor = len(input)
			}
			return parseFailure(commandUnknownCommandKey, nil, cursor, false)
		}
		if fail.Trailing {
			return parseFailure(commandUnknownArgumentKey, nil, command.TrailingCursor(input, tokens), false)
		}
		return parseFailure(commandUnknownArgumentKey, nil, tokens[fail.At].Start, false)
	}
	words := make([]string, len(tokens))
	for i, t := range tokens {
		words[i] = t.Text
	}
	result, err := command.ExecSafe(cmd, src, input, words[1:])
	if fail, ok := errors.AsType[*command.ParseError](err); ok {
		if fail.Key == "" {
			failure(protocol.Text{Text: fail.Text})
			failure(commandErrorContext(input, fail.Cursor))
			return 0, command.ErrRefused
		}
		return parseFailure(fail.Key, fail.With, fail.Cursor, fail.NoContext)
	}
	return int(result), err
}

// commandErrorContext is the gray context line: up to 10 characters before
// the cursor, the rest in red underline, then "<--[HERE]"; clicking it puts
// the command back in the chat box.
func commandErrorContext(input string, cursor int) protocol.Text {
	cursor = min(cursor, len(input))

	var context protocol.Text
	context.ClickCommand = "/" + input
	context.Color = protocol.ColorGray
	if cursor > 10 {
		context.Extra = append(context.Extra, protocol.Text{Text: "..."})
	}
	preStart := max(0, cursor-10)
	if preStart < cursor {
		context.Extra = append(context.Extra, protocol.Text{Text: input[preStart:cursor]})
	}
	if cursor < len(input) {
		context.Extra = append(context.Extra, protocol.Text{
			Text:      input[cursor:],
			Color:     protocol.ColorRed,
			Underline: true,
		})
	}
	context.Extra = append(context.Extra, protocol.Text{
		Translate: commandContextHereKey,
		Color:     protocol.ColorRed,
		Italic:    true,
	})
	return context
}

// sendFailure wraps the message in a red component and sends it via the
// mailbox like success feedback, so the two keep their order.
// vanilla: CommandSourceStack.sendFailure
func (c *client) sendFailure(message protocol.Text) {
	var b protocol.Buffer
	b.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{message}})
	b.PutBool(false) // in the chat window, not over the hotbar
	c.send(protocol.IDPlaySystemChat, b.Bytes())
}

// handleCommandSuggestion answers a tab-completion request for the text
// left of the cursor (with the slash).
func (c *client) handleCommandSuggestion(r *protocol.Reader) error {
	id := r.ReadVarInt()
	text := r.ReadString(32500)
	if err := r.Err(); err != nil {
		return c.decodeFailure("command_suggestion", err)
	}

	var at int
	var matches []string
	if c.srv.pluginCommands.active.Load() {
		c.srv.plugins.Call(func() { at, matches = command.Suggest(c.CommandTree(), text, c) })
	} else {
		at, matches = command.Suggest(c.CommandTree(), text, c)
	}

	// Brigadier's empty Suggestions have the range at 0, whatever was asked.
	start, length := 0, 0
	if len(matches) > 0 {
		start, length = command.SuggestionRange(text, at)
	}

	var b protocol.Buffer
	b.PutVarInt(id)
	b.PutVarInt(int32(start))
	b.PutVarInt(int32(length))
	b.PutVarInt(int32(len(matches)))
	for _, match := range matches {
		b.PutString(match)
		b.PutBool(false) // no tooltip
	}
	return c.writePacket(protocol.IDPlayCommandSuggestions, "command suggestions", b.Bytes())
}
