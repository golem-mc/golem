package server

import (
	"fmt"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// handlePosition updates the player's coordinates from a position-only packet.
func (c *client) handlePosition(r *protocol.Reader) error {
	x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
	flags := r.ReadU8()
	if err := r.Err(); err != nil {
		return c.decodeFailure("move_player_pos", err)
	}
	// A NaN coordinate disconnects; an infinite one is clamped. This check
	// precedes the loading and seating ones.
	// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
	if containsInvalidValues(x, y, z, 0, 0) {
		return c.invalidPlayerMovement()
	}
	// Dropped unanswered while loading; vanilla decodes the packet before its
	// loaded check. vanilla: handleMovePlayer
	if !c.clientLoaded() {
		return nil
	}
	// A seated body is where its vehicle keeps it; the client sends no
	// position while riding.
	if c.isPassenger() {
		return nil
	}
	pos := c.player.Position()
	pos.X, pos.Y, pos.Z = clampHorizontal(x), clampVertical(y), clampHorizontal(z)
	// A packet without a look still snaps the stored one.
	// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
	pos.Yaw, pos.Pitch = snapLook(pos.Yaw, pos.Pitch)
	pos.OnGround = flags&movementOnGround != 0
	c.horizontalCollision.Store(flags&movementHorizontalCollision != 0)
	c.moveReported(pos)
	c.sendChunksAsync()
	return nil
}

// handlePositionAndRotation updates the player's position and facing.
func (c *client) handlePositionAndRotation(r *protocol.Reader) error {
	x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
	yaw, pitch := r.ReadF32(), r.ReadF32()
	flags := r.ReadU8()
	if err := r.Err(); err != nil {
		return c.decodeFailure("move_player_pos_rot", err)
	}
	if containsInvalidValues(x, y, z, yaw, pitch) {
		return c.invalidPlayerMovement()
	}
	// Dropped while loading; vanilla decodes the packet first. The teleport
	// confirm applies its move without this gate.
	// vanilla: handleMovePlayer
	if !c.clientLoaded() {
		return nil
	}
	if !c.isPassenger() {
		c.horizontalCollision.Store(flags&movementHorizontalCollision != 0)
	}
	return c.applyPositionAndRotation(x, y, z, yaw, pitch, flags&movementOnGround != 0)
}

// applyPositionAndRotation applies a validated move that carries a position
// and a rotation, whether or not the client has loaded.
func (c *client) applyPositionAndRotation(x, y, z float64, yaw, pitch float32, onGround bool) error {
	yaw, pitch = snapLook(yaw, pitch)
	// A seated body keeps its vehicle's position and takes only the look.
	// vanilla: ServerGamePacketListenerImpl.handlePlayerPositionChange
	if c.isPassenger() {
		c.rotatePassenger(yaw, pitch)
		return nil
	}
	c.moveReported(player.Position{
		X: clampHorizontal(x), Y: clampVertical(y), Z: clampHorizontal(z),
		Yaw: yaw, Pitch: pitch,
		OnGround: onGround,
	})
	c.sendChunksAsync()
	return nil
}

// handleRotation updates which way the player is facing without moving them.
func (c *client) handleRotation(r *protocol.Reader) error {
	yaw, pitch := r.ReadF32(), r.ReadF32()
	flags := r.ReadU8()
	if err := r.Err(); err != nil {
		return c.decodeFailure("move_player_rot", err)
	}
	if !finite(float64(yaw), float64(pitch)) {
		return c.invalidPlayerMovement()
	}
	// Dropped while loading; vanilla decodes the packet first.
	// vanilla: handleMovePlayer
	if !c.clientLoaded() {
		return nil
	}
	yaw, pitch = snapLook(yaw, pitch)
	// A passenger only turns: no move checks, and nothing that counts as
	// floating. vanilla: handlePlayerPositionChange
	if c.isPassenger() {
		c.rotatePassenger(yaw, pitch)
		return nil
	}
	pos := c.player.Position()
	pos.Yaw, pos.Pitch = yaw, pitch
	pos.OnGround = flags&movementOnGround != 0
	c.horizontalCollision.Store(flags&movementHorizontalCollision != 0)
	c.moveReported(pos)
	return nil
}

// rotatePassenger applies a snapped look without recording a step or ground flags.
func (c *client) rotatePassenger(yaw, pitch float32) {
	c.tp.mu.Lock()
	pos := c.player.Position()
	pos.Yaw, pos.Pitch = yaw, pitch
	if !c.ignoreMove(pos) {
		c.player.SetPosition(pos)
	}
	c.tp.mu.Unlock()
}

// handleMovementFlags updates the on-ground flag the client sends on its own.
func (c *client) handleMovementFlags(r *protocol.Reader) error {
	flags := r.ReadU8()
	if err := r.Err(); err != nil {
		return c.decodeFailure("move_player_status_only", err)
	}
	// Dropped while loading; vanilla decodes the packet first.
	// vanilla: handleMovePlayer
	if !c.clientLoaded() {
		return nil
	}
	// A seated body's on-ground is its vehicle's.
	if c.isPassenger() {
		return nil
	}
	pos := c.player.Position()
	// A packet without a look still snaps the stored one.
	// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
	pos.Yaw, pos.Pitch = snapLook(pos.Yaw, pos.Pitch)
	pos.OnGround = flags&movementOnGround != 0
	c.horizontalCollision.Store(flags&movementHorizontalCollision != 0)
	c.moveReported(pos)
	return nil
}

// wireHand maps a wire hand onto the two: anything else is the main hand.
// vanilla: InteractionHand.BY_ID
func wireHand(hand int32) int32 {
	return protocol.IDOrZero(hand, 2)
}

// handleUseItem handles Use Item: the packet's look turns the player, then
// the held item's use runs (armour, food, shield, bow, spyglass, trident,
// throwables, fishing rod, buckets, bottles, maps, boats, lily pads; see
// their files).
func (c *client) handleUseItem(r *protocol.Reader) error {
	c.useSwing = swingNone
	hand := r.ReadVarInt()
	seq := r.ReadVarInt() // the block change sequence the click answers with
	yaw, pitch := r.ReadF32(), r.ReadF32()
	if err := r.Err(); err != nil {
		return c.decodeFailure("use_item", err)
	}
	hand = wireHand(hand)
	defer c.swingForUse(hand)
	// Dropped while loading, ack included.
	// vanilla: ServerGamePacketListenerImpl.handleUseItem
	if !c.clientLoaded() {
		return nil
	}
	// The look turns the player only with something held, as vanilla's handler
	// does; the rotation is trusted like a move packet's.
	if finite(float64(yaw), float64(pitch)) {
		if held := c.heldItem(hand); !held.Empty() {
			c.player.UpdatePosition(func(pos *player.Position) {
				pos.Yaw, pos.Pitch = snapLook(yaw, pitch)
			})
		}
	}
	if c.demoLocked() {
		c.ackBlock(seq)
		return nil
	}
	if e := c.pluginInteract(plugin.RightClickAir, hand, nil, 0, plugin.Vec3{}); e != nil && (e.Cancelled() || e.DenyItem) {
		c.ackBlock(seq)
		return nil
	}
	// A stack whose cooldown group still runs is not used at all.
	// vanilla: ServerPlayerGameMode.useItem, ItemCooldowns.isOnCooldown
	held := c.heldItem(hand)
	c.shieldMu.Lock()
	cooling := c.itemCooldownTicks(held)
	c.shieldMu.Unlock()
	if cooling > 0 {
		c.ackBlock(seq)
		return nil
	}
	equipped, err := c.equipHeldItem(hand)
	if err != nil {
		return err
	}
	if equipped {
		c.useSwing = swingPredicted
		if rules.DefaultUseIsInstant(held) {
			c.startUseCooldown(held)
		}
		c.ackBlock(seq)
		return nil
	}
	c.handleUseEating(hand)
	c.handleUseShield(hand)
	c.handleUseSpear(hand)
	c.handleUseBow(hand)
	c.handleUseSpyglass(hand)
	c.handleUseCrossbow(hand)
	c.handleUseTrident(hand)
	c.handleUseInstrument(hand)
	// Keep the pre-use stack: the use can consume it or replace it with a
	// filled bucket, a map or a broken steering stick's fishing rod. Each
	// handler reports whether its use returned a successful interaction.
	// vanilla: ItemStack.use, ItemStack.applyAfterUseComponentSideEffects
	for _, use := range []func(int32) bool{
		c.handleUseBundle, c.handleUseThrown, c.handleUseEye, c.handleUseFishingRod,
		c.handleUseSteeringStick, c.handleUseCarrotStick, c.handleUseFirework,
		c.handleUseSpawnEgg, c.handleUseBucket, c.handleUseBottle,
		c.handleUseMap, c.handleUseBoat, c.handleUsePlaceOnWater,
		c.handleUseBook, c.handleUseKnowledgeBook,
	} {
		c.useSwing = swingPredicted
		if use(hand) {
			if rules.DefaultUseIsInstant(held) {
				c.startUseCooldown(held)
			}
			break
		}
		c.useSwing = swingNone
	}
	// The sequence is acked once at the end, flushed at the tick end behind
	// everything the click queued (see ackBlock); a scoop of open water is such
	// a click, with no block to name.
	// vanilla: ServerGamePacketListenerImpl.handleUseItem
	c.ackBlock(seq)
	return nil
}

// handlePlayerAction handles Player Action: dig, drop, swap, release, stab.
func (c *client) handlePlayerAction(r *protocol.Reader) error {
	status := r.ReadVarInt()
	x, y, z := r.ReadPosition()
	face := int32(r.ReadI8())
	seq := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return c.decodeFailure("player_action", err)
	}
	// A status out of enum bounds fails vanilla's decode, which precedes its
	// loaded check. vanilla: FriendlyByteBuf.readEnum
	if status < 0 || status > protocol.PlayerActionStab {
		return c.decodeFailure("player_action",
			fmt.Errorf("player action carries status %d, which is not an action", status))
	}
	// Dropped while loading, the sequence unacked.
	// vanilla: ServerGamePacketListenerImpl.handlePlayerAction
	if !c.clientLoaded() {
		return nil
	}
	// A dig beyond reach (block interaction range plus buffer) gets only the
	// ack, in any mode: vanilla's "too far". An abort of a held dig is still
	// honoured, so a player moved away mid-dig does not leave it cracking.
	// vanilla: ServerPlayerGameMode.handleBlockBreakAction
	switch status {
	case protocol.PlayerActionStartedDigging, protocol.PlayerActionChangeDestroyDirection,
		protocol.PlayerActionCancelledDigging, protocol.PlayerActionFinishedDigging:
		if c.demoLocked() {
			c.ackBlock(seq)
			return nil
		}
		pos := c.player.Position()
		if !rules.WithinBlockInteractionRange(c.blockRange(),
			pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z, float64(x), float64(y), float64(z)) {
			if status == protocol.PlayerActionCancelledDigging && c.isDigging() {
				c.abortDig(x, y, z)
			}
			c.ackBlock(seq)
			return nil
		}
		if status == protocol.PlayerActionStartedDigging || status == protocol.PlayerActionChangeDestroyDirection {
			c.dig.mu.Lock()
			c.dig.face = face
			c.dig.mu.Unlock()
		}
		switch status {
		case protocol.PlayerActionStartedDigging:
			// Creative breaks on START, as do modes where one tick finishes the block,
			// since the client sends no finish for those. Otherwise the dig is
			// recorded for the ticks to finish. Adventure digs only what the main
			// hand's minecraft:can_break names (a non-matching START, like a bare
			// hand's, gets the block's state back); a spectator's client sends no START.
			// vanilla: ServerPlayerGameMode.handleBlockBreakAction,
			// Player.blockActionRestricted
			mode := c.player.GameMode()
			if mode != protocol.GameModeSpectator {
				// A refused punch gives the block back as it stands.
				if e := c.pluginInteract(plugin.LeftClickBlock, protocol.HandMain, &[3]int32{x, y, z}, face, plugin.Vec3{}); e != nil && (e.Cancelled() || e.DenyBlock) {
					w := c.theWorld()
					c.sendIn(w, protocol.IDPlayBlockUpdate, blockUpdate(x, y, z, w.BlockAt(x, y, z)))
					break
				}
			}
			if c.underSpawnProtection(x, z) {
				c.sendSpawnProtection(x, y, z)
			} else if !c.mayInteractAt(x, z) {
				// vanilla: ServerPlayerGameMode.handleBlockBreakAction
				w := c.theWorld()
				c.sendIn(w, protocol.IDPlayBlockUpdate, blockUpdate(x, y, z, w.BlockAt(x, y, z)))
			} else if mode == protocol.GameModeCreative {
				if cancelled, _ := c.pluginBlockDamage(x, y, z, true); cancelled {
					w := c.theWorld()
					c.sendIn(w, protocol.IDPlayBlockUpdate, blockUpdate(x, y, z, w.BlockAt(x, y, z)))
				} else {
					c.destroyBlock(x, y, z, false)
				}
			} else if mode != protocol.GameModeSpectator {
				w := c.theWorld()
				state := w.BlockAt(x, y, z)
				if rules.BlockActionRestricted(mode, c.heldItem(protocol.HandMain), adventureTarget(w, x, y, z, state)) {
					c.sendIn(w, protocol.IDPlayBlockUpdate, blockUpdate(x, y, z, state))
				} else if cancelled, instant := c.pluginBlockDamage(x, y, z, state != airBlockState && c.destroyProgress(state) >= 1); cancelled {
					c.sendIn(w, protocol.IDPlayBlockUpdate, blockUpdate(x, y, z, state))
				} else {
					// vanilla: BlockState.attack, before the destroy progress
					changes, effects := c.srv.physicsFor(w).AttackBlock(x, y, z)
					if registry.BlockOf(state) == registry.BlockNoteBlock {
						c.awardStat(player.Custom(registry.CustomStatPlayNoteblock), 1)
					}
					for _, e := range effects {
						c.srv.tellEffectFrom(w, c, e)
					}
					c.srv.tellBlockChanges(w, changes)
					c.teleportDragonEgg(x, y, z)
					// Air has full progress, whose stage 10 clears the crack for others.
					progress := float32(1)
					if state != airBlockState {
						progress = c.destroyProgress(state)
					}
					if state != airBlockState && (progress >= 1 || instant) {
						c.destroyBlock(x, y, z, false)
					} else {
						c.startDig(x, y, z, progress)
					}
				}
			}
		case protocol.PlayerActionCancelledDigging:
			c.abortDig(x, y, z)
		case protocol.PlayerActionFinishedDigging:
			// Ends only a recorded dig: creative broke on START, recording nothing,
			// and spectators never dig, so their finish is the bare ack (see
			// stopDigging).
			c.stopDigging(x, y, z)
		}
		// The action's single ack, recorded last, behind every queued block update,
		// for the tick end to flush (see ackBlock).
		c.ackBlock(seq)
		return nil
	case protocol.PlayerActionDropItem, protocol.PlayerActionDropItemStack:
		// Q throws one of the held stack out, Ctrl+Q the rest of it.
		// vanilla: ServerGamePacketListenerImpl.handlePlayerAction
		if c.player.GameMode() == protocol.GameModeSpectator {
			return nil
		}
		return c.dropHeld(status == protocol.PlayerActionDropItemStack)
	case protocol.PlayerActionSwapItemInHand:
		// vanilla: ServerGamePacketListenerImpl.handlePlayerAction
		if c.player.GameMode() == protocol.GameModeSpectator {
			return nil
		}
		err := c.swapHands()
		c.stopUsingItem()
		return err
	case protocol.PlayerActionReleaseUseItem:
		// Puts away a half-eaten item unapplied, or fires a drawn bow.
		c.handleReleaseUse()
	case protocol.PlayerActionStab:
		c.stab()
	default:
		c.log.Debug("ignoring player action", "status", status)
	}
	return nil
}

