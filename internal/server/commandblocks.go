package server

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/commandblock"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Command blocks' server half: the programming packet, the placement that
// starts a repeating loop, and the run (one command, then the chain ahead).
// Activation bookkeeping (rises, loop bookings, conditions) is the engine's
// in world/redstone.go. Commands run only on the tick loop's turn, so a
// block's command never wakes an engine mid-tick.

// Program Command Block's mode ordinals and flags bits.
// vanilla: ServerboundSetCommandBlockPacket, CommandBlockEntity.Mode
const (
	programChain     = 0
	programRepeating = 1
	programImpulse   = 2

	programFlagTrackOutput = 0x01
	programFlagConditional = 0x02
	programFlagAutomatic   = 0x04
)

// programBlockForMode is the block for the packet's mode: chain, repeating
// or impulse.
func programBlockForMode(mode int32) registry.Block {
	switch mode {
	case programChain:
		return registry.BlockChainCommandBlock
	case programRepeating:
		return registry.BlockRepeatingCommandBlock
	}
	return registry.BlockCommandBlock
}

// commandBlockModeOf is the mode a command block state runs as.
// vanilla: CommandBlockEntity.getMode
func commandBlockModeOf(block registry.Block) int32 {
	switch block {
	case registry.BlockRepeatingCommandBlock:
		return programRepeating
	case registry.BlockChainCommandBlock:
		return programChain
	}
	return programImpulse
}

// handleSetCommandBlock handles Program Command Block, the editor's Done
// button, for a creative gamemaster. The block switches to the chosen mode
// keeping its facing and entity (the programming lives on the entity), and
// the command, output tracking and always-active flag are stored there. A
// position without a command block entity is ignored, as vanilla's cast
// check does. vanilla: ServerGamePacketListenerImpl.handleSetCommandBlock
func (c *client) handleSetCommandBlock(r *protocol.Reader) error {
	x, y, z := r.ReadPosition()
	input := r.ReadString(32767)
	mode := r.ReadVarInt()
	flags := r.ReadU8()
	if err := r.Err(); err != nil {
		return c.decodeFailure("set_command_block", err)
	}
	// An ordinal outside the enum is one vanilla's readEnum rejects too.
	// vanilla: FriendlyByteBuf.readEnum
	if mode < programChain || mode > programImpulse {
		return c.decodeFailure("set_command_block",
			fmt.Errorf("set command block carries mode %d, which is not a command block mode", mode))
	}
	// Creative mode plus gamemaster permission, vanilla's check for the editor.
	// vanilla: Player.canUseGameMasterBlocks
	if !c.canUseGameMasterBlocks() {
		_ = c.Message(protocol.Text{Translate: "advMode.notAllowed"})
		return nil
	}

	w := c.theWorld()
	be := w.BlockEntity(x, y, z)
	if be == nil || be.Kind != world.KindCommandBlock {
		c.log.Debug("ignoring a set command block where no command block stands")
		return nil
	}

	// The state switch keeps the facing, swaps the mode's block as a
	// client-only write that wakes no neighbour, and keeps the entity.
	oldState := w.BlockAt(x, y, z)
	oldMode := commandBlockModeOf(registry.BlockOf(oldState))
	block := programBlockForMode(mode)
	conditional := "false"
	if flags&programFlagConditional != 0 {
		conditional = "true"
	}
	state, ok := registry.WithProps(block.DefaultState(),
		registry.PropValue{P: registry.PropFacing, V: blockFacingName(oldState)},
		registry.PropValue{P: registry.PropConditional, V: conditional},
	)
	if ok && state != oldState && w.SetBlock(x, y, z, state) {
		c.srv.markChanged(w, x, y, z)
	}

	trackOutput := flags&programFlagTrackOutput != 0
	automatic := flags&programFlagAutomatic != 0
	var book bool
	be.Use(func(b *world.BlockEntity) {
		b.Command = input
		b.SuccessCount = 0
		b.TrackOutput = trackOutput
		if !trackOutput {
			b.LastOutput = nil
		}
		// setAutomatic: a block just made always active and running by itself
		// books the loop's first visit, unless already powered (the booking reads
		// power before the packet could change it, using the placement's flag). A
		// switch to repeating books one regardless.
		// vanilla: CommandBlockEntity.setAutomatic, onModeSwitch
		previous := b.Automatic
		b.Automatic = automatic
		book = (!previous && automatic && !b.Powered && mode != programChain) ||
			(oldMode != mode && mode == programRepeating && (b.Powered || b.Automatic))
		w.TouchEntity(b)
	})
	if book {
		c.srv.physicsFor(w).ScheduleCommandBlock(x, y, z)
	}

	if input != "" {
		key := "advMode.setCommand.success"
		if !c.theWorld().BoolRule(world.RuleCommandBlocksWork) {
			key = "advMode.setCommand.disabled"
		}
		_ = c.Message(protocol.Text{Translate: key, With: []protocol.Text{{Text: input}}})
	}
	return nil
}

