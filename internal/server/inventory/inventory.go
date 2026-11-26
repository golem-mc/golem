// Package inventory owns what a click does to a window: the window model,
// click modes, crafting results, shift-click routing and stack rules. It
// sends nothing and takes no locks; clicks run under the caller's.
//
// Every window lays out its own slots (a Container) followed by the player's
// main inventory and hotbar.
package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// Kind is which window the player has open: their own, or one of the
// containers an Open Screen packet put in front of them.
type Kind int8

// The kinds of window this server serves.
const (
	KindPlayer Kind = iota
	KindChest
	KindCrafting
	KindFurnace
	KindBlastFurnace
	KindSmoker
	KindBrewingStand
	KindCrafter
	KindEnchant
	KindBarrel
	KindShulker
	KindEnderChest
	KindDispenser
	KindDropper
	KindHopper
	KindAnvil
	KindDoubleChest
	// KindJukebox, the decorated pot's and the shelf's kinds (one, one and three
	// slots) open no window but are hopper targets. The three composter kinds
	// are WorldlyContainerHolder's input (one compostable, top face) and output
	// (one bone meal, bottom) and the empty container a full one keeps until its
	// ready tick.
	KindJukebox
	KindStonecutter
	// KindSmithing is the smithing table: template, base and addition inputs
	// and their result.
	KindSmithing
	KindGrindstone
	// KindBeacon is the beacon's window: one payment slot the window
	// itself keeps, the beacon's own numbers in its block entity.
	KindBeacon
	KindDecoratedPot
	KindShelf
	// The chiseled bookshelf's six are the same again, its own item rule
	// beside: a hopper moves only a bookshelf's books in.
	KindChiseledBookshelf
	KindComposterInput
	KindComposterOutput
	KindComposterFull
	// KindCartography is the cartography table: map and addition inputs and
	// their result.
	// vanilla: CartographyTableMenu
	KindCartography
	// KindMerchant is the villager's trading window: two payment slots and
	// the result the merchant's offers make of them. vanilla: MerchantMenu
	KindMerchant
	// KindLectern is the lectern: its book is the only slot, without the
	// player's things, and nothing can be clicked.
	// vanilla: LecternMenu
	KindLectern
	// KindHorse is a horse, donkey, mule or llama's screen: saddle, body armour,
	// a chested mount's chest slots (View.Columns of three rows), then the
	// player's things.
	// vanilla: HorseInventoryMenu
	KindHorse
	// KindLoom is the loom's window: its banner, dye and pattern item, and
	// the banner one more layer of the selected pattern makes. vanilla: LoomMenu
	KindLoom
	// KindMenu is a plugin's chest menu: View.Rows rows of nine slots no
	// block or entity holds, then the player's things.
	KindMenu
)

// Cooking reports whether a window is one of the three cooking containers
// (ingredient, fuel and result slots, fire and progress).
func (k Kind) Cooking() bool {
	return k == KindFurnace || k == KindBlastFurnace || k == KindSmoker
}

// EntitySlots reports whether a window's own slots are a block entity's.
func (k Kind) EntitySlots() bool {
	switch k {
	case KindChest, KindDoubleChest, KindFurnace, KindBlastFurnace, KindSmoker,
		KindBrewingStand, KindCrafter, KindBarrel, KindShulker,
		KindDispenser, KindDropper, KindHopper, KindLectern:
		return true
	}
	return false
}

// HasLid reports whether a window's container counts its viewers.
func (k Kind) HasLid() bool {
	switch k {
	case KindChest, KindDoubleChest, KindBarrel, KindEnderChest, KindShulker:
		return true
	}
	return false
}

// Layout is a window's protocol-fixed shape: menu type, the slot the
// player's inventory begins at, and the title translation key.
type Layout struct {
	Menu    int32
	InvBase int32
	Title   string
}