// handleUseItemOn handles a placement against a clicked face; the client
// waits for the server's decision.
func (c *client) handleUseItemOn(r *protocol.Reader) error {
	c.useSwing = swingNone
	hand := r.ReadVarInt()
	x, y, z := r.ReadPosition()
	face := r.ReadVarInt()
	cursorX := r.ReadF32()
	cursorY := r.ReadF32()
	cursorZ := r.ReadF32()
	inside := r.ReadBool() // inside block
	r.ReadBool()           // world border hit
	seq := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return c.decodeFailure("use_item_on", err)
	}
	// vanilla: Direction.BY_ID
	hand, face = wireHand(hand), protocol.IDWrap(face, int32(len(protocol.FaceOffset)))
	defer c.swingForUse(hand)
	// Dropped while loading, ack included: vanilla acks after its
	// hasClientLoaded check.
	if !c.clientLoaded() {
		return nil
	}
	if c.demoLocked() {
		c.ackBlock(seq)
		return nil
	}
	pos := c.player.Position()
	// Out of reach (block interaction range plus buffer) only acks, like
	// vanilla's handleUseItemOn.
	if !rules.WithinBlockInteractionRange(c.blockRange(),
		pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z, float64(x), float64(y), float64(z)) {
		c.ackBlock(seq)
		return nil
	}
	// A click outside the world's height shows the limit, with no echoes.
	// vanilla: ServerGamePacketListenerImpl.handleUseItemOn
	if w := c.theWorld(); y > w.MinY()+w.Height()-1 || y < w.MinY() {
		if y < w.MinY() {
			c.sendBuildLimit(false, w.MinY())
		} else {
			c.sendBuildLimit(true, w.MinY()+w.Height()-1)
		}
		c.ackBlock(seq)
		return nil
	}
	ctx := rules.PlacementContext{
		Face:     face,
		Yaw:      pos.Yaw,
		Pitch:    pos.Pitch,
		CursorX:  cursorX,
		CursorY:  cursorY,
		CursorZ:  cursorZ,
		Sneaking: c.player.Appearance().Sneaking,
		Inside:   inside,
	}
	// A refused click shows its reason's overlay, then the clicked block and
	// its neighbour as they stand.
	// vanilla: ServerGamePacketListenerImpl.handleUseItemOn
	if !c.mayInteractAt(x, z) {
		w := c.theWorld()
		if c.underSpawnProtection(x, z) {
			c.sendSpawnProtection(x, y, z)
		} else {
			c.sendBuildLimit(true, w.MinY()+w.Height()-1)
		}
		err := c.useItemOnTail(w, x, y, z, face)
		c.ackBlock(seq)
		return err
	}
	w := c.theWorld()
	if e := c.pluginInteract(plugin.RightClickBlock, hand, &[3]int32{x, y, z}, face,
		plugin.Vec3{X: float64(cursorX), Y: float64(cursorY), Z: float64(cursorZ)}); e != nil {
		if e.Cancelled() {
			// The echoes revert the client's prediction; the slot resend
			// restores the count the client already took.
			err := c.useItemOnTail(w, x, y, z, face)
			c.resyncHand(hand)
			c.ackBlock(seq)
			return err
		}
		c.denyBlockUse, c.denyItemUse = e.DenyBlock, e.DenyItem
		defer func() { c.denyBlockUse, c.denyItemUse = false, false }()
		if e.DenyItem {
			defer c.resyncHand(hand)
		}
	}
	off := protocol.FaceOffset[face]
	held := c.heldItem(hand)
	clicked, beside := w.BlockAt(x, y, z), w.BlockAt(x+off[0], y+off[1], z+off[2])
	err := c.placeBlock(hand, x, y, z, ctx)
	if c.itemUsedOn {
		c.itemUsedOnBlockAdvancements(c.theWorld(), x, y, z, held)
	}
	c.blockUseAdvancements(c.theWorld(), x, y, z, c.heldItem(hand))
	c.sendBuildLimitIfRefused(hand, held, clicked, beside, x, y, z, face)
	// The single ack, recorded last, behind the placement's broadcasts and
	// echoes, for the tick end to flush (see ackBlock). vanilla's
	// handleUseItemOn records it before useItemOn, its connection likewise
	// holding it to the tick end.
	c.ackBlock(seq)
	return err
}

