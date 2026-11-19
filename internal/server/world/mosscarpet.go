package world

import (
	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/registry"
)

// PaleMossCarpet is the block name MossyCarpetState shapes.
const PaleMossCarpet = "minecraft:pale_moss_carpet"

// MossyCarpetState is the updated carpet at (x, y, z): a side is low where the
// neighbour's face can hold it, tall where the carpet above (a topper without a
// base) has that side too. A topper loses sides its base lacks. createSides
// adds missing sides; a base carpet always does.
// vanilla: MossyCarpetBlock.getUpdatedState
func MossyCarpetState(w BlockReader, state uint16, x, y, z int32, createSides bool) uint16 {
	if registry.BlockOf(state) != registry.BlockPaleMossCarpet {
		return state
	}
	base := registry.StateBool(state, registry.PropBottom)
	createSides = createSides || base
	above := w.BlockAt(x, y+1, z)
	below := w.BlockAt(x, y-1, z)
	for i, face := range bonemealHorizontal {
		prop := facePropIDs[face]
		side := "none"
		neighbour := w.BlockAt(x+horizontal4[i][0], y, z+horizontal4[i][1])
		if registry.FaceAttachable(neighbour, blocklogic.Opposite(int(face))) {
			side = "low"
			if !createSides {
				side = registry.StateValue(state, prop)
			}
		}
		if side == "low" {
			if registry.BlockOf(above) == registry.BlockPaleMossCarpet && registry.StateValue(above, prop) != "none" && !registry.StateBool(above, registry.PropBottom) {
				side = "tall"
			}
			if !base && registry.BlockOf(below) == registry.BlockPaleMossCarpet && registry.StateValue(below, prop) == "none" {
				side = "none"
			}
		}
		state, _ = registry.WithProp(state, prop, side)
	}
	return state
}

// MossyCarpetTopper is the carpet to put above the one at (x, y, z), if any;
// sideSurvives is asked once per side it would keep.
// vanilla: MossyCarpetBlock.createTopperWithSideChance
func MossyCarpetTopper(w BlockReader, x, y, z int32, sideSurvives func() bool) (uint16, bool) {
	above := w.BlockAt(x, y+1, z)
	isCarpet := registry.BlockOf(above) == registry.BlockPaleMossCarpet
	if int(above) >= registry.StateCount || (isCarpet && registry.StateBool(above, registry.PropBottom)) ||
		(!isCarpet && !registry.BlockOf(above).InTag(registry.TagIDReplaceable)) {
		return 0, false
	}
	noBase, _ := registry.WithBool(registry.BlockPaleMossCarpet.DefaultState(), registry.PropBottom, false)
	topper := MossyCarpetState(w, noBase, x, y+1, z, true)
	for _, prop := range [...]registry.Prop{registry.PropNorth, registry.PropEast, registry.PropSouth, registry.PropWest} {
		if registry.StateValue(topper, prop) == "none" || sideSurvives() {
			continue
		}
		topper, _ = registry.WithProp(topper, prop, "none")
	}
	if !mossCarpetHasFaces(topper) || topper == w.BlockAt(x, y+1, z) {
		return 0, false
	}
	return topper, true
}
