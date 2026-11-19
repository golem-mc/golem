package plugin

// InventoryOpenEvent fires before a player opens an inventory. Bukkit:
// InventoryOpenEvent
type InventoryOpenEvent struct {
	Cancellable
	Player Player
	View   InventoryView
}

// InventoryCloseEvent fires when a player closes an inventory view.
// Bukkit: InventoryCloseEvent
type InventoryCloseEvent struct {
	Base
	Player Player
	View   InventoryView
}

// InventoryClickEvent fires for every click in an open view, before vanilla
// applies it; cancelling resends the view's slots and the cursor, so a
// menu's items stay put. Bukkit: InventoryClickEvent
type InventoryClickEvent struct {
	Cancellable
	Player Player
	View   InventoryView
	// RawSlot numbers the view's slots, the top inventory's first; -999 is
	// outside the window. Slot is the slot within Inventory: for a click on
	// the player's own armour it is 36 (feet) to 39 (head), and -1 for the
	// off hand and crafting slots.
	RawSlot   int
	Slot      int
	Inventory Inventory
	Click     ClickType
	// Button is the number key for ClickNumberKey.
	Button int
	// Current is the clicked slot's stack, Cursor the cursor's; both may
	// be changed.
	Current, Cursor ItemStack
}

// ClickType is the kind of a click in a window.
type ClickType uint8

const (
	ClickLeft ClickType = iota
	ClickRight
	ClickShiftLeft
	ClickShiftRight
	ClickMiddle
	ClickNumberKey
	ClickDouble
	ClickDrop
	ClickControlDrop
	ClickSwapOffhand
	ClickCreative
	ClickWindowBorderLeft
	ClickWindowBorderRight
)

// InventoryDragEvent fires when a player ends a drag across several slots,
// before it writes; cancelling resends the window. Slots maps each raw slot to
// the stack it would receive, and Cursor what stays held; a change to either
// is written instead, for the marked slots only.
// Bukkit: InventoryDragEvent
type InventoryDragEvent struct {
	Cancellable
	Player Player
	View   InventoryView
	Slots  map[int]ItemStack
	Cursor ItemStack
	Right  bool
}

// InventoryMoveItemEvent fires when a hopper block pushes or pulls an item
// between itself and another block container: often, so a listener must be
// cheap. Item may be changed, and the hopper then moves one of the changed
// item. Droppers, hopper minecarts and vehicle or composter targets do not
// fire it. Cancelling costs the hopper its cooldown. Bukkit:
// InventoryMoveItemEvent
type InventoryMoveItemEvent struct {
	Cancellable
	Source, Destination Inventory
	Item                ItemStack
}

// InventoryPickupItemEvent fires when a hopper block picks up an item entity.
// Bukkit: InventoryPickupItemEvent
type InventoryPickupItemEvent struct {
	Cancellable
	Inventory Inventory
	Item      ItemEntity
}

// PrepareItemCraftEvent fires when a click or drag changes a crafting grid's
// (the player's 2x2 or a table's) or an anvil's inputs, before the result is
// sent; Result may be changed. A grid's result holds for that grid's
// contents, so a shift-click crafting several shows the listener's result for
// the first craft only. Bukkit: PrepareItemCraftEvent, PrepareAnvilEvent
type PrepareItemCraftEvent struct {
	Base
	Player Player
	View   InventoryView
	// Recipe is the matched recipe; nil for an anvil, grindstone or no
	// match.
	Recipe Recipe
	Result ItemStack
	// Cost is the anvil's level cost, and may be changed.
	Cost int
}

// CraftItemEvent fires when a player takes a crafting, smithing, anvil,
// grindstone, stonecutter or loom result with a click, before it is taken;
// cancelling resends the window. Recipe is set for a crafting grid. Bukkit:
// CraftItemEvent
type CraftItemEvent struct {
	Cancellable
	Player Player
	View   InventoryView
	Recipe Recipe
	Result ItemStack
	// Shift is a shift-click crafting as many as fit.
	Shift bool
}

// PrepareItemEnchantEvent fires when an item goes on an enchanting table;
// Offers may be changed. Bukkit: PrepareItemEnchantEvent
type PrepareItemEnchantEvent struct {
	Cancellable
	Player    Player
	Block     Block
	Item      ItemStack
	Bookshelf int
	Offers    [3]EnchantOffer
}

// EnchantOffer is one of an enchanting table's three offers.
type EnchantOffer struct {
	Enchantment Enchantment
	Level, Cost int
}

// EnchantItemEvent fires when a player enchants an item; the enchantments
// and cost may be changed. Bukkit: EnchantItemEvent
type EnchantItemEvent struct {
	Cancellable
	Player       Player
	Block        Block
	Item         ItemStack
	Enchantments map[Enchantment]int
	Cost         int
	Button       int
}

// FurnaceExtractEvent fires when a player takes a furnace's output;
// Experience may be changed. Bukkit: FurnaceExtractEvent
type FurnaceExtractEvent struct {
	Base
	Player     Player
	Block      Block
	Item       ItemStack
	Experience int
}

// LootGenerateEvent fires when a loot table fills a container (block or
// vehicle), before the stacks are split and placed; Items may be changed and
// cancelling leaves the container empty. It runs under the container's lock,
// so Entity and Holder stay unset and a listener must not touch that
// container. Bukkit: LootGenerateEvent
type LootGenerateEvent struct {
	Cancellable
	Table  Key
	World  World
	Pos    Vec3
	Entity Entity // the looter or the dying entity
	Holder any    // the Container or Entity filled
	Items  []ItemStack
}