// layouts are the shapes of each kind of window. The player's own window
// is never opened with a packet, so only its InvBase is fixed.
var layouts = map[Kind]Layout{
	KindPlayer:   {InvBase: player.MainSlot},
	KindCrafting: {Menu: protocol.MenuCrafting, InvBase: 10, Title: "container.crafting"},
	KindChest:    {Menu: protocol.MenuGeneric9x3, InvBase: 27, Title: "container.chest"},
	// vanilla: ChestBlock.MENU_PROVIDER_COMBINER
	KindDoubleChest:  {Menu: protocol.MenuGeneric9x6, InvBase: 54, Title: "container.chestDouble"},
	KindFurnace:      {Menu: protocol.MenuFurnace, InvBase: 3, Title: "container.furnace"},
	KindBlastFurnace: {Menu: protocol.MenuBlastFurnace, InvBase: 3, Title: "container.blast_furnace"},
	KindSmoker:       {Menu: protocol.MenuSmoker, InvBase: 3, Title: "container.smoker"},
	KindBrewingStand: {Menu: protocol.MenuBrewingStand, InvBase: 5, Title: "container.brewing"},
	// vanilla: CrafterMenu - the 3x3 and the player's things, the result
	// slot last.
	KindCrafter:     {Menu: protocol.MenuCrafter, InvBase: 9, Title: "container.crafter"},
	KindEnchant:     {Menu: protocol.MenuEnchantment, InvBase: 2, Title: "container.enchant"},
	KindBarrel:      {Menu: protocol.MenuGeneric9x3, InvBase: 27, Title: "container.barrel"},
	KindShulker:     {Menu: protocol.MenuShulkerBox, InvBase: 27, Title: "container.shulkerBox"},
	KindEnderChest:  {Menu: protocol.MenuGeneric9x3, InvBase: 27, Title: "container.enderchest"},
	KindDispenser:   {Menu: protocol.MenuGeneric3x3, InvBase: 9, Title: "container.dispenser"},
	KindDropper:     {Menu: protocol.MenuGeneric3x3, InvBase: 9, Title: "container.dropper"},
	KindHopper:      {Menu: protocol.MenuHopper, InvBase: 5, Title: "container.hopper"},
	KindAnvil:       {Menu: protocol.MenuAnvil, InvBase: 3, Title: "container.repair"},
	KindStonecutter: {Menu: protocol.MenuStonecutter, InvBase: 2, Title: "container.stonecutter"},
	// vanilla: SmithingMenu's slots - template, base, addition, result -
	// with the player's things from the number after them.
	KindSmithing:   {Menu: protocol.MenuSmithing, InvBase: 4, Title: "container.upgrade"},
	KindGrindstone: {Menu: protocol.MenuGrindstone, InvBase: 3, Title: "container.grindstone_title"},
	// vanilla: BeaconMenu - the payment slot, then the player's things.
	KindBeacon: {Menu: protocol.MenuBeacon, InvBase: 1, Title: "container.beacon"},
	// vanilla: CartographyTableMenu - the map, the addition beside it,
	// the result, then the player's things.
	KindCartography: {Menu: protocol.MenuCartography, InvBase: 3, Title: "container.cartography_table"},
	// vanilla: MerchantMenu - the two payments, the result, then the
	// player's things.
	KindMerchant: {Menu: protocol.MenuMerchant, InvBase: 3, Title: "entity.minecraft.villager"},
	// vanilla: LecternMenu - one slot and no player inventory.
	KindLectern: {Menu: protocol.MenuLectern, InvBase: 1, Title: "container.lectern"},
	// vanilla: LoomMenu - banner, dye, pattern item, result, then the
	// player's things.
	KindLoom: {Menu: protocol.MenuLoom, InvBase: 4, Title: "container.loom"},
	// The mount screen opens with its own packet and no menu type; its
	// player section starts at HorseBaseSlots plus the chest slots.
	KindHorse: {InvBase: HorseBaseSlots},
	KindMenu:  {Title: "container.chest"},
}

// Layout returns the window shape the protocol fixes for this kind.
func (k Kind) Layout() Layout { return layouts[k] }