// canUseGameMasterBlocks reports creative mode with gamemaster permission.
// vanilla: Player.canUseGameMasterBlocks
func (c *client) canUseGameMasterBlocks() bool {
	return c.player.GameMode() == protocol.GameModeCreative && c.PermLevel() >= command.PermLevelGamemasters
}

// blockNameOfState is the state's block name, air for an unknown state.
func blockNameOfState(state uint16) string {
	return registry.BlockOf(state).Name()
}

// blockFacingName is the state's facing property, "" if it has none.
func blockFacingName(state uint16) string {
	return registry.StateValue(state, registry.PropFacing)
}

// commandBlockPlaced starts a placed command block. Without
// block_entity_data, output tracking follows send_command_feedback and only
// chain blocks are always active; with it, the loaded flags stay and the load
// books what commandBlockTagLoaded does. A neighbour signal that differs from
// the powered flag sets it, and a rise books a non-automatic block's run.
// vanilla: CommandBlock.setPlacedBy, CommandBlock.setPoweredAndUpdate
func (c *client) commandBlockPlaced(w *world.World, block registry.Block, x, y, z int32, stack protocol.ItemStack) {
	be := w.BlockEntity(x, y, z)
	if be == nil || be.Kind != world.KindCommandBlock {
		return
	}
	physics := c.srv.physicsFor(w)
	powered := physics.HasNeighborSignal(x, y, z)
	// vanilla checks only that the component is present.
	_, _, hasData := stack.BlockEntityDataOf()
	chain := block == registry.BlockChainCommandBlock
	if hasData {
		c.srv.commandBlockTagLoaded(w, x, y, z, chain) // the kind default is the previous flag
	}
	var book bool
	be.Use(func(b *world.BlockEntity) {
		if !hasData {
			b.TrackOutput = c.theWorld().BoolRule(world.RuleSendFeedback)
			b.Automatic = chain
		}
		if powered != b.Powered {
			b.Powered = powered
			// A chain block runs only from the block behind it, never its own tick.
			book = powered && !b.Automatic && !chain
		}
	})
	if book {
		physics.ScheduleCommandBlock(x, y, z)
	}
}

// commandBlockEntityStarted gives a fresh entity its kind's always-active
// default (chain on, the others off). It books nothing; only a rise or the
// programming packet starts a run.
// vanilla: CommandBlock.newBlockEntity, CommandBlockEntity.setAutomatic
func (s *Server) commandBlockEntityStarted(w *world.World, b registry.Block, x, y, z int32) {
	if b != registry.BlockChainCommandBlock {
		return
	}
	be := w.BlockEntity(x, y, z)
	if be == nil || be.Kind != world.KindCommandBlock {
		return
	}
	be.Use(func(b *world.BlockEntity) { b.Automatic = true })
}

// commandBlockAutoAt reads the always-active flag before a tag load
// overwrites it, false where no command block is.
func commandBlockAutoAt(w *world.World, x, y, z int32) bool {
	be := w.BlockEntity(x, y, z)
	if be == nil || be.Kind != world.KindCommandBlock {
		return false
	}
	var auto bool
	be.Use(func(b *world.BlockEntity) { auto = b.Automatic })
	return auto
}

