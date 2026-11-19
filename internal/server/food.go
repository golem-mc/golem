package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Eating. The client sends Use Item when it starts and Player Action status
// 5 only if it lets go early; holding the button sends nothing more. The
// server ends the meal: when the countdown runs out it sends the
// use-finished entity event (the client waits for it whatever the consume
// time), applies the food and lowers the hand. A client still holding then
// sends a new Use Item, so holding right click eats until full.

const burpSound = registry.SoundEntityPlayerBurp

// eatSession is the meal or drink in progress. item makes a mid-meal slot
// swap apply nothing; a mode change cancels; length is the starting count
// the bite rhythm reads. ticks == 0 means no meal.
type eatSession struct {
	hand   int32
	item   int32
	use    registry.Consumable
	mode   int32
	ticks  int32
	length int32
}

// beginEating starts a meal of the hand's food unless one is running; the
// server keeps its meal even if the client restarts its use. The countdown
// is the consume time in ticks; hand-state metadata shows others eating.
func (c *client) beginEating(hand int32, use registry.Consumable) {
	c.eatMu.Lock()
	if c.eat.ticks > 0 {
		c.eatMu.Unlock()
		return
	}
	c.shieldMu.Lock()
	raised := c.raise.raised
	c.shieldMu.Unlock()
	if raised {
		c.eatMu.Unlock()
		return
	}
	var item int32
	c.player.Inventory(func(inv *player.Inventory) {
		item = inv.Slots[rules.SlotOfHand(inv, hand)].ItemID
	})
	ticks := rules.ConsumeTicks(use.Seconds)
	// A consumable of 0 ticks is consumed at once, which is the next tick
	// boundary here, without ever raising the hand.
	// vanilla: Consumable.startConsuming
	instant := ticks == 0
	if instant {
		ticks = 1
	}
	c.eat = eatSession{hand: hand, item: item, use: use, mode: c.player.GameMode(), ticks: ticks, length: ticks}
	c.eatMu.Unlock()
	if !instant {
		c.updateAppearance(func(a *player.Appearance) { a.Eating = true; a.EatingHand = hand })
	}
	c.log.Debug("eating started", "hand", hand, "ticks", ticks)
}

// cancelEating calls the meal off with nothing applied.
func (c *client) cancelEating() {
	c.eatMu.Lock()
	if c.eat.ticks == 0 {
		c.eatMu.Unlock()
		return
	}
	c.eat.ticks = 0
	c.eatMu.Unlock()
	c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
	c.log.Debug("eating cancelled")
}

// eatDue counts the meal down a tick from the main loop. bite reports
// whether this tick's bite sounds; ok reports a meal completed this tick,
// with its hand; use is its consumable.
func (c *client) eatDue() (use registry.Consumable, hand int32, ok bool, bite bool) {
	v := c.player.Vitals()
	c.eatMu.Lock()
	defer c.eatMu.Unlock()
	// The meal continues in creative (canConsume is checked only at the
	// start), but death, spectator or any mode change cancels it.
	if v.Dead || v.GameMode == protocol.GameModeSpectator || v.GameMode != c.eat.mode {
		if c.eat.ticks != 0 {
			c.eat.ticks = 0
			c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
			c.log.Debug("eating cancelled")
		}
		return registry.Consumable{}, 0, false, false
	}
	if c.eat.ticks <= 0 {
		return registry.Consumable{}, 0, false, false
	}
	// The hand must still hold the item the use began with.
	// vanilla: LivingEntity.updatingUsingItem
	var inHand int32
	c.player.Inventory(func(inv *player.Inventory) {
		inHand = inv.Slots[rules.SlotOfHand(inv, c.eat.hand)].ItemID
	})
	if inHand != c.eat.item {
		c.eat.ticks = 0
		c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
		c.log.Debug("eating cancelled")
		return registry.Consumable{}, 0, false, false
	}
	// Read before the decrement: vanilla reads the ticks still remaining.
	bite = rules.EatBiteTicks(c.eat.length, c.eat.ticks)
	c.eat.ticks--
	if c.eat.ticks > 0 {
		return c.eat.use, 0, false, bite
	}

	// The food must still be in its starting hand; the player may have swapped
	// it mid-meal.
	hand = c.eat.hand
	var held protocol.ItemStack
	c.player.Inventory(func(inv *player.Inventory) {
		held = inv.Slots[rules.SlotOfHand(inv, hand)]
	})
	if held.ItemID != c.eat.item {
		c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
		return c.eat.use, 0, false, bite
	}
	return c.eat.use, hand, true, bite
}