// The crafting slots of the two windows that have them, in each window's own
// numbering: the result is slot 0 in both, followed by the grid.
const (
	playerCraftingResult = 0
	playerCraftingGrid   = 1
	playerGridSlots      = 4
	playerGridWidth      = 2

	craftingTableResult = 0
	craftingTableGrid   = 1

	// CraftingGridCells is how many cells a crafting table window's 3x3
	// grid holds.
	CraftingGridCells = 9

	craftingGridWidth = 3

	// furnaceResultSlot is the output in window and block-entity numbering
	// alike; taking from it pays a smelt's experience.
	// vanilla: FurnaceResultSlot
	furnaceResultSlot = 2

	// brewingBottleSlots is how many of a brewing stand window's first slots
	// are potion bottles.
	brewingBottleSlots = 3

	// AnvilResultSlot gives and never takes; its stack is recomputed at take
	// time from the inputs and typed name, Menu keeping only the last
	// recompute for display.
	AnvilResultSlot = 2

	// StonecutterResultSlot gives and never takes: the picked recipe's cut of
	// the input, kept in Menu.
	StonecutterResultSlot = 1

	// SmithingResultSlot follows the three inputs; it gives and never takes,
	// holding the last recompute of their result.
	// vanilla: SmithingMenu.RESULT_SLOT
	SmithingResultSlot = 3

	// GrindstoneResultSlot gives and never takes: the stripped or merged stack
	// recomputed in Menu from the two inputs.
	GrindstoneResultSlot = 2

	// CartographyResultSlot gives and never takes: the zoomed, cloned or locked
	// map recomputed in Menu from the two inputs.
	// vanilla: CartographyTableMenu.RESULT_SLOT
	CartographyResultSlot = 2

	// LoomResultSlot gives and never takes: the banner plus the selected layer.
	// vanilla: LoomMenu.resultSlot
	LoomResultSlot = 3

	// MerchantResultSlot gives and never takes: the offer the payments buy.
	// vanilla: MerchantMenu.RESULT_SLOT
	MerchantResultSlot = 2
)

// The mount screen's fixed slots: the saddle and the body armour, which
// the chest slots follow.
// vanilla: AbstractMountInventoryMenu.SLOT_SADDLE, SLOT_BODY_ARMOR
const (
	HorseSaddleSlot = 0
	HorseArmorSlot  = 1
	HorseBaseSlots  = 2

	// HorseChestRows is the rows of chest slots a mount with a chest has.
	HorseChestRows = 3
)

// Fitter is a Container whose slots refuse what its owner cannot wear: a
// mount's saddle and armour slots.
type Fitter interface {
	// Fits reports whether the given one of the container's slots takes the
	// stack.
	Fits(slot int, stack protocol.ItemStack) bool
}

// Container is a container window's block-entity slots. Clicks on them run
// under the entity's lock, which the caller holds.
type Container interface {
	// Slot is what the given one of the container's slots holds.
	Slot(slot int) protocol.ItemStack

	// Put replaces what the given one of the container's slots holds.
	Put(slot int, stack protocol.ItemStack)
}

// View is the open window in click-logic terms: kind, window ID, crafting
// grid, menu slots and container slots. The server builds it under the locks
// it already holds; the player's part is the player's inventory.
type View struct {
	// Kind is which window this is, and ID the number the client's
	// packets address it by.
	Kind Kind
	ID   int32

	// Grid is the crafting table's 3x3, shared not copied: a click's
	// consumption writes what it changed back where it lives.
	Grid []protocol.ItemStack

	// Menu is the window's slots that are neither grid nor entity: the
	// enchanting table's item and lapis, and the anvil's, stonecutter's,
	// smithing table's and grindstone's inputs with their result's stored
	// stack between recomputes.
	Menu []protocol.ItemStack

	// Entity is the container's own slots, nil in a window without any -
	// the player's own and a crafting table's.
	Entity Container

	// Columns is the mount screen's chest columns, 0 for a mount without a
	// chest. Entity's slots are the saddle, the armour, then the chest's.
	Columns int32

	// Rows is a KindMenu window's chest rows, 1 to 6.
	Rows int32

	// AnvilName is what the player has typed into the anvil's name field.
	AnvilName string

	// StonecutterRecipe is the selected cut's resource location, empty when none is selected.
	StonecutterRecipe string

	// Offers is the merchant's list of trades, shared with it, and Hint the
	// offer the player last selected. Only the merchant window has them.
	Offers []Offer
	Hint   int32

	// Loom is the loom's window, whose patterns and selection a take
	// remakes; nil elsewhere.
	Loom *Window
}