// handlePickItemFromBlock handles middle-click on a block: the block's item
// goes to the hand with the block_state component getCloneItemStack copies
// and, with the data flag, the block entity's components.
// vanilla: ServerGamePacketListenerImpl.handlePickItemFromBlock
func (c *client) handlePickItemFromBlock(r *protocol.Reader) error {
	x, y, z := r.ReadPosition()
	includeData := r.ReadBool()
	if err := r.Err(); err != nil {
		return c.decodeFailure("pick_item_from_block", err)
	}
	// The client sends the flag on ctrl+pick in any mode; it is honoured only
	// with infinite materials.
	// vanilla: ServerGamePacketListenerImpl.handlePickItemFromBlock
	includeData = includeData && c.player.GameMode() == protocol.GameModeCreative
	state := c.theWorld().BlockAt(x, y, z)
	if state == airBlockState {
		return nil
	}
	// Out of reach (block interaction range plus buffer) picks nothing.
	// vanilla: handlePickItemFromBlock
	pos := c.player.Position()
	if !rules.WithinBlockInteractionRange(c.blockRange(),
		pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z, float64(x), float64(y), float64(z)) {
		return nil
	}
	itemID, ok := registry.CloneItem(state)
	if !ok {
		return nil
	}
	pick := protocol.ItemStack{ItemID: itemID, Count: 1}
	if props, ok := registry.CloneItemStateProps(state, includeData); ok {
		pick.SetBlockStateProperties(props)
	}
	if includeData {
		if components, ok := c.theWorld().ItemComponentsAt(x, y, z); ok {
			// The entity's own components first, the kind's after them.
			// vanilla: BlockEntity.collectComponents
			for name, value := range components.Extra {
				registry.ReadComponent(&pick, name, value)
			}
			if components.Container != nil {
				pick.Container = components.Container
			}
			if components.CustomName != nil {
				pick.CustomName = components.CustomName
			}
			if components.BannerPatterns != nil {
				pick.SetBannerPatterns(components.BannerPatterns)
			}
			if components.Profile != nil {
				pick.SetProfile(*components.Profile)
			}
			if components.NoteBlockSound != "" {
				pick.SetNoteBlockSound(components.NoteBlockSound)
			}
			if components.Bees != nil {
				pick.SetBees(components.Bees)
			}
			if components.PotDecorations != nil {
				pick.SetPotDecorations(*components.PotDecorations)
			}
			if components.SignFront != nil {
				pick.SetSignText(true, *components.SignFront)
			}
			if components.SignBack != nil {
				pick.SetSignText(false, *components.SignBack)
			}
			if components.Waxed {
				pick.SetWaxed()
			}
			pick.Lock = components.Lock
			pick.ContainerLoot = components.ContainerLoot
			if components.BlockEntityData != nil {
				pick.SetBlockEntityData(components.BlockEntityType, components.BlockEntityData)
			}
		}
	}
	return c.pickBlock(pick)
}

