package world

import (
	"github.com/golem-mc/golem/registry"
)

// signalKind classifies a state by its signal rule, so hot reads switch on a
// table lookup rather than hashing names.
type signalKind uint8

const (
	kindOther signalKind = iota
	kindWire
	kindTorch
	kindRepeater
	kindObserver
	kindComparator
	kindRedstoneBlock
	kindPressurePlate
	kindButton
	kindLever
	kindDaylightDetector
	kindTripHook
	kindTarget
	kindDetectorRail
	kindLectern
	kindTrappedChest
	kindJukebox
	kindSculkSensor
	kindLightningRod
	kindTestBlock
)

// signalKinds is indexed by state ID, built once from the block sets in
// redstone.go.
var signalKinds []signalKind

func buildSignalKinds() {
	for id := uint16(0); ; id++ {
		name, _, ok := registry.BlockStateName(id)
		if !ok {
			break
		}
		signalKinds = append(signalKinds, signalKindOfName(name))
	}
}

func signalKindOfName(name string) signalKind {
	b, _ := registry.BlockByName(name)
	switch {
	case isWire[b]:
		return kindWire
	case isTorch[b]:
		return kindTorch
	case b == registry.BlockRepeater:
		return kindRepeater
	case isObserver[b]:
		return kindObserver
	case b == registry.BlockComparator:
		return kindComparator
	case isRedstoneBlk[b]:
		return kindRedstoneBlock
	case isPressurePlate[b]:
		return kindPressurePlate
	case isButton[b]:
		return kindButton
	case b == registry.BlockLever:
		return kindLever
	case isDaylightDetector[b]:
		return kindDaylightDetector
	case isTripHook[b]:
		return kindTripHook
	case b == registry.BlockTarget:
		return kindTarget
	case b == registry.BlockDetectorRail:
		return kindDetectorRail
	case b == registry.BlockLectern:
		return kindLectern
	case b == registry.BlockTrappedChest:
		return kindTrappedChest
	case b == registry.BlockJukebox:
		return kindJukebox
	case isSculkSensor(b):
		return kindSculkSensor
	case b.InGroup(registry.GroupLightningRod):
		return kindLightningRod
	case b == registry.BlockTestBlock:
		return kindTestBlock
	}
	return kindOther
}

// signalKindOf returns a state's kind; no state reads as kindOther, like
// blockAt's air.
func signalKindOf(state uint16) signalKind {
	if int(state) >= len(signalKinds) {
		return kindOther
	}
	return signalKinds[state]
}
