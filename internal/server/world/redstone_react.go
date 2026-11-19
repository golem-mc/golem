package world

import "github.com/golem-mc/golem/registry"

// reactKind classifies a block by its neighbour-changed reaction, so dispatch
// is a table lookup rather than a chain of name-set hashes.
type reactKind uint8

const (
	reactNone reactKind = iota
	reactWire
	reactTorch
	reactRail
	reactRepeater
	reactComparator
	reactLamp
	reactTNT
	reactNoteBlock
	reactSkull
	reactBigDripleaf
	reactPlateOrButton
	reactDoorLike
	reactBell
	reactCopperBulb
	reactShelf
	reactDispenser
	reactCrafter
	reactCommandBlock
	reactStructureBlock
	reactHopper
	reactPiston
	reactPistonHead
	reactTestBlock
)

// reactKinds is indexed by Block, built at init after the block sets it reads.
var reactKinds [registry.BlockCount]reactKind

func buildReactKinds() {
	for b := range registry.Block(registry.BlockCount) {
		reactKinds[b] = reactKindOfBlock(b)
	}
}

// reactKindOfBlock matches the kinds in order; a block in two sets takes the
// first.
func reactKindOfBlock(b registry.Block) reactKind {
	switch {
	case isWire[b]:
		return reactWire
	case isTorch[b]:
		return reactTorch
	case railBlocks[b]:
		return reactRail
	case b == registry.BlockRepeater:
		return reactRepeater
	case b == registry.BlockComparator:
		return reactComparator
	case isLamp[b]:
		return reactLamp
	case isTntBlock[b]:
		return reactTNT
	case b == registry.BlockNoteBlock:
		return reactNoteBlock
	case b.IsSkull():
		return reactSkull
	case b == registry.BlockBigDripleaf:
		return reactBigDripleaf
	case isPressurePlate[b], isButton[b]:
		return reactPlateOrButton
	case isDoorLike[b]:
		return reactDoorLike
	case isBell[b]:
		return reactBell
	case isCopperBulb(b):
		return reactCopperBulb
	case shelfBlocks[b]:
		return reactShelf
	case isDispenser[b], isDropper[b]:
		return reactDispenser
	case b == registry.BlockCrafter:
		return reactCrafter
	case isCommandBlock[b]:
		return reactCommandBlock
	case b == registry.BlockStructureBlock:
		return reactStructureBlock
	case isHopper[b]:
		return reactHopper
	case pistonBlocks[b]:
		return reactPiston
	case b == registry.BlockPistonHead:
		return reactPistonHead
	case b == registry.BlockTestBlock:
		return reactTestBlock
	}
	return reactNone
}

// reactKindOf returns a state's reaction kind; no state reads as reactNone.
func reactKindOf(state uint16) reactKind {
	return reactKinds[registry.BlockOf(state)]
}