// handleHeldItemChange updates which hotbar slot the player is holding.
func (c *client) handleHeldItemChange(r *protocol.Reader) error {
	selected := int32(r.ReadI16())
	if err := r.Err(); err != nil {
		return c.decodeFailure("set_carried_item", err)
	}
	if selected < 0 || selected >= player.HotbarSlots {
		return nil
	}
	var previous int32
	c.player.Inventory(func(inv *player.Inventory) { previous = inv.Selected })
	if previous != selected && !c.pluginItemHeld(previous, selected) {
		// The client moved its selection already; put it back.
		return c.writePacket(protocol.IDPlayHeldItem, "set held item", player.HeldItem(previous))
	}
	var changed bool
	c.player.Inventory(func(inv *player.Inventory) {
		changed = inv.Selected != selected
		inv.Selected = selected
	})
	if changed {
		// Switching slots while the main hand is in use drops the use with nothing
		// applied; an off-hand use continues.
		// vanilla: ServerGamePacketListenerImpl.handleSetCarriedItem
		if app := c.player.Appearance(); !app.Eating || app.EatingHand != protocol.HandOff {
			c.stopUsingItem()
		}
		// The main hand is the selected slot, so others see a new item.
		c.broadcastEquipment()
	}
	return nil
}

// handleSwingArm spends the next hit's recovery and relays the main-hand attack animation.
// vanilla: ServerGamePacketListenerImpl.handlePunch
func (c *client) handleSwingArm(_ *protocol.Reader) error {
	if c.pluginListening(pluginruntime.PlayerAnimation) {
		e := &plugin.PlayerAnimationEvent{Player: c.pluginSelf(), Hand: plugin.MainHand}
		if c.pluginFire(e, false) && e.Cancelled() {
			return nil
		}
	}
	c.pluginLeftClickAir()
	held := c.heldItem(protocol.HandMain)
	started := c.swing.startIfAble(c.swingDuration(registry.StackAttackAnimation(held)))
	c.attackStrengthTicks.Store(0)
	if started {
		c.srv.broadcastExcept(c, protocol.IDPlaySwingAnimation, swingAttackArm(c.player, held))
	}
	return nil
}