// HorseSlotNames is the equipment slot each mount-screen slot takes, by
// HorseSaddleSlot and HorseArmorSlot.
var HorseSlotNames = [...]string{HorseSaddleSlot: "saddle", HorseArmorSlot: "body"}

// InvBase is where the player's inventory begins in the window's slot
// numbering.
func (v View) InvBase() int32 {
	if v.Kind == KindMenu {
		return 9 * v.Rows
	}
	if v.Kind == KindHorse {
		return HorseBaseSlots + HorseChestRows*v.Columns
	}
	return v.Kind.Layout().InvBase
}

// Slots is how many slot numbers this window addresses.
func (v View) Slots() int32 {
	if v.Kind == KindPlayer {
		return player.OffHandSlot + 1
	}
	if v.Kind == KindLectern {
		return 1
	}
	if v.Kind == KindCrafter {
		// The crafter appends its result slot after the player's things, the
		// one window whose own slots are not all first.
		return v.InvBase() + player.MainInvSlots + 1
	}
	return v.InvBase() + player.MainInvSlots
}

// windowSlotOfInv maps a player-window slot to the open window's number;
// false for armour and off hand, which only the player's own window has.
func (v View) windowSlotOfInv(invSlot int) (int32, bool) {
	switch {
	case v.Kind == KindLectern:
		return 0, false
	case invSlot >= player.MainSlot && invSlot < player.MainSlot+player.MainInvSlots:
		return v.InvBase() + int32(invSlot-player.MainSlot), true
	case v.Kind == KindPlayer:
		return int32(invSlot), true
	}
	return 0, false
}

// slotLoc is which store a window slot's stack lives in.
type slotLoc int8

const (
	// slotAbsent is a number this window does not address: out of range,
	// or a player's-own-window slot another window does not carry.
	slotAbsent slotLoc = iota
	// slotResult is the crafting result, computed from the grid, which
	// nothing stores a stack for.
	slotResult
	// slotInv is one of the player's inventory slots.
	slotInv
	// slotGrid is a cell of the crafting table window's 3x3 grid.
	slotGrid
	// slotMenu is a window's own slot that is neither grid nor entity.
	slotMenu
	// slotAnvilResult is the anvil's result, invented at take time.
	slotAnvilResult
	// slotStonecutterResult is the stonecutter's result, the recipe the
	// buttons picked shown as a stack no click writes but a take.
	slotStonecutterResult
	// slotSmithingResult is the smithing table's result, what its three
	// inputs last recomputed to and no click writes but a take.
	slotSmithingResult
	// slotGrindstoneResult is the grindstone's result, the stack the
	// inputs' recompute last made, served and never stored.
	slotGrindstoneResult
	// slotCartographyResult is the cartography table's result, the map
	// its two inputs last made, served and never stored.
	slotCartographyResult
	// slotLoomResult is the loom's result, the patterned banner served and
	// never stored.
	slotLoomResult
	// slotMerchantResult is the merchant window's result, the offer the
	// payment slots last paid for, served and never stored.
	slotMerchantResult
	// slotEntity is one of the window's block entity's slots.
	slotEntity
)