// finishEating completes a meal in vanilla's order (vanilla:
// ServerPlayer.completeUsingItem, Consumable.onConsume): use-finished
// event, last bite, food with eat sound and burp, potion contents,
// on_consume_effects, then the consumed item and use_remainder. The last
// bite goes to all but the eater, whose client plays it; the neutral eat and
// burp go to everyone. A harming potion's blow is returned for the caller's
// hurt pipeline, where the drink's tick runs it.
func (c *client) finishEating(hand int32, use registry.Consumable) []rules.Hurt {
	held, ok := c.pluginConsume(hand)
	if !ok {
		c.resyncHand(hand)
		c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
		return nil
	}
	c.send(protocol.IDPlayEntityEvent,
		entityEventPayload(c.player.EntityID, protocol.EntityStatusUseFinished))

	pos := c.player.Position()
	c.srv.broadcastSoundPacketExcept(c.theWorld(), c,
		c.srv.eatBiteSoundAt(use, pos.X, pos.Y, pos.Z))

	// vanilla: Consumable.onConsume
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	c.consumedItemAdvancements(held.ItemID)
	// The use_cooldown starts when the use finishes, in any game mode.
	// vanilla: ItemStack.applyAfterUseComponentSideEffects, UseCooldown.apply
	c.startUseCooldown(held)
	if food, ok := registry.StackFood(held); ok {
		c.srv.broadcastSoundHolder(c.theWorld(),
			use.Sound, protocol.SoundCategoryNeutral, pos.X, pos.Y, pos.Z, 1.0, c.srv.eatFoodPitch())

		before := c.player.Vitals()
		c.player.UpdateVitals(func(v *player.Vitals) {
			rules.Eat(v, food)
		})
		c.pluginEaten(before, held)
		c.srv.broadcastSoundID(c.theWorld(),
			burpSound, protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z, 0.5, c.srv.burpPitch())

	}

	// The stack's consume listeners run before on_consume_effects, so a
	// potion's effects apply first, with the player's own dice (vanilla's
	// user.getRandom()). vanilla: Consumable.onConsume
	var changes []effect.Change
	var hurts []rules.Hurt
	c.player.UpdateBody(func(v *player.Vitals, effects *effect.Set, a *player.Attributes) {
		if contents, ok := held.PotionContentsOf(); ok {
			changes = rules.DrinkPotion(v, effects, a, contents, held.PotionDurationScaleOf(), &hurts)
		}
		changes = append(changes, rules.ConsumeListeners(v, effects, held)...)
		if len(changes) > 0 {
			rules.UpdateEffectAttributes(a, effects.All())
		}
	})
	c.tellEffects(changes)
	c.applyConsumeEffects(use.Effects)
	// After on_consume_effects, at the position a teleport may have moved the
	// player to. vanilla: Consumable.onConsume
	ev := rules.GameEventEat
	if use.Drink {
		ev = rules.GameEventDrink
	}
	at := c.player.Position()
	c.srv.gameEvent(c.theWorld(), ev, at.X, at.Y, at.Z, c.gameEventContext(0))

	// Creative keeps the item (hasInfiniteMaterials) but still gets the food;
	// onConsume has no gate of its own.
	if c.player.GameMode() != protocol.GameModeCreative {
		var payload []byte
		var visible, extra, spent bool
		c.player.Inventory(func(inv *player.Inventory) {
			slot := rules.SlotOfHand(inv, hand)
			stack := inv.Slots[slot]
			stack.Count--
			// vanilla: UseRemainder.convertIntoRemainder: an emptied slot takes the
			// remainder, else it goes to the inventory.
			if stack.Count <= 0 {
				spent = true
				stack = use.Remainder
			} else {
				extra = !use.Remainder.Empty()
			}
			inv.Set(slot, stack)
			payload = inv.WindowSlot(playerWindowID, int32(slot), stack)
			visible = player.VisibleSlot(inv, slot)
		})
		// Sent through the mailbox: this runs on the main loop.
		c.send(protocol.IDPlayContainerSlot, payload)
		if visible {
			c.broadcastEquipment()
		}
		if spent {
			c.spendUse()
		}
		if extra {
			// The bowl is not a /give item: it goes to the inventory, and what does not
			// fit drops normally. vanilla: ServerPlayer.handleExtraItemsCreatedOnUse
			remainder := use.Remainder
			if left := c.addToInventory(remainder); left > 0 {
				remainder.Count = left
				c.throwItem(remainder)
			}
		}
	}
	// Metadata is absolute, so the lowered hand has to be sent explicitly.
	c.updateAppearance(func(a *player.Appearance) { a.Eating = false })
	return hurts
}

// applyConsumeEffects completes each action before starting the next one.
// vanilla: Consumable.onConsume
func (c *client) applyConsumeEffects(actions []registry.ConsumeEffect) {
	for i := range actions {
		var consumed rules.Consumed
		// Teleports need the player lock released, and must finish before the
		// next action reads the position or draws from the player's random.
		c.player.UpdateBody(func(v *player.Vitals, effects *effect.Set, a *player.Attributes) {
			consumed = rules.Consume(v, effects, a, actions[i:i+1], c.ownDice())
			if len(consumed.Changes) > 0 {
				rules.UpdateEffectAttributes(a, effects.All())
			}
		})
		c.tellEffects(consumed.Changes)
		for _, tp := range consumed.Teleports {
			c.teleportRandomly(tp.Diameter, tp.Directional)
		}
		for _, sound := range consumed.Sounds {
			// vanilla: PlaySoundConsumeEffect.apply, Level.playSound(BlockPos)
			pos := c.player.Position()
			c.srv.broadcastSoundHolder(c.theWorld(),
				sound, protocol.SoundCategoryPlayer, math.Floor(pos.X)+0.5, math.Floor(pos.Y)+0.5, math.Floor(pos.Z)+0.5, 1, 1)
		}
	}
}