// commandBlockTagLoaded books the run a tag load owes under setAutomatic: a
// non-chain block the load turns always active while unpowered starts
// itself. previousAuto is the flag before the load, which resets it.
// vanilla: CommandBlockEntity.loadAdditional, CommandBlockEntity.setAutomatic
func (s *Server) commandBlockTagLoaded(w *world.World, x, y, z int32, previousAuto bool) {
	block := registry.BlockOf(w.BlockAt(x, y, z))
	if block != registry.BlockCommandBlock && block != registry.BlockRepeatingCommandBlock {
		return
	}
	be := w.BlockEntity(x, y, z)
	if be == nil || be.Kind != world.KindCommandBlock {
		return
	}
	var book bool
	be.Use(func(b *world.BlockEntity) { book = !previousAuto && b.Automatic && !b.Powered })
	if book {
		s.physicsFor(w).ScheduleCommandBlock(x, y, z)
	}
}

// runCommandBlock performs one booked visit: the entity's command (none
// counts as nothing), then the chain it points into. Visits run on the tick
// loop after the engine's pass, so the commands are the tick's last change
// to the world. vanilla: CommandBlock.execute, CommandBlock.tick
func (s *Server) runCommandBlock(w *world.World, r world.CommandRun) {
	state := w.BlockAt(r.X, r.Y, r.Z)
	be := w.BlockEntity(r.X, r.Y, r.Z)
	if be == nil || be.Kind != world.KindCommandBlock {
		return
	}
	gameTime, _ := s.world.Time()
	// A block with no command counts nothing, and the chain behind still runs.
	empty := false
	be.Use(func(b *world.BlockEntity) {
		empty = b.Command == ""
		if empty {
			b.SuccessCount = 0
		}
	})
	if !empty {
		s.performCommandBlock(w, be, gameTime)
	}
	s.executeCommandChain(w, r.X, r.Y, r.Z, state)
}

// performCommandBlock runs one block's command. It returns false if the
// block already ran this game tick, where a chain walk stops; the run's
// success count and, while tracking output, its last line are the block's.
// vanilla: BaseCommandBlock.performCommand
func (s *Server) performCommandBlock(w *world.World, be *world.BlockEntity, gameTime int64) bool {
	enabled := w.BoolRule(world.RuleCommandBlocksWork)
	var step commandblock.Step
	var src *commandBlockSource
	var line string
	be.Use(func(b *world.BlockEntity) {
		step = b.Begin(gameTime, enabled)
		if step == commandblock.Run {
			line = b.Command
			src = s.newCommandBlockSource(w, be, b.TrackOutput)
		}
	})
	switch step {
	case commandblock.Gated:
		return false
	case commandblock.Answered:
		be.Use(func(b *world.BlockEntity) { w.TouchEntity(b) })
		return true
	}
	var success int
	var out *protocol.Text
	if src != nil {
		success = s.runBlockCommand(src, line)
		out = src.lastOutput
	}
	be.UseLive(func(b *world.BlockEntity) {
		b.End(gameTime, step, success, out)
		w.TouchEntity(b)
	})
	return true
}

