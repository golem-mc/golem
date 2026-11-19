package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// Blocks that respond to an item or bare hand themselves: cakes, candles,
// flower pots, composters, cauldrons. Each reports whether the click was
// spent; package rules decides the result.

// useCakeOrCandle puts a candle on a cake, puts a candle out, or takes a
// bite. vanilla: CakeBlock, CandleBlock, CandleCakeBlock
func (c *client) useCakeOrCandle(hand, x, y, z int32, ctx rules.PlacementContext) bool {
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	held := c.heldItem(hand)
	if held.Empty() {
		// Only a hand that may build douses a candle; any hand douses a candle
		// cake, and the bite needs none.
		// vanilla: CandleBlock.useItemOn, CandleCakeBlock.useItemOn
		if out, ok := rules.CandleOut(state, ctx.CursorY); ok &&
			(rules.ModeMayBuild(c.player.GameMode()) || !registry.BlockOf(state).InTag(registry.TagIDCandles)) {
			c.srv.broadcastSoundPacketExcept(w, nil, candleExtinguishSound(float64(x), float64(y), float64(z)))
			c.setUsedBlock(w, x, y, z, out)
			c.gameEventAtCell(rules.GameEventBlockChange, x, y, z)
			return true
		}
	} else if candleCake, ok := rules.CandleOnCake(state, registry.Item(held.ItemID)); ok {
		c.srv.broadcastSoundID(w, registry.SoundBlockCakeAddCandle,
			protocol.SoundCategoryBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

		c.setUsedBlock(w, x, y, z, candleCake)
		c.awardUsedHeld(hand)
		c.spendHeld(hand)
		return true
	}
	if hand != protocol.HandMain {
		return false
	}
	after, ok := rules.CakeBite(state)
	if !ok {
		return false
	}
	if registry.BlockOf(state) != registry.BlockCake && (registry.Item(held.ItemID) == registry.ItemFlintAndSteel || registry.Item(held.ItemID) == registry.ItemFireCharge) {
		return false // the candle is the item's to light
	}
	if c.player.GameMode() != protocol.GameModeCreative && c.player.Vitals().Food >= rules.MaxFood {
		return false
	}
	if registry.BlockOf(state) != registry.BlockCake {
		// The candle comes off with the first bite. vanilla: CandleCakeBlock.dropResources
		loot := rules.LootContext{BlockState: state, HasBlock: true, Level: c.srv.lootLevelAtBlock(w, x, y, z)}
		for _, stack := range rules.RollLoot(rules.BlockLootTable(registry.BlockOf(state)), &loot, c.srv.lootRng) {
			c.srv.spawnBlockDrop(w, x, y, z, stack)
		}
	}
	c.awardStat(player.Custom(registry.CustomStatEatCakeSlice), 1)
	vitals := c.player.UpdateVitals(func(v *player.Vitals) { rules.Eat(v, rules.CakeSlice) })
	c.sendHealth(vitals)
	c.gameEventAtCell(rules.GameEventEat, x, y, z)
	c.setUsedBlock(w, x, y, z, after)
	if after == airBlockState {
		c.gameEventAtCell(rules.GameEventBlockDestroy, x, y, z)
	}
	return true
}

// useFlowerPot pots or unpots a plant. vanilla: FlowerPotBlock
func (c *client) useFlowerPot(hand, x, y, z int32) bool {
	w := c.theWorld()
	held := c.heldItem(hand)
	item := registry.Item(held.ItemID)
	if held.Empty() {
		item = registry.ItemAir
	}
	after, taken, handBack, used := rules.FlowerPotUse(w.BlockAt(x, y, z), item, hand == protocol.HandMain)
	if !used {
		return false
	}
	if after != w.BlockAt(x, y, z) {
		c.setUsedBlock(w, x, y, z, after)
		c.gameEventAtCell(rules.GameEventBlockChange, x, y, z)
	}
	if taken {
		c.awardStat(player.Custom(registry.CustomStatPotFlower), 1)
		c.spendHeld(hand)
	}
	if handBack != registry.ItemAir {
		c.takeResult(protocol.ItemStack{ItemID: int32(handBack), Count: 1})
	}
	return true
}

// useComposter adds a compostable, or takes bone meal from a ready
// composter. vanilla: ComposterBlock.useItemOn, useWithoutItem,
// extractProduce
func (c *client) useComposter(hand, x, y, z int32) bool {
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	held := c.heldItem(hand)
	if !held.Empty() {
		ctx := c.srv.blockInteractLootContext(w, x, y, z, state, protocol.ItemStack{}, c.LootEntity())
		if after, inserted, used := rules.ComposterInsert(state, held, &ctx, c.srv.levelRng); used {
			if inserted {
				rose := int32(0)
				if after != state {
					rose = 1
					c.setUsedBlock(w, x, y, z, after)
					c.gameEventAtCell(rules.GameEventBlockChange, x, y, z)
				}
				c.srv.broadcastLevelEvent(w, rules.LevelEventComposterFill, x, y, z, rose)
				c.awardUsedHeld(hand)
				c.spendHeld(hand)
			}
			return true
		}
	}
	if hand != protocol.HandMain || !rules.ComposterReady(state) {
		return false
	}
	c.srv.spawnPoppedItem(w, x, y, z, protocol.ItemStack{ItemID: int32(registry.ItemBoneMeal), Count: 1})
	c.setUsedBlock(w, x, y, z, registry.BlockComposter.DefaultState())
	c.gameEventAtCell(rules.GameEventBlockChange, x, y, z)
	c.srv.broadcastSoundID(w, registry.SoundBlockComposterEmpty,
		protocol.SoundCategoryBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	return true
}

// useCauldron handles a held stack on a cauldron: buckets pour or fill,
// anything else goes to useCauldronItem.
// vanilla: CauldronInteractions.emptyBucket, fillBucket
func (c *client) useCauldron(hand, x, y, z int32) bool {
	w := c.theWorld()
	held := c.heldItem(hand)
	if held.Empty() {
		return false
	}
	use, refused, ok := rules.CauldronBucketUse(w.BlockAt(x, y, z), registry.Item(held.ItemID), rules.IsWater(w.BlockAt(x, y+1, z)))
	if !ok && !refused {
		return c.useCauldronItem(hand, x, y, z, held)
	}
	if !ok || refused {
		return ok
	}
	// vanilla: CauldronInteractions.fillBucket, emptyBucket
	state, allowed := c.pluginCauldronChange(w, x, y, z, use.State)
	if !allowed {
		return true
	}
	use.State = state
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	if use.Bucket == registry.ItemBucket {
		c.awardStat(player.Custom(registry.CustomStatFillCauldron), 1)
	} else {
		c.awardStat(player.Custom(registry.CustomStatUseCauldron), 1)
	}
	c.filledResult(hand, protocol.ItemStack{ItemID: int32(use.Bucket), Count: 1}, true)
	c.setUsedBlock(w, x, y, z, use.State)
	// The bucket's own events name no source entity.
	// vanilla: CauldronInteractions.fillBucket, emptyBucket
	fluidEvent := rules.GameEventFluidPickup
	if use.Bucket == registry.ItemBucket {
		fluidEvent = rules.GameEventFluidPlace
	}
	c.srv.gameEvent(w, fluidEvent, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, rules.GameEventContext{})
	c.srv.broadcastSoundID(w, use.Sound,
		protocol.SoundCategoryBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	return true
}

// useCauldronItem handles a bottle, dyed item, banner or shulker box on a
// cauldron. vanilla: CauldronInteractions.bootStrap
func (c *client) useCauldronItem(hand, x, y, z int32, held protocol.ItemStack) bool {
	w := c.theWorld()
	use, ok := rules.CauldronItemUse(w.BlockAt(x, y, z), held)
	if !ok {
		return false
	}
	state, allowed := c.pluginCauldronChange(w, x, y, z, use.State)
	if !allowed {
		return true
	}
	use.State = state
	// Only bottle and potion interactions count item use; dye, banner and
	// shulker box ones count their own custom stats.
	// vanilla: CauldronInteractions.bootStrap
	if name := registry.Item(held.ItemID); name == registry.ItemGlassBottle || name == registry.ItemPotion {
		c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	}
	if use.Stat != registry.NoCustomStat {
		c.awardStat(player.Custom(use.Stat), 1)
	}
	if use.InPlace {
		c.replaceHeldWith(hand, use.Result)
	} else {
		c.filledResult(hand, use.Result, use.LimitCreative)
	}
	before := w.BlockAt(x, y, z)
	c.setUsedBlock(w, x, y, z, use.State)
	c.cauldronItemEvents(w, x, y, z, before, use)
	if use.Sound != registry.SoundNone {
		c.srv.broadcastSoundID(w, use.Sound,
			protocol.SoundCategoryBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	}
	return true
}

// cauldronItemEvents posts what an item used on a cauldron made: block_change
// for a level lowered or taken, and fluid_place or fluid_pickup for a bottle.
// vanilla: LayeredCauldronBlock.lowerFillLevel, CauldronInteractions.bootStrap
func (c *client) cauldronItemEvents(w *world.World, x, y, z int32, before uint16, use rules.CauldronItem) {
	was, _ := registry.StateInt(before, registry.PropLevel)
	now, _ := registry.StateInt(use.State, registry.PropLevel) // 0 for the emptied cauldron
	lowered := registry.BlockOf(before) == registry.BlockWaterCauldron && now < was
	if lowered {
		c.srv.gameEvent(w, rules.GameEventBlockChange, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, rules.GameEventContext{Affected: use.State})
	}
	var fluid rules.GameEvent
	switch use.Sound {
	case registry.SoundItemBottleFill:
		fluid = rules.GameEventFluidPickup
	case registry.SoundItemBottleEmpty:
		fluid = rules.GameEventFluidPlace
	default:
		return
	}
	c.srv.gameEvent(w, fluid, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, rules.GameEventContext{})
}

// useWaterBottleOnMud is PotionItem.useOn: a water bottle on a non-bottom
// face of a convertible_to_mud block makes mud, with splash and empty
// sounds and five splash particles. vanilla: PotionItem.useOn
func (c *client) useWaterBottleOnMud(hand, x, y, z, face int32) bool {
	held := c.heldItem(hand)
	// Only the potion family runs PotionItem.useOn (ThrowablePotionItem extends
	// it without overriding), so other stacks with water potion_contents do not
	// convert. vanilla: PotionItem.useOn, ThrowablePotionItem
	if name := registry.Item(held.ItemID); name != registry.ItemPotion && name != registry.ItemSplashPotion && name != registry.ItemLingeringPotion {
		return false
	}
	contents, ok := held.PotionContentsOf()
	if !ok || !rules.PotionIsWater(contents) {
		return false
	}
	w := c.theWorld()
	mud, ok := rules.MudConversion(w.BlockAt(x, y, z), face)
	if !ok {
		return false
	}
	c.srv.broadcastSoundID(w,
		registry.SoundEntityGenericSplash, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	c.filledResult(hand, protocol.ItemStack{ItemID: int32(registry.ItemGlassBottle), Count: 1}, true)
	// The particles are a block above the cell (vanilla sendParticles' y+1),
	// scattered across the top.
	for range 5 {
		c.srv.sendParticles(w, protocol.ParticleSplash,
			float64(x)+c.srv.levelRng.Float64(), float64(y)+1, float64(z)+c.srv.levelRng.Float64(),
			0, 0, 0, 1.0, 1)
	}
	c.srv.broadcastSoundID(w,
		registry.SoundItemBottleEmpty, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	c.setUsedBlock(w, x, y, z, mud)
	// No entity: PotionItem.useOn posts it with a null source.
	c.srv.gameEvent(w, rules.GameEventFluidPlace, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, rules.GameEventContext{})
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	return true
}

// spendHeld takes one of the held item, except in creative. vanilla:
// ItemStack.consume
func (c *client) spendHeld(hand int32) {
	if c.player.GameMode() != protocol.GameModeCreative {
		_ = c.consumeHeld(hand)
	}
}

// useJukebox inserts a disc into an empty jukebox or ejects the one in it,
// reporting whether the click was spent. has_record decides: while set, an
// insert is refused even with an empty slot.
// vanilla: JukeboxBlock.useItemOn, useWithoutItem
func (c *client) useJukebox(hand, x, y, z int32) bool {
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockJukebox {
		return false
	}
	be := w.EnsureBlockEntity(x, y, z, world.KindJukebox)
	if be == nil {
		return false
	}
	if registry.StateBool(state, registry.PropHasRecord) {
		// With a song set, only the main hand ejects; the off hand's click is
		// consumed without it. vanilla: useWithoutItem
		if hand != protocol.HandMain {
			c.useSwing = swingNone
			return true
		}
		return c.ejectJukebox(w, be, x, y, z)
	}
	held := c.heldItem(hand)
	song, playable := registry.JukeboxSongForStack(held)
	if !playable {
		return false
	}
	one := held
	one.Count = 1
	inserted := false
	be.Use(func(b *world.BlockEntity) {
		if b.Slots[0].Empty() {
			b.Put(0, one)
			// The song starts now; a jukebox loaded without the ticks tag carries -1
			// and is not playing until this.
			b.TicksSinceSongStarted = 0
			inserted = true
		}
	})
	if !inserted {
		return false
	}
	c.spendHeld(hand)
	c.awardStat(player.Custom(registry.CustomStatPlayRecord), 1)
	c.srv.setJukeboxHasRecord(w, x, y, z, true)
	c.srv.broadcastLevelEvent(w,
		protocol.WorldEventJukeboxPlay, x, y, z, registry.JukeboxSongID(song))

	return true
}

// ejectJukebox pops the disc from a jukebox with has_record, stopping its
// song, clearing the property and dropping the disc above. A finished song
// sends no stop; only a set one does.
// vanilla: JukeboxBlockEntity.popOutTheItem
func (c *client) ejectJukebox(w *world.World, be *world.BlockEntity, x, y, z int32) bool {
	var disc protocol.ItemStack
	playing := false
	be.Use(func(b *world.BlockEntity) {
		disc = b.Slots[0]
		playing = b.JukeboxPlaying()
		b.Put(0, protocol.ItemStack{})
		b.TicksSinceSongStarted = -1
	})
	c.srv.setJukeboxHasRecord(w, x, y, z, false)
	if playing {
		c.srv.broadcastLevelEvent(w,
			protocol.WorldEventJukeboxStop, x, y, z, 0)

	}
	if !disc.Empty() {
		c.srv.spawnJukeboxRecord(w, x, y, z, disc)
	}
	return true
}

// useChiseledBookshelf inserts the held book into the clicked front cell or
// takes the book there, reporting whether the click was spent. A book over
// an empty cell goes in; an occupied cell is taken from with any hand (but
// only the main hand's click takes); other front cells spend the click with
// nothing done. The shelf redraws its bits and records the cell as last,
// which its comparator outputs.
// vanilla: ChiseledBookShelfBlock.useItemOn, useWithoutItem, addBook, removeBook
func (c *client) useChiseledBookshelf(hand, x, y, z int32, ctx rules.PlacementContext) bool {
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockChiseledBookshelf {
		return false
	}
	slot, hit := rules.ContainerHitSlot(registry.StateValue(state, registry.PropFacing), 2, 3, ctx)
	if !hit {
		return false
	}
	be := w.EnsureBlockEntity(x, y, z, world.KindChiseledBookshelf)
	if be == nil {
		return false
	}
	occupied := registry.StateBool(state, registry.BookshelfSlotProps[slot])
	if held := c.heldItem(hand); !held.Empty() && registry.Item(held.ItemID).InTag(registry.ItemTagIDBookshelfBooks) && !occupied {
		// The book the click put in, one of the held stack.
		one := held
		one.Count = 1
		be.Use(func(b *world.BlockEntity) {
			b.Put(slot, one)
			b.LastInteractedSlot = slot
		})
		c.awardUsedHeld(hand)
		c.spendHeld(hand)
		c.srv.broadcastSoundID(w,
			bookshelfSound("insert", one.ItemID), protocol.SoundCategoryBlock,
			float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

		c.srv.setBookshelfState(w, be, slot)
		return true
	}
	// The rest was vanilla's TRY_WITH_EMPTY_HAND, answered only by the main
	// hand's follow-up.
	if hand != protocol.HandMain {
		return false
	}
	if !occupied {
		// The front's cells take the click even when nothing happened.
		c.useSwing = swingNone
		return true
	}
	var book protocol.ItemStack
	be.Use(func(b *world.BlockEntity) {
		book = b.Slots[slot]
		if !book.Empty() {
			b.Put(slot, protocol.ItemStack{})
			b.LastInteractedSlot = slot
		}
	})
	c.srv.broadcastSoundID(w,
		bookshelfSound("pickup", book.ItemID), protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	if book.Empty() {
		// A cell the state marked occupied whose slot is empty (a stored compound
		// reads empty) played vanilla's sound and changed nothing else.
		return true
	}
	c.takeResult(book)
	c.srv.setBookshelfState(w, be, slot)
	c.gameEventAtCell(rules.GameEventBlockChange, x, y, z)
	return true
}

// useShelf spends a click on a shelf's front: a powered shelf swaps its
// three cells with the far end of the hotbar, an unpowered one swaps the
// clicked cell with the held stack. An off-hand click or a miss passes
// through before that, so the held item's use follows.
// vanilla: ShelfBlock.useItemOn
func (c *client) useShelf(hand, x, y, z int32, ctx rules.PlacementContext) bool {
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	if registry.BlockOf(state).Type() != registry.BlockTypeShelf {
		return false
	}
	if hand != protocol.HandMain {
		// vanilla answers the off hand with PASS before the front hit.
		return false
	}
	slot, hit := rules.ContainerHitSlot(registry.StateValue(state, registry.PropFacing), 1, 3, ctx)
	if !hit {
		return false
	}
	c.useSwing = swingServer
	if registry.StateBool(state, registry.PropPowered) {
		return c.shelfSwapHotbar(x, y, z)
	}
	return c.shelfSwapSingle(x, y, z, slot, state)
}

// shelfSwapSingle swaps the held stack with the clicked cell either way. A
// creative hand over an empty cell keeps its stack; an empty hand over an
// empty cell passes through to the item's use. The sound is take, swap or
// place by what moved. The shelf posts item_interact_finish, sourceless,
// even for a click that moved nothing, unless the stack put in the hand turns
// interact_vibrations off. vanilla: ShelfBlock.swapSingleItem
func (c *client) shelfSwapSingle(x, y, z int32, slot int, state uint16) bool {
	w := c.theWorld()
	be := w.EnsureBlockEntity(x, y, z, world.KindShelf)
	if be == nil {
		return false
	}
	var held, removed, newStack protocol.ItemStack
	var payload []byte
	swapped, present := false, false
	creative := c.player.GameMode() == protocol.GameModeCreative
	if hook := c.srv.shelfSwapHook; hook != nil {
		hook()
	}
	c.player.Inventory(func(inv *player.Inventory) {
		handSlot := player.HotbarSlot + int(inv.Selected)
		held = inv.Slots[handSlot]
		be.Use(func(b *world.BlockEntity) {
			removed = b.Slots[slot]
			// Lock order: inventory, entity, world. The mark fails when a break or
			// unload removed the entity since the lookup; a stack put into it then
			// would duplicate the broken block's spill.
			if !w.TouchEntity(b) {
				return
			}
			present = true
			if removed.Empty() && held.Empty() {
				// Nothing moves either way: vanilla passes the click through.
				return
			}
			b.Put(slot, held)
			swapped = true
		})
		if !swapped {
			return
		}
		if removed.Empty() && creative {
			newStack = held
		} else {
			newStack = removed
		}
		inv.Set(handSlot, newStack)
		payload = inv.WindowSlot(playerWindowID, int32(handSlot), newStack)
	})
	if present && registry.StackInteractVibrations(newStack) {
		c.srv.gameEvent(w, rules.GameEventItemInteractFinish, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, rules.GameEventContext{Affected: state})
	}
	if !swapped {
		return false
	}
	_ = c.writePacket(protocol.IDPlayContainerSlot, "set container slot", payload)
	c.broadcastEquipment()
	sound := registry.SoundBlockShelfTakeItem
	switch {
	case removed.Empty():
		sound = registry.SoundBlockShelfPlaceItem
	case !held.Empty():
		sound = registry.SoundBlockShelfSingleSwap
	}
	c.srv.broadcastSoundID(w,
		sound, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	c.srv.broadcastBlockEntity(w, x, y, z, be)
	return true
}

// shelfSwapHotbar swaps each cell of every shelf in the clicked one's side
// chain with a three-slot hotbar stretch anchored at the right: the
// rightmost shelf takes 6-8, each to its left the three before, so a chain
// of two takes 3-5 and 6-8. A powered shelf always spends the click, the
// multi-swap sound playing only when something moved; the held stack stays.
// Each shelf of the chain posts entity_interact, sourceless, moved or not.
// vanilla: ShelfBlock.swapHotbar
func (c *client) shelfSwapHotbar(x, y, z int32) bool {
	w := c.theWorld()
	chain := c.srv.physicsFor(w).ShelfChain(x, y, z)
	// Block entities are prepared before the inventory lock: reading them
	// takes the world lock, which the inventory closure must not want while the
	// tick loop takes them the other way round.
	type shelfBE struct {
		i       int
		x, y, z int32
		be      *world.BlockEntity
	}
	var touched []shelfBE
	for i, pos := range chain {
		be := w.EnsureBlockEntity(pos[0], pos[1], pos[2], world.KindShelf)
		if be == nil {
			continue
		}
		touched = append(touched, shelfBE{i, pos[0], pos[1], pos[2], be})
	}
	if hook := c.srv.shelfSwapHook; hook != nil {
		hook()
	}
	var payloads [][]byte
	var anySwapped bool
	c.player.Inventory(func(inv *player.Inventory) {
		for _, t := range touched {
			marked := false
			for s := range 3 {
				// From the right: the rightmost shelf takes 6-8, each to its left the three
				// before, so three shelves span 0-8. The bounds guard is dead in vanilla,
				// which caps the chain at three.
				hotbar := 9 - (len(chain)-t.i)*3 + s
				if hotbar < 0 || hotbar >= player.HotbarSlots {
					continue
				}
				inventorySlot := player.HotbarSlot + hotbar
				t.be.Use(func(b *world.BlockEntity) {
					cell, stack := b.Slots[s], inv.Slots[inventorySlot]
					if cell.Empty() && stack.Empty() {
						return
					}
					// Lock order: inventory, entity, world, once per shelf that still moves a
					// cell. A failed mark means a break or unload removed the shelf since the
					// lookup, so its cells are left alone rather than duplicating its spill.
					if !marked {
						if !w.TouchEntity(b) {
							return
						}
						marked = true
					}
					b.Put(s, stack)
					inv.Set(inventorySlot, cell)
					anySwapped = true
					payloads = append(payloads,
						inv.WindowSlot(playerWindowID, int32(inventorySlot), cell))
				})
			}
		}
	})
	for _, payload := range payloads {
		_ = c.writePacket(protocol.IDPlayContainerSlot, "set container slot", payload)
	}
	c.broadcastEquipment()
	for _, t := range touched {
		c.srv.gameEvent(w, rules.GameEventEntityInteract, float64(t.x)+0.5, float64(t.y)+0.5, float64(t.z)+0.5,
			rules.GameEventContext{Affected: w.BlockAt(t.x, t.y, t.z)})
		c.srv.broadcastBlockEntity(w, t.x, t.y, t.z, t.be)
	}
	c.useSwing = swingNone
	if anySwapped {
		c.useSwing = swingServer
		c.srv.broadcastSoundID(w,
			registry.SoundBlockShelfMultiSwap, protocol.SoundCategoryBlock,
			float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	}
	return true
}

// bookshelfSound is a shelf click's insert or pickup sound, the enchanted
// book's own for one. vanilla: ChiseledBookShelfBlock.addBook, removeBook
func bookshelfSound(kind string, item int32) registry.Sound {
	if registry.Item(item) == registry.ItemEnchantedBook {
		if kind == "insert" {
			return registry.SoundBlockChiseledBookshelfInsertEnchanted
		}
		return registry.SoundBlockChiseledBookshelfPickupEnchanted
	}
	if kind == "insert" {
		return registry.SoundBlockChiseledBookshelfInsert
	}
	return registry.SoundBlockChiseledBookshelfPickup
}

// useDecoratedPot adds one held item to the pot's slot when empty or the
// same item below its stack size, and wobbles the pot. A refused item or
// bare main hand wobbles the other way with the failure sound; an off-hand
// refusal falls through.
// vanilla: DecoratedPotBlock.useItemOn, useWithoutItem
func (c *client) useDecoratedPot(hand, x, y, z int32) bool {
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	if registry.BlockOf(state) != registry.BlockDecoratedPot {
		return false
	}
	be := w.EnsureBlockEntity(x, y, z, world.KindDecoratedPot)
	if be == nil {
		return false
	}
	held := c.heldItem(hand)
	inserted := false
	var pitch float32
	if !held.Empty() {
		one := held
		one.Count = 1
		be.Use(func(b *world.BlockEntity) {
			// A pending table rolls before the slot is read.
			// vanilla: DecoratedPotBlockEntity.getTheItem
			w.UnpackLoot(b)
			slot := b.Slots[0]
			maxCount := registry.StackMaxStackSize(held)
			fits := slot.Empty() || (slot.Same(held) && slot.Count < maxCount)
			// Mark first, so a pot broken since the lookup takes nothing.
			if !fits || !w.TouchEntity(b) {
				return
			}
			switch {
			case slot.Empty():
				b.Put(0, one)
				pitch = 1 / float32(maxCount)
			case slot.Same(held) && slot.Count < maxCount:
				slot.Count++
				b.Put(0, slot)
				pitch = float32(slot.Count) / float32(maxCount)
			default:
				return
			}
			inserted = true
		})
	}
	block := int32(registry.BlockIDs["minecraft:decorated_pot"])
	if inserted {
		c.awardUsedHeld(hand)
		c.spendHeld(hand)
		c.srv.tellEffect(w, world.Effect{
			SoundID: registry.SoundBlockDecoratedPotInsert, X: x, Y: y, Z: z,
			Volume: 1, Pitch: 0.7 + 0.5*pitch,
			HasBlockAction: true, BlockAction: potWobbleEvent,
			BlockActionData: potWobblePositive, BlockActionBlock: block,
		})
		c.srv.sendParticles(w, protocol.ParticleDustPlume, float64(x)+0.5, float64(y)+1.2, float64(z)+0.5, 0, 0, 0, 0, 7)
		c.gameEventAtCell(rules.GameEventBlockChange, x, y, z)
		return true
	}
	if hand != protocol.HandMain {
		return false
	}
	c.srv.tellEffect(w, world.Effect{
		SoundID: registry.SoundBlockDecoratedPotInsertFail, X: x, Y: y, Z: z,
		Volume: 1, Pitch: 1,
		HasBlockAction: true, BlockAction: potWobbleEvent,
		BlockActionData: potWobbleNegative, BlockActionBlock: block,
	})
	c.gameEventAtCell(rules.GameEventBlockChange, x, y, z)
	return true
}

// The pot's block event and wobble styles.
// vanilla: DecoratedPotBlockEntity.EVENT_POT_WOBBLES, WobbleStyle
const (
	potWobbleEvent    = 1
	potWobblePositive = 0
	potWobbleNegative = 1
)

// useBell rings a bell hit on its body, reporting whether it did; other
// clicks fall through to the held item. The ring is the block event the
// client swings the bell away from the clicked side by, then the sound at
// volume 2, both to the whole world since no client predicts them.
// vanilla: BellBlock.useWithoutItem, onHit, attemptToRing, BellBlockEntity.onHit
func (c *client) useBell(x, y, z int32, ctx rules.PlacementContext) bool {
	if !rules.BellHit(c.theWorld().BlockAt(x, y, z), ctx.Face, ctx.CursorY) {
		return false
	}
	if c.srv.ringBell(c.theWorld(), x, y, z, uint8(ctx.Face), c.gameEventContext(0), c) {
		c.awardStat(player.Custom(registry.CustomStatBellRing), 1)
	}
	return true
}

// ringBell rings the bell, swinging it away from dir (a Direction 3D data
// value: the click's side, or the facing for a redstone ring, which has no
// direction) and playing its sound.
// by is the client whose click rings it, nil on the tick goroutine; false
// means a plugin cancelled the ring.
// vanilla: BellBlock.attemptToRing, BellBlockEntity.onHit
func (s *Server) ringBell(w *world.World, x, y, z int32, dir uint8, ctx rules.GameEventContext, by *client) bool {
	if s.pluginOn(pluginruntime.BellRing) {
		e := &plugin.BellRingEvent{Block: s.pluginBlock(w, x, y, z), Face: plugin.BlockFace(dir)}
		if by != nil {
			e.Entity = by.pluginSelf()
			if !by.pluginFire(e, false) {
				return true
			}
		} else {
			s.plugins.Fire(e)
		}
		if e.Cancelled() {
			return false
		}
	}
	s.gameEvent(w, rules.GameEventBlockChange, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, ctx)
	s.tellEffect(w, world.Effect{
		SoundID:          registry.SoundBlockBellUse,
		X:                x,
		Y:                y,
		Z:                z,
		Volume:           2,
		Pitch:            1,
		HasBlockAction:   true,
		BlockAction:      1, // vanilla: BellBlock.EVENT_BELL_RING
		BlockActionData:  dir,
		BlockActionBlock: protocol.BlockBell,
	})
	return true
}

// filledResult swaps one held item for result. In creative the hand keeps
// its item and result is added unless the inventory already has an equal
// one (same components); limitCreative applies that rule, off for the
// mooshroom's bowl, which adds stew on every creative click.
// vanilla: ItemUtils.createFilledResult
func (c *client) filledResult(hand int32, result protocol.ItemStack, limitCreative bool) {
	held := c.heldItem(hand)
	if c.player.GameMode() == protocol.GameModeCreative {
		if !limitCreative {
			c.takeResult(result)
			return
		}
		has := false
		c.player.Inventory(func(inv *player.Inventory) {
			// contains checks carried slots, not the 2x2 grid, and matches components
			// (another potion is not the water bottle). vanilla: Inventory.contains
			for _, s := range inv.Slots[player.ArmorSlot:] {
				if !s.Empty() && s.Same(result) {
					has = true
				}
			}
		})
		if !has {
			c.takeResult(result)
		}
		return
	}
	if held.Count <= 1 {
		c.replaceHeldWith(hand, result)
		return
	}
	_ = c.consumeHeld(hand)
	c.takeResult(result)
}

// takeResult adds a use's result to the inventory and sends the changed
// slots. A survival overflow is thrown from the eyes with a throw's pickup
// delay; creative's is voided. vanilla: player.drop, hasInfiniteMaterials
func (c *client) takeResult(result protocol.ItemStack) {
	if left := c.addToInventory(result); left > 0 && c.player.GameMode() != protocol.GameModeCreative {
		// The leftover keeps the result's components: an enchanted book dropped
		// from a full inventory must stay that book.
		leftover := result
		leftover.Count = left
		c.throwItem(leftover)
	}
}
