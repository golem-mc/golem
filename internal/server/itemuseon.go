package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// useOnClick is the click an item's use-on handles: hand, block and
// placement context.
type useOnClick struct {
	hand, x, y, z int32
	ctx           rules.PlacementContext
}

// itemUseOn is one item's handler for a click on a block. matches picks the
// stacks it is tried for and must hold for every stack use would handle;
// use reports whether it handled the click, its error counting only then.
// vanilla: Item.useOn
type itemUseOn struct {
	matches func(protocol.ItemStack) bool
	use     func(c *client, u useOnClick) (used bool, err error)
}

// itemUsesOn is tried in order; the first that handles the click ends it,
// and past the last the held item's block form is tried. A stack can match
// multiple entries through its components, so order matters.
var itemUsesOn = []itemUseOn{
	// A lead's useOn on a fence, after the block's sneak-suppressed use: a
	// sneaking hand still ties its led mobs to the fence's knot, under the
	// mayBuild gate every item use shares. Only a lead runs it; a sneak with
	// anything else is that item's use, and a non-sneaking click already tied
	// at the block. vanilla: LeadItem.useOn, LeadItem.bindPlayerMobs
	{itemIs(registry.ItemLead), func(c *client, u useOnClick) (bool, error) {
		handled, err := c.useLeadOnFence(u.hand, u.x, u.y, u.z)
		if handled && err == nil {
			c.awardItemStat(registry.StatTypeUsed, c.heldItem(u.hand).ItemID, 1)
		}
		return handled, err
	}},
	// The debug stick handles every use, result or not, so nothing else of the
	// item runs. vanilla: DebugStickItem.useOn
	{itemIs(registry.ItemDebugStick), func(c *client, u useOnClick) (bool, error) {
		if !c.holdsDebugStick(u.hand) {
			return false, nil
		}
		// Only a SUCCESS result counts as the item's use.
		// vanilla: ItemStack.useOn
		if c.debugStick(u.hand, u.x, u.y, u.z, true) {
			c.awardItemStat(registry.StatTypeUsed, c.heldItem(u.hand).ItemID, 1)
		} else {
			c.useSwing = swingNone
		}
		return true, nil
	}},
	{func(s protocol.ItemStack) bool {
		_, egg := registry.SpawnEggEntity(registry.Item(s.ItemID))
		return egg
	}, func(c *client, u useOnClick) (bool, error) {
		return c.useSpawnEgg(u.hand, u.x, u.y, u.z, u.ctx.Face), nil
	}},
	// Flint and steel makes fire instead of placing, and fire in a complete
	// obsidian frame lights a portal. It handles the click itself.
	{itemIs(registry.ItemFlintAndSteel), func(c *client, u useOnClick) (bool, error) {
		return c.useFlintAndSteel(u.hand, u.x, u.y, u.z, u.ctx.Face)
	}},
	{itemIs(registry.ItemFireCharge), func(c *client, u useOnClick) (bool, error) {
		return c.useFireCharge(u.hand, u.x, u.y, u.z, u.ctx.Face)
	}},
	// An eye of ender fills an end portal frame, and the last eye of a ring
	// opens the portal. It handles the click itself.
	// vanilla: EnderEyeItem.useOn
	{itemIs(registry.ItemEnderEye), func(c *client, u useOnClick) (bool, error) {
		return c.useEnderEye(u.hand, u.x, u.y, u.z)
	}},
	// A brush on a block starts brushing, handling the click.
	// vanilla: BrushItem.useOn
	{itemIs(registry.ItemBrush), func(c *client, u useOnClick) (bool, error) {
		c.useSwing = swingNone
		return c.useBrush(u.hand), nil
	}},
	{itemIs(registry.ItemHoneycomb, registry.ItemShears), func(c *client, u useOnClick) (bool, error) {
		if !c.useTool(u.hand, u.x, u.y, u.z) {
			return false, nil
		}
		c.itemUsedOn = true
		return true, nil
	}},
	{itemIs(registry.ItemBoneMeal), func(c *client, u useOnClick) (bool, error) {
		return c.useBoneMeal(u.hand, u.x, u.y, u.z, u.ctx.Face)
	}},
	// A water bottle turns convertible_to_mud dirt to mud (its useOn).
	// vanilla: PotionItem.useOn
	{itemIs(registry.ItemPotion, registry.ItemSplashPotion, registry.ItemLingeringPotion),
		func(c *client, u useOnClick) (bool, error) {
			return c.useWaterBottleOnMud(u.hand, u.x, u.y, u.z, u.ctx.Face), nil
		}},
	// A painting hangs on the clicked face instead of placing a block: its
	// useOn, after the blocks' uses and before the item's block form. Item
	// frames do the same with a mayPlace that admits flat faces.
	// vanilla: HangingEntityItem.useOn, ItemFrameItem.mayPlace
	{itemIs(registry.ItemPainting), func(c *client, u useOnClick) (bool, error) {
		return c.placePainting(u.hand, u.x, u.y, u.z, u.ctx.Face)
	}},
	{itemIs(registry.ItemItemFrame, registry.ItemGlowItemFrame), func(c *client, u useOnClick) (bool, error) {
		return c.placeItemFrame(u.hand, u.x, u.y, u.z, u.ctx.Face)
	}},
	// An armor stand spawns on the clicked block (its useOn), an entity rather
	// than a block, like the painting. vanilla: ArmorStandItem.useOn
	{itemIs(registry.ItemArmorStand), func(c *client, u useOnClick) (bool, error) {
		return c.placeArmorStand(u.hand, u.x, u.y, u.z, u.ctx.Face)
	}},
	// A cushion goes down on a block's top; any other face refuses the use.
	// vanilla: CushionItem.useOn
	{isCushionItem, func(c *client, u useOnClick) (bool, error) {
		return c.placeCushion(u.hand, u.x, u.y, u.z, u.ctx)
	}},
	// vanilla: EndCrystalItem.useOn
	{itemIs(registry.ItemEndCrystal), func(c *client, u useOnClick) (bool, error) {
		return c.placeEndCrystal(u.hand, u.x, u.y, u.z)
	}},
	// A firework rocket launches from the clicked face (its useOn).
	// vanilla: FireworkRocketItem.useOn
	{itemIs(registry.ItemFireworkRocket), func(c *client, u useOnClick) (bool, error) {
		return c.launchFireworkOnFace(u.hand, u.x, u.y, u.z, u.ctx.Face,
			u.ctx.CursorX, u.ctx.CursorY, u.ctx.CursorZ)
	}},
	// A minecart spawns on the clicked rail (its useOn).
	// vanilla: MinecartItem.useOn
	{itemIs(registry.ItemMinecart, registry.ItemChestMinecart, registry.ItemFurnaceMinecart,
		registry.ItemTntMinecart, registry.ItemHopperMinecart, registry.ItemCommandBlockMinecart), func(c *client, u useOnClick) (bool, error) {
		return c.placeMinecart(u.hand, u.x, u.y, u.z)
	}},
	// A compass on a lodestone becomes a lodestone compass (its useOn).
	// vanilla: CompassItem.useOn
	{itemIs(registry.ItemCompass), func(c *client, u useOnClick) (bool, error) {
		handled, err := c.lockLodestone(u.hand, u.x, u.y, u.z)
		if handled {
			c.itemUsedOn = true
		}
		return handled, err
	}},
	// A map on a banner toggles its marker, and handles the click either way.
	// vanilla: MapItem.useOn
	{func(s protocol.ItemStack) bool { return registry.Item(s.ItemID).IsMap() }, func(c *client, u useOnClick) (bool, error) {
		return c.toggleMapBanner(u.hand, u.x, u.y, u.z), nil
	}},
	blockTransformerUseOn,
	// A lily pad or frogspawn only places through its use in air; its useOn
	// handles the click with nothing placed.
	// vanilla: PlaceOnWaterBlockItem.useOn
	{func(s protocol.ItemStack) bool { return rules.PlacesOnWater(registry.Item(s.ItemID)) }, func(c *client, _ useOnClick) (bool, error) {
		c.useSwing = swingNone
		return true, nil
	}},
}

