package inventory

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// IsCookingFuel reports whether a stack fuels a furnace; it gates the fuel
// slot and shift-click routing.
// vanilla: AbstractFurnaceMenu.isFuel
func IsCookingFuel(stack protocol.ItemStack) bool { return registry.HasCookingFuel(stack) }

// recipeKind is the block entity kind whose recipe table a cooking window
// cooks from, which is what the registry's matcher keys the table on.
func recipeKind(kind Kind) string {
	switch kind {
	case KindBlastFurnace:
		return "minecraft:blast_furnace"
	case KindSmoker:
		return "minecraft:smoker"
	}
	return "minecraft:furnace"
}