// handlePlayerInput reads the held keys; only sneak matters to others.
func (c *client) handlePlayerInput(r *protocol.Reader) error {
	flags := r.ReadU8()
	if err := r.Err(); err != nil {
		return c.decodeFailure("player_input", err)
	}
	// Vanilla stores input while loading but applies sneak only after its
	// hasClientLoaded check.
	c.moveKeys.Store(uint32(flags))
	if !c.clientLoaded() {
		return nil
	}
	sneak := flags&protocol.InputSneak != 0
	if sneak != c.player.Appearance().Sneaking && !c.pluginToggle(pluginruntime.PlayerToggleSneak, sneak) {
		return nil
	}
	c.updateAppearance(func(a *player.Appearance) {
		a.Sneaking = sneak
	})
	// Sneaking dismounts a driver from their boat or cart, the dismount of vanilla's
	// Player Input.
	c.steerCamel(flags&protocol.InputForward != 0 && flags&protocol.InputBackward == 0)
	if sneak && (c.ridingBoat() || c.ridingMob() || c.ridingMinecart() || c.ridingCushion()) && !c.dismountAllowed() {
		return nil
	}
	if sneak && c.ridingBoat() {
		c.dismountBoat(true)
	}
	if sneak && c.ridingMob() {
		c.dismountMob(true)
	}
	if sneak && c.ridingMinecart() {
		c.dismountMinecart(true)
	}
	if sneak && c.ridingCushion() {
		c.dismountCushion(true)
	}
	return nil
}

