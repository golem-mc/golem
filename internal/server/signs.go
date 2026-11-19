package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Signs: the editor a placement or right-click opens, the Update Sign that
// writes it, and held dye, glow ink, ink and honeycomb. The text lives in
// the saved block entity (internal/server/world/blockentity.go); this is the wire
// around it. vanilla: SignBlock, SignItem, SignBlockEntity

// signLineMax is the editor's wire line limit, the String length each
// Update Sign line has.
const signLineMax = 384

// signPlaced gives a placed sign its block entity: the stack's data, then
// its sign_text_front, sign_text_back and waxed over it (every sign item
// carries both sides, empty by default), sent when the sign holds any text,
// dye, glow or wax. The placer's editor then opens on the front, the side
// facing them, unless the sign is waxed or a front line is not plain text.
// vanilla: BlockItem.place, SignBlockEntity.applyImplicitComponents,
// SignBlock.setPlacedBy
func (c *client) signPlaced(b registry.Block, x, y, z int32, stack protocol.ItemStack) {
	kind, ok := world.SignEntityKind(b)
	if !ok {
		return
	}
	w := c.theWorld()
	// A new sign must not inherit the cell's old block entity's text and wax.
	w.RemoveBlockEntity(x, y, z)
	be := w.EnsureBlockEntity(x, y, z, kind)
	if be == nil {
		return
	}
	var written, editable bool
	typeID, tag, hasData := stack.BlockEntityDataOf()
	hasData = hasData && typeID == be.TypeID() && (!world.OpOnlyKind(kind) || c.canUseGameMasterBlocks())
	if hasData {
		tag = w.ResolveSignTag(x, y, z, tag)
	}
	be.Use(func(b *world.BlockEntity) {
		if hasData {
			b.LoadBlockEntityData(typeID, tag)
		}
		b.ApplySignComponents(stack)
		blank := world.SignText{Color: "black"}
		written = b.Waxed || b.SignFront.HasMessage() || b.SignBack.HasMessage() ||
			b.SignFront.Color != blank.Color || b.SignBack.Color != blank.Color ||
			b.SignFront.Glowing || b.SignBack.Glowing
		editable = !b.Waxed && b.SignFront.Editable()
	})
	if written {
		// Send the laid text: the block update had none, and the placer's client
		// does not see the server's copy.
		c.srv.broadcastBlockEntity(w, x, y, z, be)
	}
	if editable {
		c.openSignEditor(x, y, z, true)
	}
}

type signResolver struct {
	s *Server
	w *world.World
}

// vanilla: SignBlockEntity.resolveLine, createCommandSourceStack(null, ...)
func (r signResolver) ResolveSignLine(x, y, z int32, line nbt.Tag) nbt.Tag {
	src := &commandBlockSource{name: "Sign"}
	src.worldScope = worldScope{srv: r.s, in: func() *dimension { return r.s.dimensionOfWorld(r.w) }, by: src.Profile}
	src.pos = player.Position{X: float64(x) + 0.5, Y: float64(y) + 0.5, Z: float64(z) + 0.5}
	return command.ResolveComponent(src, line)
}

// openSignEditor gives this player alone the edit of the sign (displacing
// any previous holder) and opens their editor for the side, after the block
// update. vanilla: SignBlock.openTextEdit, ServerPlayer.openTextEdit
func (c *client) openSignEditor(x, y, z int32, front bool) {
	w := c.theWorld()
	be := w.BlockEntity(x, y, z)
	if be == nil || !world.IsSignKind(be.Kind) {
		return
	}
	be.Use(func(b *world.BlockEntity) { b.Editor = c.player.UUID })
	c.send(protocol.IDPlayBlockUpdate, blockUpdate(x, y, z, w.BlockAt(x, y, z)))
	c.send(protocol.IDPlayOpenSignEditor, openSignEditorPayload(x, y, z, front))
}

// openSignEditorPayload builds Open Sign Editor: the position and side.
func openSignEditorPayload(x, y, z int32, front bool) []byte {
	var b protocol.Buffer
	b.PutPosition(x, y, z)
	b.PutBool(front)
	return b.Bytes()
}

// clearFarSignEditor revokes a sign's edit once its holder disconnected,
// changed dimension or walked out of range, as vanilla's SignBlockEntity.tick
// does, so placement's permission does not outlast leaving.
// vanilla: SignBlockEntity.tick, SignBlockEntity.playerIsTooFarAwayToEdit
func (s *Server) clearFarSignEditor(w *world.World, be *world.BlockEntity) {
	var editor uuid.UUID
	be.Use(func(b *world.BlockEntity) { editor = b.Editor })
	if editor == uuid.Nil {
		return
	}
	s.mu.RLock()
	c := s.clients[editor]
	s.mu.RUnlock()
	if c != nil && c.theWorld() == w {
		pos := c.player.Position()
		if rules.WithinContainerRange(c.blockRange(),
			pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z, float64(be.X), float64(be.Y), float64(be.Z)) {
			return
		}
	}
	be.Use(func(b *world.BlockEntity) { b.ReleaseSignEditor(editor) })
}