// executeCommandChain walks the chain blocks the origin points into, each
// checking powered or always active, then its condition, until the walk
// leaves the chain, a run returns false (already ran this game tick) or
// max_command_sequence_length runs out.
// vanilla: CommandBlock.executeChain
func (s *Server) executeCommandChain(w *world.World, startX, startY, startZ int32, startState uint16) {
	dir := registry.StateFacing(startState)
	gameTime, _ := s.world.Time()
	limit := int(w.IntRule(world.RuleMaxCommandSequence))
	x, y, z := startX, startY, startZ
	steps := limit
	for steps > 0 {
		steps--
		x, y, z = x+protocol.FaceOffset[dir][0], y+protocol.FaceOffset[dir][1], z+protocol.FaceOffset[dir][2]
		state := w.BlockAt(x, y, z)
		if registry.BlockOf(state) != registry.BlockChainCommandBlock {
			break
		}
		be := w.BlockEntity(x, y, z)
		if be == nil || be.Kind != world.KindCommandBlock {
			break
		}
		var powered, automatic bool
		be.Use(func(b *world.BlockEntity) { powered, automatic = b.Powered, b.Automatic })
		// The engine's lock and the entity's must never be held together: the flags
		// are read first and the condition asked between the two holds of the
		// entity's lock.
		if !powered && !automatic {
			dir = registry.StateFacing(state)
			continue
		}
		met := s.physicsFor(w).CommandConditionMet(x, y, z, state)
		be.Use(func(b *world.BlockEntity) { b.ConditionMet = met })
		if met {
			if !s.performCommandBlock(w, be, gameTime) {
				break
			}
		} else if registry.StateBool(state, registry.PropConditional) {
			be.Use(func(b *world.BlockEntity) { b.SuccessCount = 0 })
		}
		dir = registry.StateFacing(state)
	}
	if steps <= 0 {
		slog.Warn(fmt.Sprintf("Command Block chain tried to execute more than %d steps!", max(limit, 0)))
	}
}

// runBlockCommand runs one command line as the block's source and returns
// its success count: one per successful command, an execute one per
// succeeding context, as vanilla's withCallback increments.
// vanilla: BaseCommandBlock.performCommand, Commands.performPrefixedCommand
func (s *Server) runBlockCommand(src *commandBlockSource, line string) int {
	line = strings.TrimPrefix(line, "/")
	tree := src.CommandTree()
	if !s.blockLines.accepted(tree, line) {
		command.CacheLineTokens(line)
	}
	tokens := command.Tokenize(line)
	if len(tokens) == 0 {
		return 0
	}
	node, fail := (*command.Node)(nil), (*command.Failure)(nil)
	if !s.blockLines.accepted(tree, line) {
		node, fail = command.Match(tree, line, tokens, true)
		if node != nil {
			s.blockLines.accept(tree, line)
		}
	}
	if fail != nil {
		// Refusals go to the block's output, as red lines and context from the
		// source's fail.
		if fail.Err != nil {
			src.fail(fail.Err.Key, fail.Err.With, line, fail.Err.Cursor, fail.Err.NoContext)
			return 0
		}
		if fail.UnknownCommand {
			cursor := 0
			if fail.At > 0 {
				cursor = len(line)
			}
			src.fail(commandUnknownCommandKey, nil, line, cursor, false)
			return 0
		}
		if fail.Trailing {
			src.fail(commandUnknownArgumentKey, nil, line, command.TrailingCursor(line, tokens), false)
			return 0
		}
		src.fail(commandUnknownArgumentKey, nil, line, tokens[fail.At].Start, false)
		return 0
	}
	words := make([]string, len(tokens))
	for i, t := range tokens {
		words[i] = t.Text
	}
	cmd, ok := command.LookupSource(src, words[0])
	if !ok || cmd.Run == nil {
		slog.Debug("unhandled command block command", "input", line)
		return 0
	}
	err := command.RunSafe(cmd, src, line, words[1:])
	if parseErr, ok := errors.AsType[*command.ParseError](err); ok {
		// A parse error goes to the source as a failure line and context, a keyed
		// one as its translation.
		if parseErr.Key != "" {
			src.fail(parseErr.Key, parseErr.With, line, parseErr.Cursor, parseErr.NoContext)
		} else {
			src.failure(protocol.Text{Text: parseErr.Text})
			if !parseErr.NoContext {
				src.record(commandErrorContext(line, parseErr.Cursor))
			}
		}
		return 0
	}
	switch cmd.Name {
	case "execute", "function", "return":
		if err == nil || errors.Is(err, command.ErrRefused) {
			// These report only through the callback: a fork counts one per
			// succeeding context, a function only if it returns, a return
			// fail none.
			// vanilla: BaseCommandBlock.performCommand, FunctionCommand.queueFunctionsNoReturn
			return src.successes
		}
	}
	if err != nil {
		// A refused command acted on nothing, which counts no success.
		if !errors.Is(err, command.ErrRefused) {
			slog.Error("command block command failed", "input", line, "error", err)
		}
		return 0
	}
	return 1
}