// handlePlayerAbilities reads the creative double-jump flying flag, ignored
// outside creative and spectator (vanilla accepts anyone's, but only those
// modes get the ability). Flying prevents the swimming pose, which makes
// underwater creative building bearable.
func (c *client) handlePlayerAbilities(r *protocol.Reader) error {
	flags := r.ReadI8()
	if err := r.Err(); err != nil {
		return c.decodeFailure("player_abilities", err)
	}
	if c.abilityFlags()&protocol.AbilityAllowFlying == 0 {
		return nil
	}
	wasFlying := c.flying.Load()
	if now := flags&protocol.AbilityFlying != 0; now != wasFlying && !c.pluginToggle(pluginruntime.PlayerToggleFlight, now) {
		return c.sendAbilities()
	}
	c.flying.Store(flags&protocol.AbilityFlying != 0)
	// A flyer leaves the swim pose at once. vanilla: Player.updateSwimming
	if wasFlying != c.flying.Load() {
		c.updateSwimming()
	}
	return nil
}

// handleChangeGameMode handles the F3+F4 switcher: below gamemaster level
// it is refused with a warning, else applied as /gamemode, with feedback.
// vanilla: ServerGamePacketListenerImpl.handleChangeGameMode
func (c *client) handleChangeGameMode(r *protocol.Reader) error {
	mode := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return c.decodeFailure("change_game_mode", err)
	}
	// GameType.STREAM_CODEC is ByteBufCodecs.idMapper(BY_ID, getId), and
	// BY_ID's OutOfBoundsStrategy.ZERO decodes any id outside 0..3 (negatives
	// too) as survival rather than failing. vanilla: GameType.BY_ID
	if mode < protocol.GameModeSurvival || mode > protocol.GameModeSpectator {
		mode = protocol.GameModeSurvival
	}
	if c.PermLevel() < command.PermLevelGamemasters {
		c.log.Warn("player tried to change game mode without permission", "player", c.player.Name)
		return nil
	}
	return command.SetOwnGameMode(c, mode)
}