// handleSignUpdate writes one side's four lines from the editor, stripping
// §-codes. It applies only from the edit's holder, in reach, on an unwaxed
// sign; anything else is ignored.
// vanilla: ServerGamePacketListenerImpl.handleSignUpdate,
// SignBlockEntity.updateSignText, SignBlockEntity.playerIsTooFarAwayToEdit
func (c *client) handleSignUpdate(r *protocol.Reader) error {
	x, y, z := r.ReadPosition()
	var lines [4]string
	for i := range lines {
		lines[i] = rules.StripFormatting(r.ReadString(signLineMax))
	}
	slot := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return c.decodeFailure("sign_update", err)
	}
	front := slot == 1 // any other slot is the back. vanilla: SignTextSlot.BY_ID
	w := c.theWorld()
	be := w.BlockEntity(x, y, z)
	if be == nil || !world.IsSignKind(be.Kind) {
		return nil
	}
	pos := c.player.Position()
	refused := !rules.WithinContainerRange(c.blockRange(),
		pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z, float64(x), float64(y), float64(z))
	if !refused && c.pluginListening(pluginruntime.SignChange) {
		var cancelled bool
		if lines, cancelled = c.pluginSignChange(x, y, z, front, lines); cancelled {
			// The editor closes without changing the text; the client's
			// own edit goes back with the sign's data.
			be.Use(func(b *world.BlockEntity) { b.ReleaseSignEditor(c.player.UUID) })
			c.srv.broadcastBlockEntity(w, x, y, z, be)
			return nil
		}
	}
	if !refused {
		be.Use(func(b *world.BlockEntity) { refused = !b.WriteSign(c.player.UUID, front, lines) })
	}
	if refused {
		c.log.Debug("refused a sign update", "x", x, "y", y, "z", z)
		return nil
	}
	w.Touch(x, y, z)
	c.srv.broadcastBlockEntity(w, x, y, z, be)
	return nil
}

// broadcastBlockEntity sends the world a block entity's whole state (sign
// text, pot or shelf items) as Block Entity Data, as vanilla's
// sendBlockUpdated does.
// vanilla: SignBlockEntity.markUpdated, ShelfBlockEntity.setChanged
func (s *Server) broadcastBlockEntity(w *world.World, x, y, z int32, be *world.BlockEntity) {
	var data nbt.Compound
	be.Use(func(b *world.BlockEntity) { data = b.UpdateTag() })
	s.broadcastBlockChunkExcept(w, nil, x, z, protocol.IDPlayBlockEntityData, blockEntityData(x, y, z, be.TypeID(), data))
}

