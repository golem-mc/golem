package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// useHoe tills the clicked block (top or side), reporting whether it
// handled the click. The sound skips the clicker; rooted dirt drops hanging
// roots; one item or wear, none in creative. A shield in the off hand holds it back
// (rules.TransformHeldBackByShield). vanilla: BlockTransformer.transformBlock
func (c *client) useHoe(hand, x, y, z, face int32) (bool, error) {
	held := c.heldItem(hand)
	if held.Empty() {
		return false, nil
	}
	transformer, _ := registry.StackBlockTransformer(held)
	if transformer != registry.BlockTransformerHoe ||
		rules.TransformHeldBackByShield(hand, c.heldItem(protocol.HandOff), c.player.Appearance().Sneaking) {
		return false, nil
	}
	w := c.theWorld()
	next, table, ok := rules.HoeTill(w, x, y, z, face)
	if !ok {
		return false, nil
	}
	var stacks []protocol.ItemStack
	if table != "" {
		ctx := c.srv.blockInteractLootContext(w, x, y, z, w.BlockAt(x, y, z), held, c.LootEntity())
		stacks = rules.RollLoot(table, &ctx, c.srv.lootRng)
	}
	if err := c.changeBlock(x, y, z, next); err != nil {
		return true, err
	}
	c.gameEventAt(rules.GameEventBlockChange, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, next)
	c.srv.broadcastSoundIDExcept(w, c,
		registry.SoundItemHoeTill, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)

	for _, stack := range stacks {
		// Thrown from the clicked face, not the centre (vanilla:
		// HoeItem.changeIntoStateAndDropItem, Block.popResourceFromFace).
		c.srv.spawnBlockDropFromFace(w, x, y, z, face, stack)
	}
	c.awardUsedHeld(hand)
	c.spendBlockTransform(hand, held)
	return true, nil
}

// useBoneMeal grows what the clicked block's rule grows, else the water the
// face opens onto (vanilla: BoneMealItem.useOn). A valid target spends the
// bone meal whether or not its roll passed; drops go out at the event
// position.
func (c *client) useBoneMeal(hand, x, y, z, face int32) (bool, error) {
	held := c.heldItem(hand)
	if held.Empty() || held.ItemID != int32(registry.ItemBoneMeal) {
		return false, nil
	}
	w := c.theWorld()
	changes, eventPos, drops, used, swung := c.srv.physicsFor(w).BoneMeal(x, y, z, face, c.player.UUID)
	if !used {
		return false, nil
	}
	c.srv.boneMealResults(w, eventPos, drops, changes)
	c.awardUsedHeld(hand)
	// The vibration reads the stack after the growth shrank it, in creative
	// too (the count comes back only after useOn), so the last bone meal
	// posts none. vanilla: BoneMealItem.useOn, growCrop
	if held.Count > 1 {
		c.useVibration(held, rules.GameEventItemInteractFinish)
	}
	// The bone meal is spent outside creative. vanilla: ItemStack.consume
	if c.player.GameMode() != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return true, err
		}
	}
	// growCrop is SUCCESS_SERVER; water plants are SUCCESS, predicted locally
	// and relayed to the other players. vanilla: BoneMealItem.useOn
	c.useSwing = swingPredicted
	if swung {
		c.useSwing = swingServer
	}
	return true, nil
}

// boneMealResults sends a growth's results: block changes, the event 1505
// sparkle, and its drops.
func (s *Server) boneMealResults(w *world.World, eventPos [3]int32, drops []registry.Item, changes []world.BlockChange) {
	s.tellBlockChanges(w, changes)
	s.broadcastLevelEvent(w,
		protocol.WorldEventBonemeal, eventPos[0], eventPos[1], eventPos[2], 15)

	for _, drop := range drops {
		s.spawnBlockDrop(w, eventPos[0], eventPos[1], eventPos[2],
			protocol.ItemStack{ItemID: int32(drop), Count: 1})
	}
}
