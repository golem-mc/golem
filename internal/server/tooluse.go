package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// useTool handles a right-click with honeycomb or shears
// on a block, reporting whether it did; package rules decides the result.
// vanilla: HoneycombItem.useOn, ShearsItem.useOn
func (c *client) useTool(hand, x, y, z int32) bool {
	held := c.heldItem(hand)
	if held.Empty() {
		return false
	}
	var use rules.ToolUse
	var ok bool
	switch registry.Item(held.ItemID) {
	case registry.ItemHoneycomb:
		use, ok = rules.HoneycombWax(c.theWorld().BlockAt(x, y, z))
	case registry.ItemShears:
		use, ok = rules.ShearsTrim(c.theWorld().BlockAt(x, y, z))
	}
	if !ok {
		return false
	}
	c.applyToolUse(hand, x, y, z, held, use, "")
	return true
}

// useBlockTransformer runs Item.useOn's component after any subclass use.
// vanilla: Item.useOn, BlockTransformer.transformBlock
func (c *client) useBlockTransformer(hand, x, y, z, face int32) (bool, error) {
	held := c.heldItem(hand)
	transformer, present := registry.StackBlockTransformer(held)
	if !present {
		return false, nil
	}
	if transformer == registry.BlockTransformerHoe {
		return c.useHoe(hand, x, y, z, face)
	}
	w := c.theWorld()
	var use rules.ToolUse
	var ok bool
	heldBack := rules.TransformHeldBackByShield(hand, c.heldItem(protocol.HandOff), c.player.Appearance().Sneaking)
	switch transformer {
	case registry.BlockTransformerAxe:
		if heldBack {
			return false, nil
		}
		use, ok = rules.AxeConversion(w.BlockAt(x, y, z))
	case registry.BlockTransformerShovel:
		use, ok = rules.ShovelConversion(w, x, y, z, face, heldBack)
	}
	if !ok {
		return false, nil
	}
	c.applyToolUse(hand, x, y, z, held, use, transformer)
	c.itemUsedOn = true
	return true, nil
}

