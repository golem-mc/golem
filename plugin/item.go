package plugin

import "github.com/golem-mc/golem/registry"

// ItemStack is a stack of items: a value, copied when passed. The zero
// value is the empty stack. Bukkit: ItemStack and ItemMeta together
type ItemStack struct {
	Item  registry.Item
	Count int
	// Patch is what the stack changes from the item's default components.
	Patch ItemPatch
}

// NewStack answers count of item with no changes.
func NewStack(item registry.Item, count int) ItemStack { return ItemStack{Item: item, Count: count} }

// Empty reports whether the stack holds nothing.
func (s ItemStack) Empty() bool { return s.Count <= 0 || s.Item == registry.ItemAir }

// ItemPatch is a stack's data component patch, with the components plugins
// use most as fields; nil and empty fields leave the default. Components
// with no field here are read and written as SNBT in Other, the way /give
// writes them.
type ItemPatch struct {
	CustomName, ItemName *Text
	Lore                 []Text
	// Damage is the durability used; MaxDamage and MaxStackSize override
	// the item's.
	Damage, MaxDamage, MaxStackSize *int
	Unbreakable                     *bool
	Enchantments                    map[Enchantment]int
	// StoredEnchantments are an enchanted book's.
	StoredEnchantments map[Enchantment]int
	// GlintOverride forces the enchantment glint on or off.
	GlintOverride *bool
	// Model and CustomModelData pick a resource pack model.
	Model           *Key
	CustomModelData *CustomModelData
	Rarity          *registry.Rarity
	HideTooltip     *bool
	// DyedColor is leather armour's RGB colour.
	DyedColor *int32
	// Profile is a player head's owner.
	Profile *Profile
	// Data is the plugins' data, kept in minecraft:custom_data.
	Data map[Key]any
	// Other holds the remaining components as SNBT by component key; an
	// empty string removes the component (!key in /give).
	Other map[Key]string
}

// CustomModelData is the minecraft:custom_model_data component.
type CustomModelData struct {
	Floats  []float32
	Flags   []bool
	Strings []string
	Colors  []int32
}

// Items creates and reads item stacks with the server's item rules.
// Bukkit: ItemFactory
type Items interface {
	// Parse reads a stack in /give's syntax:
	// "diamond_sword[enchantments={sharpness:5}]".
	Parse(s string, count int) (ItemStack, error)
	// Format writes s in /give's syntax.
	Format(s ItemStack) string
	// Check reports a component in Other that does not parse.
	Check(s ItemStack) error
	// MaxStackSize answers the stack size s can reach.
	MaxStackSize(s ItemStack) int
	// Similar reports whether a and b stack: the same item and components.
	Similar(a, b ItemStack) bool
	// Name answers the name the client shows for s.
	Name(s ItemStack) Text
	// Enchant answers s enchanted as an enchanting table at level would
	// (with treasure enchantments if treasure).
	Enchant(s ItemStack, level int, treasure bool) ItemStack
}

// InventoryType is the kind of an inventory, which decides its size and
// window. Bukkit: InventoryType
type InventoryType uint8

const (
	InvChest InventoryType = iota // 9 to 54 slots
	InvDispenser
	InvDropper
	InvHopper
	InvFurnace
	InvBlastFurnace
	InvSmoker
	InvBrewing
	InvBeacon
	InvAnvil
	InvGrindstone
	InvSmithing
	InvStonecutter
	InvLoom
	InvCartography
	InvEnchanting
	InvCrafting // a crafting table
	InvCrafter
	InvShulkerBox
	InvBarrel
	InvEnderChest
	InvMerchant
	InvPlayer
	InvLectern
)

// Inventory is a set of slots. Bukkit: Inventory
type Inventory interface {
	Type() InventoryType
	Size() int
	Item(slot int) ItemStack
	// SetItem changes a slot and sends the change to every viewer.
	SetItem(slot int, s ItemStack)
	Contents() []ItemStack
	SetContents([]ItemStack)
	// Add puts the stacks in, merging first, and answers what did not fit.
	Add(stacks ...ItemStack) []ItemStack
	// Remove takes out the stacks' counts and answers what was missing.
	Remove(stacks ...ItemStack) []ItemStack
	// Count answers how many of item the inventory holds.
	Count(item registry.Item) int
	// First answers the first slot holding a stack similar to s, -1 for none.
	First(s ItemStack) int
	FirstEmpty() int
	Clear()
	Viewers() []Player
	// Holder answers what holds the inventory: a Container, an Entity, or
	// the plugin's own value given to CreateInventory.
	Holder() any
}