// eatBiteSoundAt builds the Sound Effect packet for one bite: volume 0.5 or
// 1, pitch 1 +/- 0.2 (triangle); a sip is volume 0.5, pitch 0.9 to 1.
// vanilla: Consumable.emitParticlesAndSounds
func (s *Server) eatBiteSoundAt(use registry.Consumable, x, y, z float64) []byte {
	volume, pitch := rules.ConsumableSound(s.soundRng, use.Drink)
	return soundEffectHolderAt(use.Sound, protocol.SoundCategoryPlayer, x, y, z, volume, pitch)
}

// eatFoodPitch is the completion bite's pitch, 1 +/- 0.4 (triangle).
func (s *Server) eatFoodPitch() float32 {
	return 1.0 + (s.soundRng.Float32()-s.soundRng.Float32())*0.4
}

// burpPitch is the burp's pitch, uniform in [0.9, 1.0).
// vanilla: FoodProperties.onConsume, Mth.randomBetween
func (s *Server) burpPitch() float32 {
	// Variables, not constants: the bound difference must round in float32.
	lo, hi := float32(0.9), float32(1.0)
	return float32(s.soundRng.Float32()*(hi-lo)) + lo
}

// handleUseEating starts a meal for a right-click with food and reports
// whether one began. Creative can always eat; others need the bar not full
// unless the food is always-edible (vanilla: Consumable.canConsume). The
// client applies the same rule, so both agree.
func (c *client) handleUseEating(hand int32) bool {
	mode := c.player.GameMode()
	if mode == protocol.GameModeSpectator {
		return false
	}
	held := c.heldItem(hand)
	use, ok := registry.StackConsumable(held)
	if !ok {
		return false
	}
	if food, ok := registry.StackFood(held); ok && mode != protocol.GameModeCreative {
		if v := c.player.Vitals(); !food.CanAlwaysEat && v.Food >= rules.MaxFood {
			return false
		}
	}
	c.beginEating(hand, use)
	return true
}

// handleReleaseUse handles Player Action status 5: it cancels a meal,
// lowers a shield (shield.go) and releases a drawn bow (bow.go).
func (c *client) handleReleaseUse() {
	c.cancelEating()
	c.releaseShield()
	c.releaseSpear()
	c.releaseBow()
	c.cancelBrush()
}

// stopUsingItem ends a use with nothing applied: meal, shield and bow draw.
// Vanilla's stopUsingItem never calls releaseUsing, so a drawn bow fires
// nothing. Switching to spectator and swapping the held slot run it.
// vanilla: LivingEntity.stopUsingItem
func (c *client) stopUsingItem() {
	c.cancelEating()
	c.releaseShield()
	c.releaseSpear()
	c.cancelBow()
	c.cancelBrush()
}

// chorusTeleportSound is teleport_randomly's landing sound.
// vanilla: SoundEvents.CHORUS_FRUIT_TELEPORT
const chorusTeleportSound = registry.SoundItemChorusFruitTeleport

// teleportRandomly moves the eater to a fitting spot within diameter, with
// portal particles and the sound at the landing and fall distance reset;
// nothing if no try fits. A rider is dismounted first, even if none fits.
// vanilla: TeleportRandomlyConsumeEffect.apply
func (c *client) teleportRandomly(diameter float32, directional bool) {
	c.dismountBoat(false)
	c.dismountMinecart(false)
	c.dismountCushion(false)
	w := c.theWorld()
	pos := c.player.Position()
	ox, oy, oz := blockOf(pos.X, pos.Y, pos.Z)
	x, y, z, ok := rules.RandomTeleport(w, pos.X, pos.Y, pos.Z, diameter, c.ownDice())
	if !ok {
		return
	}
	c.srv.gameEvent(w, rules.GameEventTeleport, pos.X, pos.Y, pos.Z, c.gameEventContext(0))
	pos.X, pos.Y, pos.Z = x, y, z
	c.teleport(command.Move{Pos: pos})
	c.tp.mu.Lock()
	c.sendMovement(c.player.Position())
	c.tp.mu.Unlock()
	c.srv.broadcastWorld(w, protocol.IDPlayEntityEvent,
		entityEventPayload(c.player.EntityID, protocol.EntityStatusTeleportParticles))
	c.srv.broadcastSoundID(w, chorusTeleportSound, protocol.SoundCategoryPlayer, x, y, z, 1, 1)
	if directional {
		tx, ty, tz := blockOf(x, y, z)
		c.srv.broadcastLevelEvent(w, protocol.WorldEventConsumeTeleport, ox, oy, oz,
			protocol.ClampedPackPosDiff(ox, oy, oz, tx, ty, tz))
	}
	c.player.UpdateVitals(func(v *player.Vitals) {
		v.FallDistance = 0
		rules.ResetImpulse(v)
	})
}
