package rules

import (
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// openStats is OpenStat's table, built once from the per-block keys.
var openStats = func() *[registry.BlockCount]registry.CustomStat {
	t := new([registry.BlockCount]registry.CustomStat)
	for b := range t {
		t[b] = registry.NoCustomStat
	}
	for b, key := range map[registry.Block]registry.CustomStat{
		registry.BlockTrappedChest: registry.CustomStatTriggerTrappedChest,
		registry.BlockBarrel:       registry.CustomStatOpenBarrel,
		registry.BlockEnderChest:   registry.CustomStatOpenEnderchest,
		registry.BlockDispenser:    registry.CustomStatInspectDispenser,
		registry.BlockDropper:      registry.CustomStatInspectDropper,
		registry.BlockHopper:       registry.CustomStatInspectHopper,
		registry.BlockBrewingStand: registry.CustomStatInteractWithBrewingstand,

		registry.BlockCraftingTable:    registry.CustomStatInteractWithCraftingTable,
		registry.BlockFurnace:          registry.CustomStatInteractWithFurnace,
		registry.BlockBlastFurnace:     registry.CustomStatInteractWithBlastFurnace,
		registry.BlockSmoker:           registry.CustomStatInteractWithSmoker,
		registry.BlockStonecutter:      registry.CustomStatInteractWithStonecutter,
		registry.BlockSmithingTable:    registry.CustomStatInteractWithSmithingTable,
		registry.BlockGrindstone:       registry.CustomStatInteractWithGrindstone,
		registry.BlockLoom:             registry.CustomStatInteractWithLoom,
		registry.BlockCartographyTable: registry.CustomStatInteractWithCartographyTable,
		registry.BlockBeacon:           registry.CustomStatInteractWithBeacon,
		registry.BlockLectern:          registry.CustomStatInteractWithLectern,
	} {
		t[b] = key
	}
	for _, b := range []registry.Block{registry.BlockAnvil, registry.BlockChippedAnvil, registry.BlockDamagedAnvil} {
		t[b] = registry.CustomStatInteractWithAnvil
	}
	return t
}()

// OpenStat is the minecraft:custom_stat key a player's use of the container
// block b counts, or registry.NoCustomStat for a block that counts none.
// vanilla: ChestBlock.getOpenChestStat, BarrelBlock.useWithoutItem,
// ShulkerBoxBlock.useWithoutItem, EnderChestBlock.useWithoutItem,
// DispenserBlock.openContainer, HopperBlock.useWithoutItem, and the
// INTERACT_WITH_* awards of the crafting, cooking and workstation blocks
func OpenStat(b registry.Block) registry.CustomStat {
	switch {
	case openStats[b] != registry.NoCustomStat:
		return openStats[b]
	case world.IsChestBlock(b):
		return registry.CustomStatOpenChest
	case b.IsShulkerBox():
		return registry.CustomStatOpenShulkerBox
	}
	return registry.NoCustomStat
}