// applyToolUse applies the conversion's effects and charges its item or tool.
func (c *client) applyToolUse(hand, x, y, z int32, held protocol.ItemStack, use rules.ToolUse, transformer registry.BlockTransformer) {
	w := c.theWorld()
	// Axe, honeycomb and shears fire the trigger themselves before the block
	// changes; the shovel does not.
	// vanilla: BlockTransformer.transformBlock, HoneycombItem.useOn, ShearsItem.useOn
	if transformer != registry.BlockTransformerShovel {
		c.itemUsedOnBlockAdvancements(w, x, y, z, held)
	}
	if use.Sound != registry.SoundNone {
		c.srv.broadcastSoundIDExcept(w, c,
			use.Sound, protocol.SoundCategoryBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	}
	if use.LevelEvent != 0 {
		c.srv.broadcastLevelEventExcept(w, c, use.LevelEvent, x, y, z, 0)
		if use.HasPartner {
			c.srv.broadcastLevelEventExcept(w, c, use.LevelEvent, x+use.PartnerDX, y, z+use.PartnerDZ, 0)
		}
	}
	c.setUsedBlock(w, x, y, z, use.State)
	c.gameEventAt(rules.GameEventBlockChange, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, use.State)
	if use.HasPartner {
		px, pz := x+use.PartnerDX, z+use.PartnerDZ
		c.gameEventAt(rules.GameEventBlockChange, float64(px)+0.5, float64(y)+0.5, float64(pz)+0.5, w.BlockAt(px, y, pz))
	}
	c.awardUsedHeld(hand)
	if c.player.GameMode() != protocol.GameModeCreative {
		if use.Consumes {
			_ = c.consumeHeld(hand)
		} else if transformer != "" {
			c.spendBlockTransform(hand, held)
		} else {
			c.wearHeld(hand, 1)
		}
	}
}

// spendBlockTransform pays the built-in transform's consumeOnUse or itemDamagePerUse.
// vanilla: BlockTransformer.transformBlock, ItemStack.isStackable
func (c *client) spendBlockTransform(hand int32, held protocol.ItemStack) {
	if c.player.GameMode() == protocol.GameModeCreative {
		return
	}
	if registry.StackMaxStackSize(held) > 1 &&
		(registry.StackMaxDamage(held) == 0 || held.Unbreakable || held.Damage == 0) {
		_ = c.consumeHeld(hand)
	} else {
		c.wearHeld(hand, 1)
	}
}

// useItemOnBlock is the block's own response to an item or bare hand,
// before the item's: carving pumpkins, picking berries, dyeing signs, and
// using cakes, candles, flower pots, composters, cauldrons, bookshelves,
// active vaults, anchors (blockuse.go) and redstone ore.
// vanilla: Block.useItemOn, then useWithoutItem for the main hand
func (c *client) useItemOnBlock(hand, x, y, z int32, ctx rules.PlacementContext) (used bool) {
	// Handled uses default to SUCCESS; CONSUME and SUCCESS_SERVER branches override it.
	c.useSwing = swingPredicted
	defer func() {
		if !used {
			c.useSwing = swingNone
		}
	}()
	return c.carvePumpkin(hand, x, y, z, ctx) || c.harvestBerries(hand, x, y, z) ||
		c.useSignApplicator(hand, x, y, z) ||
		c.useCakeOrCandle(hand, x, y, z, ctx) || c.useFlowerPot(hand, x, y, z) ||
		c.useComposter(hand, x, y, z) || c.useCauldron(hand, x, y, z) ||
		c.useRespawnAnchorCharge(hand, x, y, z) ||
		c.useJukebox(hand, x, y, z) || c.useLectern(hand, x, y, z) || c.useChiseledBookshelf(hand, x, y, z, ctx) ||
		c.useShelf(hand, x, y, z, ctx) || c.useCampfire(hand, x, y, z) ||
		c.useBeehive(hand, x, y, z) || c.useDecoratedPot(hand, x, y, z) ||
		c.useVault(hand, x, y, z) || c.useCopperGolemStatue(hand, x, y, z) ||
		c.useRedstoneOre(hand, x, y, z)
}

// harvestBerries picks a ripe sweet berry bush or a berried cave vine and
// reports whether it did. vanilla: SweetBerryBushBlock.useItemOn and
// useWithoutItem, CaveVines.use
func (c *client) harvestBerries(hand, x, y, z int32) bool {
	if hand != protocol.HandMain {
		return false
	}
	w := c.theWorld()
	pick, ok := rules.BerryHarvest(w.BlockAt(x, y, z), c.heldItem(hand).ItemID == int32(registry.ItemBoneMeal))
	if !ok {
		return false
	}
	pick.Loot.Level = c.srv.lootLevelAtBlock(w, x, y, z)
	for _, stack := range rules.RollLoot(pick.Table, &pick.Loot, c.srv.lootRng) {
		c.srv.spawnBlockDrop(w, x, y, z, stack)
	}
	c.srv.broadcastSoundID(w,
		pick.Sound, protocol.SoundCategoryBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 0.8+c.srv.soundRng.Float32()*0.4)

	// Flag 2: clients are told, no neighbour is woken.
	c.srv.writeBlock(w, x, y, z, pick.After, nil, writeLight|writeLightAfter|writeNeighborShapes)
	c.gameEventAt(rules.GameEventBlockChange, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, pick.After)
	return true
}

// carvePumpkin handles shears on a pumpkin, reporting whether it did: four
// seeds out of the carved side, the carve sound for all, a carved pumpkin,
// one wear. vanilla: PumpkinBlock.useItemOn
func (c *client) carvePumpkin(hand, x, y, z int32, ctx rules.PlacementContext) bool {
	held := c.heldItem(hand)
	w := c.theWorld()
	if held.Empty() || held.ItemID != int32(registry.ItemShears) || registry.BlockOf(w.BlockAt(x, y, z)) != registry.BlockPumpkin {
		return false
	}
	if ctx.Face < 0 || int(ctx.Face) >= len(protocol.FaceOffset) {
		return false
	}
	state, dx, dz := rules.PumpkinCarve(ctx.Face, ctx.Yaw)
	loot := rules.LootContext{Level: c.srv.lootLevelAtBlock(w, x, y, z)}
	for _, stack := range rules.RollLoot("minecraft:carve/pumpkin", &loot, c.srv.lootRng) {
		c.srv.spawnItem(w, stack,
			float64(x)+0.5+float64(dx)*0.65, float64(y)+0.1, float64(z)+0.5+float64(dz)*0.65,
			0.05*float64(dx)+c.srv.popRng.Float64()*0.02, 0.05, 0.05*float64(dz)+c.srv.popRng.Float64()*0.02, 0)
	}
	c.srv.broadcastSoundID(w,
		registry.SoundBlockPumpkinCarve, protocol.SoundCategoryBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	c.gameEventAtCell(rules.GameEventShear, x, y, z)
	c.setUsedBlock(w, x, y, z, state)
	c.awardUsedHeld(hand)
	c.wearHeld(hand, 1)
	return true
}

// setUsedBlock writes the block a player's use changed and tells it at once:
// the use's echoes and drops follow it on the wire.
func (c *client) setUsedBlock(w *world.World, x, y, z int32, state uint16) {
	c.srv.writeBlock(w, x, y, z, state, nil, writeLight|writeLightAfter|writeWake)
}