// useSign handles a right-click on a sign with nothing applicable: a waxed
// one wobbles; otherwise the clicker's facing side's editor opens for them,
// only if no one else holds the edit, the side is plain text and the hand
// may build. vanilla: SignBlock.useWithoutItem
func (c *client) useSign(b registry.Block, x, y, z int32) (bool, error) {
	kind, ok := world.SignEntityKind(b)
	if !ok {
		return false, nil
	}
	w := c.theWorld()
	// A sign saved before signs were modelled, or without its entity, gets an
	// empty one, still editable.
	be := w.EnsureBlockEntity(x, y, z, kind)
	if be == nil {
		return false, nil
	}
	pos := c.player.Position()
	front := rules.SignFrontFacing(w.BlockAt(x, y, z), x, z, pos.X, pos.Z)
	clicked := c.runSignClicks(w, be, x, y, z, front)
	var waxed, other, editable bool
	be.Use(func(b *world.BlockEntity) { waxed, other, editable = b.SignUse(c.player.UUID, front) })
	if waxed {
		c.useSwing = swingServer
		c.srv.broadcastSoundID(w,
			world.SignWaxedFailSound(kind), protocol.SoundCategoryBlock,
			float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

		return true, nil
	}
	if clicked {
		c.useSwing = swingServer
		return true, nil
	}
	if other || !editable || !rules.ModeMayBuild(c.player.GameMode()) {
		return false, nil
	}
	c.openSignEditor(x, y, z, front)
	c.useSwing = swingServer
	return true, nil
}

// runSignClicks runs the click events on the lines of the sign's side front
// names, for c: a command as c at the sign's centre at gamemaster level with
// its output dropped, a dialog shown to c, a custom action logged. A sign
// without allow_op_features runs none and tells c so if any line had one.
// The result is whether any line had one.
// vanilla: SignBlockEntity.executeClickCommandsIfPresent, SignBlockEntity.createCommandSourceStack
func (c *client) runSignClicks(w *world.World, be *world.BlockEntity, x, y, z int32, front bool) bool {
	var allowed bool
	var events [4]nbt.Compound
	be.Use(func(b *world.BlockEntity) { allowed, events = b.SignClicks(front) })
	clicked := false
	for _, event := range events {
		action, _ := nbt.Get[nbt.String](event, "action")
		switch action {
		case "run_command":
			if allowed {
				line, _ := nbt.Get[nbt.String](event, "command")
				c.srv.runBlockCommand(c.srv.newSignClickSource(w, c, x, y, z), string(line))
			}
		case "show_dialog":
			if allowed {
				if dialog, ok := command.DialogOf(event["dialog"]); ok {
					c.ShowDialog(dialog)
				}
			}
		case "custom":
			if allowed {
				// vanilla: MinecraftServer.handleCustomClickAction only logs.
				c.pluginSignClick(event)
				c.log.Debug("received custom click action", "id", event["id"], "payload", event["payload"])
			}
		default:
			continue
		}
		clicked = true
	}
	if !allowed && clicked {
		c.send(protocol.IDPlaySystemChat, systemChatPayload(
			protocol.Text{Translate: "sign.click_actions_disabled", Color: protocol.ColorRed}, true))
	}
	return clicked
}

// newSignClickSource is the source a sign's run_command runs from: c as @s
// and the name, at the sign's centre facing 0/0, output dropped.
// vanilla: SignBlockEntity.createCommandSourceStack
func (s *Server) newSignClickSource(w *world.World, c *client, x, y, z int32) *commandBlockSource {
	src := &commandBlockSource{name: c.player.Name, self: c}
	src.worldScope = worldScope{
		srv: s,
		in:  func() *dimension { return s.dimensionOfWorld(w) },
		by:  src.Profile,
	}
	src.pos = player.Position{X: float64(x) + 0.5, Y: float64(y) + 0.5, Z: float64(z) + 0.5}
	return src
}

// useSignApplicator handles held dye, glow ink, ink or honeycomb on a sign
// before the item's own use: the facing side takes the colour, glow, glow
// removal or wax, one item spent outside creative. A waxed sign, one whose
// edit another holds, or a side already so set is left alone.
// vanilla: SignBlock.useItemOn, DyeItem, GlowInkSacItem, InkSacItem,
// HoneycombItem.tryApplyToSign
func (c *client) useSignApplicator(hand, x, y, z int32) bool {
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	kind, isSign := world.SignEntityKind(registry.BlockOf(state))
	if !isSign {
		return false
	}
	// The applicators sit behind mayBuild: adventure never dyes.
	if !rules.ModeMayBuild(c.player.GameMode()) {
		return false
	}
	be := w.EnsureBlockEntity(x, y, z, kind)
	if be == nil {
		return false
	}
	held := c.heldItem(hand)
	dyeColour, _ := rules.DyeItemColour(held)
	pos := c.player.Position()
	front := rules.SignFrontFacing(state, x, z, pos.X, pos.Z)

	var applied bool
	var sound registry.Sound
	var waxEvent bool
	var data nbt.Compound
	be.Use(func(b *world.BlockEntity) {
		if sound, waxEvent, applied = b.ApplySignItem(registry.Item(held.ItemID), dyeColour, c.player.UUID, front); applied {
			data = b.UpdateTag()
		}
	})
	if !applied {
		return false
	}
	w.Touch(x, y, z)
	c.srv.broadcastBlockChunkExcept(w, nil, x, z, protocol.IDPlayBlockEntityData, blockEntityData(x, y, z, be.TypeID(), data))
	c.runSignClicks(w, be, x, y, z, front)
	if waxEvent {
		// vanilla: HoneycombItem.useOn, LevelEvent.PARTICLES_WAX_ON (particles only)
		c.srv.broadcastLevelEvent(w, protocol.WorldEventWaxOn, x, y, z, 0)
		c.srv.broadcastSoundID(w, rules.SoundWaxOn, protocol.SoundCategoryBlock,
			float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)
	} else {
		c.srv.broadcastSoundID(w,
			sound, protocol.SoundCategoryBlock,
			float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	}
	c.gameEventAt(rules.GameEventBlockChange, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, w.BlockAt(x, y, z))
	c.awardUsedHeld(hand)
	c.spendHeld(hand)
	return true
}
