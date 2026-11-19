package entity

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The minecart kinds this server simulates, by entity type and item name.
const (
	MinecartPlain  = "minecraft:minecart"
	MinecartChest  = "minecraft:chest_minecart"
	MinecartHopper = "minecraft:hopper_minecart"
	MinecartTNT    = "minecraft:tnt_minecart"

	MinecartFurnace = "minecraft:furnace_minecart"
	MinecartCommand = "minecraft:command_block_minecart"
	// MinecartSpawner has no item; it breaks into a plain minecart.
	MinecartSpawner = "minecraft:spawner_minecart"
)

// MinecartKind is what a cart kind adds to the shared ride: its English
// name, its container slots (0 for none), and its display block's default
// offset. Drop is the item a broken cart leaves where that is not its own.
// vanilla: AbstractMinecartContainer.getContainerSize, AbstractMinecart.getDefaultDisplayOffset
type MinecartKind struct {
	Name          string
	Slots         int
	DisplayOffset int32
	// Drop is the item a broken cart leaves, empty for the kind's own name.
	Drop string
}

var minecartKinds = [registry.EntityTypeCount]MinecartKind{
	registry.EntityTypeMinecart:       {Name: "Minecart", DisplayOffset: 6},
	registry.EntityTypeChestMinecart:  {Name: "Minecart with Chest", Slots: 27, DisplayOffset: 8},
	registry.EntityTypeHopperMinecart: {Name: "Minecart with Hopper", Slots: 5, DisplayOffset: 1},
	registry.EntityTypeTntMinecart:    {Name: "Minecart with TNT", DisplayOffset: 6},

	registry.EntityTypeFurnaceMinecart: {Name: "Minecart with Furnace", DisplayOffset: 6},
	// vanilla: MinecartCommandBlock.getDropItem
	registry.EntityTypeCommandBlockMinecart: {Name: "Minecart with Command Block", DisplayOffset: 6, Drop: MinecartPlain},
	registry.EntityTypeSpawnerMinecart:      {Name: "Minecart with Spawner", DisplayOffset: 6, Drop: MinecartPlain},
}

// MinecartKindOf is the kind the entity type names; ok is false for a type
// that is no cart this server simulates.
func MinecartKindOf(name string) (MinecartKind, bool) {
	typ, ok := registry.EntityTypeByName(name)
	if !ok {
		return MinecartKind{}, false
	}
	return MinecartKindForType(typ)
}

// MinecartKindForType is the cart data for a typed entity ID.
func MinecartKindForType(typ registry.EntityType) (MinecartKind, bool) {
	if uint32(typ) >= uint32(len(minecartKinds)) {
		return MinecartKind{}, false
	}
	k := minecartKinds[typ]
	return k, k.Name != ""
}

var minecartItems = map[registry.Item]string{
	registry.ItemMinecart:             MinecartPlain,
	registry.ItemChestMinecart:        MinecartChest,
	registry.ItemFurnaceMinecart:      MinecartFurnace,
	registry.ItemTntMinecart:          MinecartTNT,
	registry.ItemHopperMinecart:       MinecartHopper,
	registry.ItemCommandBlockMinecart: MinecartCommand,
}

// MinecartOfItem is the cart kind the item places; ok is false for any other item.
func MinecartOfItem(item registry.Item) (kind string, ok bool) {
	kind, ok = minecartItems[item]
	return kind, ok
}

// ContainerSlowdown is a container cart's rolling pace: 0.98 plus 0.001 per
// level its contents fall short of a full comparator signal, or 0.98 flat
// while its loot table is pending. Riders do not change it.
// vanilla: AbstractMinecartContainer.applyNaturalSlowdown
func ContainerSlowdown(slots []protocol.ItemStack, lootPending bool) float64 {
	keep := float32(0.98)
	if !lootPending {
		keep += float32(15-ContainerSignal(slots)) * float32(0.001)
	}
	return float64(keep)
}

// ContainerSignal is the comparator strength a container's fullness reads
// as, 0 to 15.
// vanilla: AbstractContainerMenu.getRedstoneSignalFromContainer, Mth.lerpDiscrete
func ContainerSignal(slots []protocol.ItemStack) int {
	if len(slots) == 0 {
		return 0
	}
	var total float32
	for _, stack := range slots {
		if !stack.Empty() {
			total += float32(stack.Count) / float32(registry.StackMaxStackSize(stack))
		}
	}
	total /= float32(len(slots))
	if total <= 0 {
		return 0
	}
	return int(total*14) + 1
}