// resolve maps a window slot number to its store and the index within it.
func (v View) resolve(slot int32) (slotLoc, int) {
	invBase := v.InvBase()
	switch {
	case slot < 0 || slot >= v.Slots():
		return slotAbsent, 0

	case slot >= invBase:
		if slot < invBase+player.MainInvSlots {
			return slotInv, player.MainSlot + int(slot-invBase)
		}
		if v.Kind == KindPlayer && slot == player.OffHandSlot {
			return slotInv, player.OffHandSlot
		}
		if v.Kind == KindCrafter && slot == invBase+player.MainInvSlots {
			// The crafter's result is the menu's last slot, after the
			// player's hotbar. vanilla: CrafterMenu.addSlots
			return slotResult, 0
		}
		return slotAbsent, 0
	}

	switch v.Kind {
	case KindPlayer:
		switch {
		case slot == playerCraftingResult:
			return slotResult, 0
		case slot < player.ArmorSlot+player.ArmorSlots:
			// Slots 1-4 are the 2x2 grid, 5-8 the armour; both live
			// in the inventory.
			return slotInv, int(slot)
		}
		return slotAbsent, 0

	case KindCrafting:
		if slot == craftingTableResult {
			return slotResult, 0
		}
		return slotGrid, int(slot - craftingTableGrid)

	case KindEnchant:
		return slotMenu, int(slot)

	case KindBeacon:
		return slotMenu, int(slot)

	case KindAnvil:
		if slot == AnvilResultSlot {
			return slotAnvilResult, 0
		}
		return slotMenu, int(slot)

	case KindStonecutter:
		if slot == StonecutterResultSlot {
			return slotStonecutterResult, 0
		}
		return slotMenu, int(slot)

	case KindSmithing:
		if slot == SmithingResultSlot {
			return slotSmithingResult, 0
		}
		return slotMenu, int(slot)

	case KindGrindstone:
		if slot == GrindstoneResultSlot {
			return slotGrindstoneResult, 0
		}
		return slotMenu, int(slot)

	case KindCartography:
		if slot == CartographyResultSlot {
			return slotCartographyResult, 0
		}
		return slotMenu, int(slot)

	case KindLoom:
		if slot == LoomResultSlot {
			return slotLoomResult, 0
		}
		return slotMenu, int(slot)

	case KindHorse:
		return slotEntity, int(slot)

	case KindMerchant:
		if slot == MerchantResultSlot {
			return slotMerchantResult, 0
		}
		return slotMenu, int(slot)

	case KindCrafter:
		// The crafter's own slots before the result are its 3x3 grid.
		// vanilla: CrafterMenu
		return slotGrid, int(slot)

	// Windows whose own slots are a block entity's. The ender chest opens
	// the player's own three rows, handed over as the entity.
	case KindChest, KindFurnace, KindBlastFurnace, KindSmoker, KindBrewingStand,
		KindBarrel, KindShulker, KindEnderChest, KindDispenser, KindDropper,
		KindHopper, KindDoubleChest, KindMenu:
		return slotEntity, int(slot)
	}
	return slotAbsent, 0
}

// isResult reports whether a window slot is the crafting result, which
// only ever gives.
func (v View) isResult(slot int32) bool {
	loc, _ := v.resolve(slot)
	return loc == slotResult
}

// isAnvilResult reports whether a window slot is the anvil's result.
func (v View) isAnvilResult(slot int32) bool {
	loc, _ := v.resolve(slot)
	return loc == slotAnvilResult
}

// isStonecutterResult reports whether a window slot is the stonecutter's
// result.
func (v View) isStonecutterResult(slot int32) bool {
	loc, _ := v.resolve(slot)
	return loc == slotStonecutterResult
}

// isSmithingResult reports whether a window slot is the smithing table's
// result.
func (v View) isSmithingResult(slot int32) bool {
	loc, _ := v.resolve(slot)
	return loc == slotSmithingResult
}

// isGrindstoneResult reports whether a window slot is the grindstone's
// result.
func (v View) isGrindstoneResult(slot int32) bool {
	loc, _ := v.resolve(slot)
	return loc == slotGrindstoneResult
}

// isCartographyResult reports whether a window slot is the cartography
// table's result.
func (v View) isCartographyResult(slot int32) bool {
	loc, _ := v.resolve(slot)
	return loc == slotCartographyResult
}

// isLoomResult reports whether a window slot is the loom's result.
func (v View) isLoomResult(slot int32) bool {
	loc, _ := v.resolve(slot)
	return loc == slotLoomResult
}

// isMerchantResult reports whether a window slot is the merchant window's
// result.
func (v View) isMerchantResult(slot int32) bool {
	loc, _ := v.resolve(slot)
	return loc == slotMerchantResult
}

// addresses reports whether the window has the slot. The client reports a
// click on the background as -1, which vanilla treats as nothing.
func (v View) addresses(slot int32) bool {
	loc, _ := v.resolve(slot)
	return loc != slotAbsent
}

// armorFits reports whether an armour slot accepts the stack. Only the
// equippable's slot and allowed entities are checked, not swappable, so a
// carved pumpkin can be dragged onto the head though right-clicking does not
// wear it.
// vanilla: ArmorSlot.mayPlace, LivingEntity.isEquippableInSlot
func armorFits(invSlot int, stack protocol.ItemStack) bool {
	item, ok := registry.StackEquippable(stack)
	if !ok || !item.AllowsEntity("minecraft:player") {
		return false
	}
	worn, ok := player.EquipmentSlotOf(item.Slot)
	return ok && worn == invSlot
}