// commandBlockSource is a command block's runner: at the block, named for
// it, at vanilla's gamemaster permission. Feedback, failures and output go
// to the block's last output (none if it tracks none), success lines only
// with send_command_feedback on; no chat sees anything but broadcasts.
// vanilla: CommandBlockEntity.createCommandSourceStack,
// BaseCommandBlock.CloseableCommandBlockSource
type commandBlockSource struct {
	worldScope
	name        string
	pos         player.Position
	trackOutput bool
	lastOutput  *protocol.Text
	successes   int
	// self is @s: the minecart a cart's command runs as, nil for a block.
	self command.Entity
}

// Result counts one success the command reported through its callback.
func (src *commandBlockSource) Result(success bool, _ int32) {
	if success {
		src.successes++
	}
}

// newCommandBlockSource builds be's source in w: named for the block,
// centred on it, facing its facing property.
func (s *Server) newCommandBlockSource(w *world.World, be *world.BlockEntity, trackOutput bool) *commandBlockSource {
	src := &commandBlockSource{name: commandBlockName(be), trackOutput: trackOutput}
	src.worldScope = worldScope{
		srv: s,
		in:  func() *dimension { return s.dimensionOfWorld(w) },
		by:  src.Profile,
	}
	src.pos = player.Position{
		X: float64(be.X) + 0.5, Y: float64(be.Y) + 0.5, Z: float64(be.Z) + 0.5,
		Yaw: commandBlockFacingYaw(blockFacingName(w.BlockAt(be.X, be.Y, be.Z))),
	}
	return src
}

// commandBlockName is the block's run name: a placed item's custom name, or
// vanilla's "@".
func commandBlockName(be *world.BlockEntity) string {
	if be.CustomName != nil {
		if name := be.CustomName.Plain(); name != "" {
			return name
		}
	}
	return "@"
}

// commandBlockFacingYaw is the facing's yaw: horizontal quadrants split as a
// player's look does; vertical facings have none and read east's.
// vanilla: Direction.toYRot
func commandBlockFacingYaw(facing string) float32 {
	switch facing {
	case "south":
		return 0
	case "west":
		return 90
	case "north":
		return 180
	default:
		return 270
	}
}

// record keeps a message as the block's last line while it tracks output,
// prefixed with the server's local wall clock as "[HH:mm:ss] ".
// vanilla: CloseableCommandBlockSource.sendSystemMessage
func (src *commandBlockSource) record(text protocol.Text) {
	if !src.trackOutput {
		return
	}
	src.lastOutput = &protocol.Text{Text: "[" + time.Now().Format("15:04:05") + "] ", Extra: []protocol.Text{text}}
}

// Feedback and Error record a line only when the rule or the refusal
// allows. vanilla: CloseableCommandBlockSource.acceptsSuccess, acceptsFailure
func (src *commandBlockSource) Feedback(key string, with ...protocol.Text) error {
	return src.FeedbackText(protocol.Text{Translate: key, With: with})
}

func (src *commandBlockSource) FeedbackText(text protocol.Text) error {
	if !src.rules().BoolRule(world.RuleSendFeedback) {
		return nil
	}
	src.record(text)
	return nil
}

func (src *commandBlockSource) Message(text protocol.Text) error {
	src.record(text)
	return nil
}

func (src *commandBlockSource) Error(key string) error {
	src.failure(protocol.Text{Translate: key})
	return nil
}

func (src *commandBlockSource) ErrorWith(key string, with ...protocol.Text) error {
	src.failure(protocol.Text{Translate: key, With: with})
	return nil
}

// failure records the bare red message a command failure wears.
func (src *commandBlockSource) failure(message protocol.Text) {
	src.record(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{message}})
}

// fail records a parse failure's message and context line, as a player is
// sent. vanilla: Commands.finishParsing
func (src *commandBlockSource) fail(key string, with []string, input string, cursor int, noContext bool) {
	message := protocol.Text{Translate: key}
	for _, value := range with {
		message.With = append(message.With, protocol.Text{Text: value})
	}
	src.failure(message)
	if noContext {
		return
	}
	src.record(commandErrorContext(input, cursor))
}

