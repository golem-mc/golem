package rules

import "github.com/golem-mc/golem/registry"

// BeeAttracts reports whether a bee pollinates the flower state: a block of
// the bee_attractive tag that holds no water, and of a sunflower only the
// upper half.
// vanilla: Bee.attractsBees
func BeeAttracts(state uint16) bool {
	b := registry.BlockOf(state)
	if !b.InTag(registry.TagIDBeeAttractive) {
		return false
	}
	if registry.StateBool(state, registry.PropWaterlogged) {
		return false
	}
	if b == registry.BlockSunflower {
		return registry.StateValue(state, registry.PropHalf) == "upper"
	}
	return true
}

// BeeGrownCrop is the state a block of the bee_growables tag grows into, false
// at its last age or for a block a bee does not grow: a CropBlock, stem or
// sweet berry bush gains one age (a torchflower crop past age 1 becomes a
// torchflower), cave vines grow their berries as bone meal does, and the
// pitcher crop, a DoublePlantBlock, is left alone.
// vanilla: Bee.BeeGrowCropGoal.tick, TorchflowerCropBlock.getStateForAge
func BeeGrownCrop(state uint16) (uint16, bool) {
	block := registry.BlockOf(state)
	if !block.InTag(registry.TagIDBeeGrowables) {
		return 0, false
	}
	switch block {
	case registry.BlockCaveVines, registry.BlockCaveVinesPlant:
		if registry.StateBool(state, registry.PropBerries) {
			return 0, false
		}
		return registry.WithBool(state, registry.PropBerries, true)
	case registry.BlockTorchflowerCrop:
		// Its age property stops at 1; getStateForAge(2) is the flower.
		if age, _ := registry.StateInt(state, registry.PropAge); age == 1 {
			return registry.BlockTorchflower.DefaultState(), true
		}
	case registry.BlockWheat, registry.BlockCarrots, registry.BlockPotatoes, registry.BlockBeetroots,
		registry.BlockMelonStem, registry.BlockPumpkinStem, registry.BlockSweetBerryBush:
	default:
		return 0, false
	}
	// Each block's last age is its age property's last value, so WithInt
	// fails exactly where vanilla's isMaxAge / age < 7 / age < 3 stops.
	age, ok := registry.StateInt(state, registry.PropAge)
	if !ok {
		return 0, false
	}
	return registry.WithInt(state, registry.PropAge, age+1)
}