// armorSlotFor is the armour slot a stack's equippable component names;
// false if not worn. Shift-click tries it first, by slot alone, which is how
// a shift-clicked shield reaches the off hand.
// vanilla: InventoryMenu.quickMoveStack
func armorSlotFor(stack protocol.ItemStack) (int32, bool) {
	item, ok := registry.StackEquippable(stack)
	if !ok {
		return 0, false
	}
	worn, ok := player.EquipmentSlotOf(item.Slot)
	return int32(worn), ok
}

// gridOf returns the window's crafting grid and width, false for none; the
// slice is the stored grid, so consumption writes through.
func (v View) gridOf(inv *player.Inventory) (cells []protocol.ItemStack, width int32, ok bool) {
	switch v.Kind {
	case KindPlayer:
		return inv.Slots[playerCraftingGrid : playerCraftingGrid+playerGridSlots], playerGridWidth, true
	case KindCrafting:
		return v.Grid, craftingGridWidth, true
	case KindCrafter:
		// The crafter's result is computed from its 3x3, which is the
		// block entity's own first nine slots.
		return v.Grid, craftingGridWidth, true
	}
	return nil, 0, false
}

// Stack returns what a window slot holds right now, under the caller's
// locks - the player's inventory lock and the entity's own.
func (v View) Stack(inv *player.Inventory, slot int32) protocol.ItemStack {
	loc, index := v.resolve(slot)
	switch loc {
	case slotInv:
		return inv.Slots[index]
	case slotGrid:
		return v.Grid[index]
	case slotMenu:
		return v.Menu[index]
	case slotEntity:
		if v.Entity != nil {
			return v.Entity.Slot(index)
		}
	case slotResult:
		return craftResultOf(v, inv)
	case slotAnvilResult:
		return v.Menu[AnvilResultSlot]
	case slotStonecutterResult:
		return v.Menu[StonecutterResultSlot]
	case slotSmithingResult:
		return v.Menu[SmithingResultSlot]
	case slotGrindstoneResult:
		return v.Menu[GrindstoneResultSlot]
	case slotCartographyResult:
		return v.Menu[CartographyResultSlot]
	case slotLoomResult:
		return v.Menu[LoomResultSlot]
	case slotMerchantResult:
		return v.Menu[MerchantResultSlot]
	case slotAbsent:
		// The lectern's book is shown and never clickable.
		if v.Kind == KindLectern && slot == 0 && len(v.Menu) > 0 {
			return v.Menu[0]
		}
	}
	return protocol.ItemStack{}
}