// useHeldItemOn runs the held stack's itemUsesOn entries in order,
// reporting whether one handled the click, with its error.
func (c *client) useHeldItemOn(u useOnClick) (used bool, err error) {
	held := c.heldItem(u.hand)
	for _, entry := range itemUsesOn {
		if !entry.matches(held) {
			continue
		}
		c.useSwing = swingPredicted
		if used, err := entry.use(c, u); used {
			return true, err
		}
		c.useSwing = swingNone
	}
	return false, nil
}

// itemIs matches the stacks of the given items.
func itemIs(items ...registry.Item) func(protocol.ItemStack) bool {
	return func(s protocol.ItemStack) bool { return slices.Contains(items, registry.Item(s.ItemID)) }
}

// ownsUseOn excludes overrides that never call Item.useOn. Shears, compasses
// and maps reach it after their own use; block items reach it after placement.
// vanilla: Item.useOn and its overrides
var ownsUseOn = func() []bool {
	items := make([]bool, registry.ItemCount)
	for _, item := range []registry.Item{
		registry.ItemBoneMeal, registry.ItemFlintAndSteel, registry.ItemFireCharge,
		registry.ItemEnderEye, registry.ItemLead, registry.ItemDebugStick, registry.ItemBrush,
		registry.ItemHoneycomb, registry.ItemPotion, registry.ItemSplashPotion, registry.ItemLingeringPotion,
		registry.ItemPainting, registry.ItemItemFrame, registry.ItemGlowItemFrame,
		registry.ItemArmorStand, registry.ItemEndCrystal, registry.ItemFireworkRocket,
		registry.ItemMinecart, registry.ItemChestMinecart, registry.ItemFurnaceMinecart,
		registry.ItemTntMinecart, registry.ItemHopperMinecart, registry.ItemCommandBlockMinecart,
	} {
		items[item] = true
	}
	for item := range registry.Item(registry.ItemCount) {
		_, egg := registry.SpawnEggEntity(item)
		_, cushion := cushionColorOfItem(item)
		items[item] = items[item] || egg || cushion
	}
	return items
}()

var blockTransformerUseOn = itemUseOn{
	matches: func(s protocol.ItemStack) bool {
		item := registry.Item(s.ItemID)
		if item < 0 || int(item) >= len(ownsUseOn) || ownsUseOn[item] {
			return false
		}
		if _, block := item.Block(); block {
			return false
		}
		_, ok := registry.StackBlockTransformer(s)
		return ok
	},
	use: func(c *client, u useOnClick) (bool, error) {
		return c.useBlockTransformer(u.hand, u.x, u.y, u.z, u.ctx.Face)
	},
}