// handleChangeDifficulty applies a gamemaster's options-menu difficulty
// while unlocked and tells every player.
// vanilla: ServerGamePacketListenerImpl.handleChangeDifficulty
func (c *client) handleChangeDifficulty(r *protocol.Reader) error {
	d := r.ReadVarInt()
	if err := r.Err(); err != nil {
		return c.decodeFailure("change_difficulty", err)
	}
	if c.PermLevel() < command.PermLevelGamemasters {
		c.log.Warn("player tried to change difficulty without permission", "player", c.player.Name)
		return nil
	}
	// Difficulty.BY_ID wraps ids outside 0..3.
	// vanilla: Difficulty.BY_ID
	d = ((d % 4) + 4) % 4
	w := c.theWorld()
	if !w.ChangeDifficulty(uint8(d)) {
		return nil
	}
	c.srv.broadcastDifficultyOf(w)
	return nil
}

// handleLockDifficulty locks or unlocks the difficulty for a gamemaster and
// tells every player. vanilla: ServerGamePacketListenerImpl.handleLockDifficulty
func (c *client) handleLockDifficulty(r *protocol.Reader) error {
	locked := r.ReadBool()
	if err := r.Err(); err != nil {
		return c.decodeFailure("lock_difficulty", err)
	}
	if c.PermLevel() < command.PermLevelGamemasters {
		return nil
	}
	w := c.theWorld()
	w.SetDifficultyLocked(locked)
	c.srv.broadcastDifficultyOf(w)
	return nil
}