// placeable reports whether the stack may rest in the window slot (no stone
// in the helmet slot). The crafting result is written by crafting alone.
func (v View) placeable(slot int32, stack protocol.ItemStack) bool {
	if stack.Empty() {
		return true
	}
	switch v.Kind {
	case KindPlayer:
		loc, index := v.resolve(slot)
		if loc != slotInv {
			return false
		}
		if index >= player.ArmorSlot && index < player.ArmorSlot+player.ArmorSlots {
			return armorFits(index, stack)
		}
		return true
	case KindFurnace, KindBlastFurnace, KindSmoker:
		loc, index := v.resolve(slot)
		if loc != slotEntity {
			// The player's own things behind the furnace's three
			// take anything.
			return true
		}
		switch index {
		case 0: // the ingredient slot, which anything may smelt from
			return true
		case 1: // the fuel slot: what burns, and an empty bucket for a wet sponge to fill
			return IsCookingFuel(stack) || isEmptyBucket(stack)
		}
		return false // the output, which only ever gives
	case KindMenu:
		// A plugin menu's slots with no store behind them refuse items.
		// vanilla: Slot.mayPlace
		return !v.slotDisabled(slot)
	case KindCrafting:
		loc, _ := v.resolve(slot)
		return loc != slotResult
	case KindCrafter:
		// The crafter's 3x3 takes anything but a disabled slot; its result
		// is the one slot nothing rests in but what the grid makes.
		loc, _ := v.resolve(slot)
		return loc != slotResult && !v.slotDisabled(slot)
	case KindBrewingStand:
		loc, index := v.resolve(slot)
		if loc != slotEntity {
			// The player's own things behind the stand take anything.
			return true
		}
		switch index {
		case 0, 1, 2: // the bottle slots
			return isPotionBottle(stack)
		case 3: // the ingredient slot
			return isBrewingIngredient(registry.Item(stack.ItemID))
		case 4: // the fuel slot
			return IsBrewingFuel(stack)
		}
		return false
	case KindShulker:
		// Shulker boxes do not nest; the player's slots behind take anything.
		// vanilla: ShulkerBoxSlot.mayPlace
		loc, _ := v.resolve(slot)
		if loc != slotEntity {
			return true
		}
		return !isShulkerBoxItem(stack)
	case KindEnchant:
		loc, index := v.resolve(slot)
		if loc != slotMenu {
			return true
		}
		if index == 0 {
			// The item to enchant takes anything: the enchantable
			// component governs enchanting, not the slot's contents.
			return true
		}
		// The lapis, which takes only lapis.
		return isLapis(stack)
	case KindAnvil:
		loc, _ := v.resolve(slot)
		// The two inputs take whatever is put there; the result is the
		// one slot nothing rests in but what the anvil itself makes.
		return loc != slotAnvilResult
	case KindStonecutter:
		loc, _ := v.resolve(slot)
		// The input takes whatever is put in it; the result is the one
		// slot nothing rests in but what the selected recipe cuts.
		return loc != slotStonecutterResult
	case KindSmithing:
		loc, index := v.resolve(slot)
		if loc == slotSmithingResult {
			// The result gives and never takes.
			return false
		}
		if loc != slotMenu {
			// The player's own things behind the table take anything.
			return true
		}
		// Each input takes what the recipes' property sets name for it.
		// vanilla: SmithingMenu.createInputSlotDefinitions
		switch index {
		case 0:
			return registry.ItemInPropertySet(smithingTemplateSet, stack.ItemID)
		case 1:
			return registry.ItemInPropertySet(smithingBaseSet, stack.ItemID)
		case 2:
			return registry.ItemInPropertySet(smithingAdditionSet, stack.ItemID)
		}
		return false
	case KindGrindstone:
		loc, _ := v.resolve(slot)
		if loc == slotGrindstoneResult {
			// The result is the one slot nothing rests in but what the
			// inputs last made.
			return false
		}
		if loc != slotMenu {
			// The player's own things behind the grindstone take anything.
			return true
		}
		// The inputs take an item that wears or carries magic.
		// vanilla: GrindstoneMenu's input slots' mayPlace
		return grindable(stack)
	case KindCartography:
		loc, index := v.resolve(slot)
		if loc == slotCartographyResult {
			// The result is the one slot nothing rests in but what the
			// recipes make of the inputs.
			return false
		}
		if loc != slotMenu {
			// The player's own things behind the table take anything.
			return true
		}
		// The map slot takes a stack with a map id; the addition paper, an empty map or a
		// glass pane.
		// vanilla: CartographyTableMenu's slots' mayPlace
		if index == 0 {
			return isCartographyMap(stack)
		}
		return isCartographyAddition(stack)
	case KindLoom:
		loc, index := v.resolve(slot)
		if loc == slotLoomResult {
			return false
		}
		if loc != slotMenu {
			return true
		}
		// vanilla: LoomMenu's slots' mayPlace
		switch index {
		case 0:
			return isLoomBanner(stack)
		case 1:
			return isLoomDye(stack)
		}
		return isLoomPatternItem(stack)
	case KindHorse:
		// Saddle slot a saddle, armour slot a body piece the mount may wear;
		// chest slots and the player's things anything.
		// vanilla: ArmorSlot.mayPlace, LivingEntity.isEquippableInSlot
		if loc, index := v.resolve(slot); loc == slotEntity && index < HorseBaseSlots {
			fits, ok := v.Entity.(Fitter)
			return equippableSlot(stack) == HorseSlotNames[index] && (!ok || fits.Fits(index, stack))
		}
		return true

	case KindMerchant:
		// The payments take anything; the result gives and never takes.
		// vanilla: MerchantResultSlot.mayPlace
		loc, _ := v.resolve(slot)
		return loc != slotMerchantResult
	case KindBeacon:
		// The payment slot takes only what a beacon's effect is paid in;
		// the player's own things behind it take anything.
		loc, _ := v.resolve(slot)
		return loc != slotMenu || isBeaconPayment(stack)
	}
	return true
}

