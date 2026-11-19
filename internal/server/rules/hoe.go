package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// hoeTiller is one HoeItem.TILLABLES entry: the result, whether it needs
// onlyIfAirAbove, and the loot table it rolls.
type hoeTiller struct {
	into     registry.Block
	airAbove bool
	loot     string
}

// hoeTillerOf is HoeItem.TILLABLES. Only rooted dirt (predicate
// ctx -> true) tills under a block or from below.
func hoeTillerOf(b registry.Block) (hoeTiller, bool) {
	switch b {
	case registry.BlockGrassBlock, registry.BlockDirtPath, registry.BlockDirt:
		return hoeTiller{into: registry.BlockFarmland, airAbove: true}, true
	case registry.BlockCoarseDirt:
		return hoeTiller{into: registry.BlockDirt, airAbove: true}, true
	case registry.BlockRootedDirt:
		return hoeTiller{into: registry.BlockDirt, loot: "minecraft:till/rooted_dirt"}, true
	}
	return hoeTiller{}, false
}

// HoeTill is what a hoe's click on the face of the block at (x, y, z)
// tills it into, and the loot table dropped from that face (empty for none).
// ok is false for a block the hoe leaves alone.
// vanilla: HoeItem.useOn
func HoeTill(w BlockReader, x, y, z, face int32) (into uint16, loot string, ok bool) {
	if face < 0 || int(face) >= len(protocol.FaceOffset) {
		return 0, "", false
	}
	state := w.BlockAt(x, y, z)
	if int(state) >= registry.StateCount {
		return 0, "", false
	}
	till, known := hoeTillerOf(registry.BlockOf(state))
	if !known {
		return 0, "", false
	}
	if till.airAbove {
		// vanilla: HoeItem.onlyIfAirAbove, which also refuses a click from below.
		if protocol.FaceOffset[face][1] == -1 {
			return 0, "", false
		}
		if above := w.BlockAt(x, y+1, z); !world.IsKnownAirState(above) {
			return 0, "", false
		}
	}
	return till.into.DefaultState(), till.loot, true
}