// handlePlayerCommand handles sprinting, leaving a bed and elytra gliding.
// Swimming is re-evaluated with sprinting, so stopping a sprint in water
// stops swimming in the same metadata packet.
func (c *client) handlePlayerCommand(r *protocol.Reader) error {
	r.ReadVarInt() // entity ID, which the vanilla server ignores: a client can only speak for itself
	action := r.ReadVarInt()
	jumpBoost := r.ReadVarInt() // only the horse jump carries one
	if err := r.Err(); err != nil {
		return c.decodeFailure("player_command", err)
	}
	// An action past the vanilla enum fails its decode.
	// vanilla: FriendlyByteBuf.readEnum
	if action < 0 || action > protocol.PlayerCommandStartFlyingElytra {
		return c.decodeFailure("player_command",
			fmt.Errorf("player command carries action %d, which is not an action", action))
	}
	// Ignored while loading; vanilla decodes the packet first.
	// vanilla: handlePlayerCommand
	if !c.clientLoaded() {
		return nil
	}
	switch action {
	case protocol.PlayerCommandStartSprinting, protocol.PlayerCommandStopSprinting:
		on := action == protocol.PlayerCommandStartSprinting
		if on != c.player.Appearance().Sprinting && !c.pluginToggle(pluginruntime.PlayerToggleSprint, on) {
			return nil
		}
		c.updateAppearance(func(a *player.Appearance) {
			a.Sprinting = action == protocol.PlayerCommandStartSprinting
			a.Swimming = c.swimmingNow(*a)
		})
	case protocol.PlayerCommandStartRidingJump:
		c.horseJump(jumpBoost)
	case protocol.PlayerCommandOpenInventory:
		return c.openRiddenMountScreen()
	case protocol.PlayerCommandLeaveBed:
		// The wake key.
		c.stopSleeping(false, true)
	case protocol.PlayerCommandStartFlyingElytra:
		// The glide key: the server decides, putting a refused prediction back
		// down. vanilla: ServerGamePacketListenerImpl.handlePlayerCommand
		c.tryToStartFallFlying()
	default:
		c.log.Debug("ignoring player command", "action", action)
	}
	return nil
}

// sendBuildLimitIfRefused shows the build-limit overlay when a block or
// fluid bucket was held, no block use consumed the click, nothing changed
// (stack and both cells) and the face points out of the world.
// vanilla: ServerGamePacketListenerImpl.handleUseItemOn, wasBlockPlacementAttempt
func (c *client) sendBuildLimitIfRefused(hand int32, held protocol.ItemStack, clicked, beside uint16, x, y, z, face int32) {
	w := c.theWorld()
	maxY, minY := w.MinY()+w.Height()-1, w.MinY()
	tooHigh := face == protocol.FaceTop && y >= maxY
	if !tooHigh && (face != protocol.FaceBottom || y > minY) {
		return
	}
	if c.blockUseConsumed || held.Empty() || !placesBlockOrFluid(registry.Item(held.ItemID)) {
		return
	}
	c.shieldMu.Lock()
	cooling := c.itemCooldownTicks(held)
	c.shieldMu.Unlock()
	now := c.heldItem(hand)
	off := protocol.FaceOffset[face]
	if cooling > 0 || now.ItemID != held.ItemID || now.Count != held.Count ||
		w.BlockAt(x, y, z) != clicked || w.BlockAt(x+off[0], y+off[1], z+off[2]) != beside {
		return
	}
	if tooHigh {
		c.sendBuildLimit(true, maxY)
	} else {
		c.sendBuildLimit(false, minY)
	}
}

// placesBlockOrFluid reports a block item or a bucket holding a fluid.
func placesBlockOrFluid(item registry.Item) bool {
	switch item {
	case registry.ItemWaterBucket, registry.ItemLavaBucket, registry.ItemPowderSnowBucket,
		registry.ItemPufferfishBucket, registry.ItemSalmonBucket, registry.ItemCodBucket,
		registry.ItemTropicalFishBucket, registry.ItemAxolotlBucket, registry.ItemSulfurCubeBucket,
		registry.ItemTadpoleBucket:
		return true
	}
	_, ok := item.Block()
	return ok
}