// slotLimit is how many the window slot holds: the item's maximum, except
// one in an armour slot, the enchanting item slot, a beacon payment, a
// brewing bottle slot and a bucket in furnace fuel.
// vanilla: ArmorSlot.getMaxStackSize, FurnaceFuelSlot.getMaxStackSize
func (v View) slotLimit(slot int32, stack protocol.ItemStack) int32 {
	switch v.Kind {
	case KindPlayer:
		// A worn piece is a single item. vanilla: ArmorSlot.getMaxStackSize
		if loc, index := v.resolve(slot); loc == slotInv &&
			index >= player.ArmorSlot && index < player.ArmorSlot+player.ArmorSlots {
			return 1
		}
	case KindEnchant:
		if loc, index := v.resolve(slot); loc == slotMenu && index == 0 {
			return 1
		}
	case KindHorse:
		// The saddle and the armour are one item each.
		// vanilla: ArmorSlot.getMaxStackSize
		if loc, index := v.resolve(slot); loc == slotEntity && index < HorseBaseSlots {
			return 1
		}
	case KindBeacon:
		// The payment slot holds a single item, the price of one choice.
		if loc, _ := v.resolve(slot); loc == slotMenu {
			return 1
		}
	case KindFurnace, KindBlastFurnace, KindSmoker:
		if loc, index := v.resolve(slot); loc == slotEntity && index == 1 && isEmptyBucket(stack) {
			return 1
		}
	case KindBrewingStand:
		// A bottle slot holds a single bottle.
		if loc, index := v.resolve(slot); loc == slotEntity && index <= 2 {
			return 1
		}
	}
	return stackMax(stack)
}

// slotDisabled reports whether the window slot is a crafter grid cell the
// block entity has disabled, which takes no item from a click either.
func (v View) slotDisabled(slot int32) bool {
	d, ok := v.Entity.(disabledContainer)
	if !ok {
		return false
	}
	loc, index := v.resolve(slot)
	return loc == slotEntity && d.SlotDisabled(index)
}

// disabledContainer is a Container whose slots may be individually disabled,
// the crafter's own.
type disabledContainer interface {
	SlotDisabled(slot int) bool
}

// isEmptyBucket reports whether the stack is an empty bucket.
func isEmptyBucket(stack protocol.ItemStack) bool {
	return registry.Item(stack.ItemID) == registry.ItemBucket
}

// isLapis reports whether the stack is lapis, the only thing the
// enchanting table's second slot takes.
func isLapis(stack protocol.ItemStack) bool {
	return registry.Item(stack.ItemID) == registry.ItemLapisLazuli
}

// equippableSlot is the slot the stack's equippable component names, empty
// for an item without one.
func equippableSlot(stack protocol.ItemStack) string {
	item, _ := registry.StackEquippable(stack)
	return item.Slot
}

// isBeaconPayment reports whether the stack is one of the items the
// beacon's payment slot takes.
func isBeaconPayment(stack protocol.ItemStack) bool {
	return registry.Item(stack.ItemID).InTag(registry.ItemTagIDBeaconPaymentItems)
}

// The recipe property sets the smithing table's three input slots take,
// by the names the join's Update Recipes packet sends them under.
const (
	smithingTemplateSet = "minecraft:smithing_template"
	smithingBaseSet     = "minecraft:smithing_base"
	smithingAdditionSet = "minecraft:smithing_addition"
)

// isShulkerBoxItem reports whether the stack is a shulker box of any colour
// (the items share their blocks' names).
func isShulkerBoxItem(stack protocol.ItemStack) bool {
	return registry.Item(stack.ItemID).IsShulkerBox()
}

// GridOf is the window's crafting grid and its width, for a window with one.
func (v View) GridOf(inv *player.Inventory) (cells []protocol.ItemStack, width int32, ok bool) {
	return v.gridOf(inv)
}
