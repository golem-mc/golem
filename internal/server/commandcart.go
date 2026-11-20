package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/commandblock"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// runCommandCart performs the cart's command as the cart.
// vanilla: BaseCommandBlock.performCommand
func (s *Server) runCommandCart(it *minecartEntity) {
	gameTime, _ := s.world.Time()
	enabled := it.w.BoolRule(world.RuleCommandBlocksWork)
	s.minecartsMu.Lock()
	cc := it.command
	if cc == nil || it.cart.Dead {
		s.minecartsMu.Unlock()
		return
	}
	step := cc.Begin(gameTime, enabled)
	var line string
	var src *commandBlockSource
	if step == commandblock.Run {
		line = cc.Command
		src = s.newCommandCartSource(it, cc.TrackOutput)
	}
	s.minecartsMu.Unlock()
	if step == commandblock.Gated || step == commandblock.Answered {
		return
	}

	success := 0
	if src != nil {
		success = s.runBlockCommand(src, line)
	}
	var out *protocol.Text
	if src != nil {
		out = src.lastOutput
	}
	s.minecartsMu.Lock()
	cc.End(gameTime, step, success, out)
	s.minecartsMu.Unlock()
}

// newCommandCartSource is the runner the cart's command runs as: at the
// cart, facing its yaw, @s the cart itself, at gamemaster permission. Called
// under the cart lock.
// vanilla: MinecartCommandBlock.MinecartCommandBase.createCommandSourceStack
func (s *Server) newCommandCartSource(it *minecartEntity, trackOutput bool) *commandBlockSource {
	m := it.cart
	name := "@"
	if text, ok := protocol.StyledTextOf(nbtOfNetwork(it.custom.wire)); ok && text.Plain() != "" {
		name = text.Plain()
	}
	src := &commandBlockSource{name: name, trackOutput: trackOutput}
	src.worldScope = worldScope{
		srv: s,
		in:  func() *dimension { return s.dimensionOfWorld(it.w) },
		by:  src.Profile,
	}
	src.pos = player.Position{X: m.X, Y: m.Y, Z: m.Z, Yaw: m.Yaw}
	if snap := s.minecartSnapshot(it, nil); snap != nil {
		src.self = snap
	}
	return src
}

func nbtOfNetwork(data string) nbt.Tag {
	if data == "" {
		return nil
	}
	tag, _, _ := nbt.DecodeNetwork([]byte(data))
	return tag
}

// handleSetCommandMinecart handles Program Command Minecart, the edit
// screen's Done button, for a creative gamemaster.
// vanilla: ServerGamePacketListenerImpl.handleSetCommandMinecart
func (c *client) handleSetCommandMinecart(r *protocol.Reader) error {
	id := r.ReadVarInt()
	input := r.ReadString(32767)
	trackOutput := r.ReadBool()
	if err := r.Err(); err != nil {
		return c.decodeFailure("set_command_minecart", err)
	}
	if !c.canUseGameMasterBlocks() {
		_ = c.Message(protocol.Text{Translate: "advMode.notAllowed"})
		return nil
	}
	it := c.srv.minecartByEntityID(c.theWorld(), id)
	if it == nil {
		return nil
	}
	enabled := c.theWorld().BoolRule(world.RuleCommandBlocksWork)
	c.srv.minecartsMu.Lock()
	cc := it.command
	if cc != nil {
		cc.Program(input, trackOutput)
	}
	c.srv.minecartsMu.Unlock()
	if cc != nil && input != "" {
		key := "advMode.setCommand.success"
		if !enabled {
			key = "advMode.setCommand.disabled"
		}
		_ = c.Message(protocol.Text{Translate: key, With: []protocol.Text{{Text: input}}})
	}
	return nil
}