// PlayerInventory is a player's inventory in Bukkit's numbering: 36 storage
// slots with the hotbar at 0 to 8, then boots, leggings, chestplate and
// helmet at 36 to 39 and the off hand at 40; Equipment reaches the same
// slots by name. Add and FirstEmpty use the storage slots only.
type PlayerInventory interface {
	Inventory
	Equipment
	// HeldSlot is the hotbar slot selected, 0 to 8.
	HeldSlot() int
	SetHeldSlot(int)
}

// InventoryView is an open window: the top inventory and the player's own
// below it. Bukkit: InventoryView
type InventoryView interface {
	Player() Player
	Top() Inventory
	Bottom() PlayerInventory
	Title() Text
	// SetProperty sets a window property: furnace progress, enchanting
	// costs (the Set Container Property packet).
	SetProperty(id, value int)
	Close()
}

// Recipes is the server's recipe list. A recipe a plugin adds is sent to
// clients' recipe books and crafts like a data pack's. Bukkit: Server's
// recipe methods
type Recipes interface {
	Get(key Key) (Recipe, bool)
	// Add adds a recipe; its Key must be in the plugin's namespace.
	Add(r Recipe) error
	Remove(key Key) bool
	All() []Recipe
}

// Recipe is one of the recipe types below.
type Recipe interface{ RecipeKey() Key }

// Ingredient is the items a recipe slot accepts.
type Ingredient struct {
	Items []registry.Item
	// Tag is an item tag accepted instead; zero for none.
	Tag Key
}

// ShapedRecipe is a crafting recipe with a pattern.
type ShapedRecipe struct {
	Key Key
	// Pattern is up to three rows of up to three characters, keyed by Keys;
	// a space is an empty slot.
	Pattern []string
	Keys    map[rune]Ingredient
	Result  ItemStack
	Group   string
}

// ShapelessRecipe is a crafting recipe with no pattern.
type ShapelessRecipe struct {
	Key         Key
	Ingredients []Ingredient
	Result      ItemStack
	Group       string
}

// CookingRecipe is a furnace, blast furnace, smoker or campfire recipe.
type CookingRecipe struct {
	Key Key
	// Kind is minecraft:smelting, minecraft:blasting, minecraft:smoking or
	// minecraft:campfire_cooking.
	Kind       Key
	Input      Ingredient
	Result     ItemStack
	Experience float32
	CookTime   int
}

// StonecuttingRecipe is a stonecutter recipe.
type StonecuttingRecipe struct {
	Key    Key
	Input  Ingredient
	Result ItemStack
}

// SmithingRecipe is a smithing table transform.
type SmithingRecipe struct {
	Key                      Key
	Template, Base, Addition Ingredient
	Result                   ItemStack
}

func (r ShapedRecipe) RecipeKey() Key       { return r.Key }
func (r ShapelessRecipe) RecipeKey() Key    { return r.Key }
func (r CookingRecipe) RecipeKey() Key      { return r.Key }
func (r StonecuttingRecipe) RecipeKey() Key { return r.Key }
func (r SmithingRecipe) RecipeKey() Key     { return r.Key }

// Data is the plugins' data kept with an entity, a block entity, a chunk or
// a world, saved with it under each value's Key. Values are bool, int32,
// int64, float64, string, []byte, []int32, []int64 or a nested
// map[string]any of those. Bukkit: PersistentDataContainer
type Data interface {
	Get(key Key) (any, bool)
	Set(key Key, v any)
	Remove(key Key)
	Keys() []Key
}

// DataValue is a type Data holds.
type DataValue interface {
	bool | int32 | int64 | float64 | string | []byte | []int32 | []int64 | map[string]any
}

// GetData answers the value at key when it holds a T.
func GetData[T DataValue](d Data, key Key) (T, bool) {
	v, ok := d.Get(key)
	t, isT := v.(T)
	return t, ok && isT
}