// InformAdmins echoes the run to operators when the block tracks output and
// command_block_output allows. Without tracking vanilla uses a null source,
// whose shouldInformAdmins is false.
// vanilla: CloseableCommandBlockSource.shouldInformAdmins
func (src *commandBlockSource) InformAdmins(message protocol.Text) {
	src.informAdminsAs(src.DisplayName(), message)
}

func (src *commandBlockSource) informAdminsAs(name, message protocol.Text) {
	if !src.trackOutput || !src.rules().BoolRule(world.RuleCommandBlockOutput) {
		return
	}
	src.srv.informAdmins(uuid.Nil, false, name, message)
}

// Name is what the run logs and broadcasts under.
func (src *commandBlockSource) Name() string { return src.name }

func (src *commandBlockSource) DisplayName() protocol.Text {
	return protocol.Text{Text: src.name}
}

// Profile supplies the block's name to world operations' logs.
func (src *commandBlockSource) Profile() player.Profile {
	return player.Profile{UUID: uuid.Nil, Name: src.name}
}

func (src *commandBlockSource) ChatArgument(string) *chatsecurity.Message { return nil }

// PermLevel is vanilla's command block permission, gamemaster.
// vanilla: LevelBasedPermissionSet.GAMEMASTER
func (src *commandBlockSource) PermLevel() int { return command.PermLevelGamemasters }

// Debug and Info log from inside a command on the server's log.
func (src *commandBlockSource) Debug(msg string, args ...any) { slog.Debug(msg, args...) }

func (src *commandBlockSource) Info(msg string, args ...any) { slog.Info(msg, args...) }

// CommandTree is the tree of the block's own level.
func (src *commandBlockSource) CommandTree() *command.Node {
	return src.srv.pluginCommandTree(src, command.PermLevelGamemasters)
}

// SelfEntity is the running cart, nil from a block: @s names nobody there.
func (src *commandBlockSource) SelfEntity() command.Entity { return src.self }

// AnchorEyes is false: the block anchors at its own centre.
func (src *commandBlockSource) AnchorEyes() bool { return false }

// InContext returns the source an execution chain's tail runs as.
func (src *commandBlockSource) InContext(ctx command.Context) (command.Source, bool) {
	return execOver(src, src.worldScope, ctx)
}

// Position and EyePosition are the block's centre, the base for relative
// arguments and selector volumes.
func (src *commandBlockSource) Position() player.Position { return src.pos }

func (src *commandBlockSource) EyePosition() player.Position { return src.pos }

// OutgoingMessage records the outgoing direct-message feedback.
func (src *commandBlockSource) OutgoingMessage(_ protocol.Text, _ *chatsecurity.Message, to protocol.Text, resolved protocol.Text) {
	_ = src.Message(protocol.Text{
		Translate: "commands.message.display.outgoing",
		With:      []protocol.Text{to, resolved},
	})
}

// Silent reports whether nothing reaches the source's output.
func (src *commandBlockSource) Silent() bool { return false }

// maxBlockLines bounds blockLineCache.
const maxBlockLines = 4096

// blockLineCache remembers which command-block lines Match accepted against
// which command tree. The tree pointer changes on /reload and on plugin
// command changes, which drops every older entry.
type blockLineCache struct {
	mu    sync.Mutex
	lines map[blockLineKey]struct{}
}

type blockLineKey struct {
	tree *command.Node
	line string
}

func (c *blockLineCache) accepted(tree *command.Node, line string) bool {
	c.mu.Lock()
	_, ok := c.lines[blockLineKey{tree, line}]
	c.mu.Unlock()
	return ok
}

func (c *blockLineCache) accept(tree *command.Node, line string) {
	c.mu.Lock()
	if c.lines == nil || len(c.lines) >= maxBlockLines {
		c.lines = make(map[blockLineKey]struct{})
	}
	c.lines[blockLineKey{tree, line}] = struct{}{}
	c.mu.Unlock()
}
