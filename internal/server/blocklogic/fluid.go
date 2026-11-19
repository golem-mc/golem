package blocklogic

import "github.com/golem-mc/golem/registry"

// FluidKind identifies water, lava or no fluid.
type FluidKind uint8

const (
	FluidNone FluidKind = iota
	FluidWater
	FluidLava
)

// FluidState holds a legacy level (0 source, 1-7 flowing, 8-15 falling).
// Pure distinguishes liquid blocks from water held by plants or containers.
type FluidState struct {
	Kind  FluidKind
	Level uint8
	Pure  bool
}

// IsSource reports a still fluid source, including contained water.
func (f FluidState) IsSource() bool { return f.Kind != FluidNone && f.Level == 0 }

// IsFullWater reports water with eight eighths of depth, including falling water.
func (f FluidState) IsFullWater() bool {
	return f.Kind == FluidWater && FluidAmount(int(f.Level)) == 8
}

// FluidOf reads the fluid held by a block state, including contained water.
func FluidOf(state uint16) FluidState {
	p := fluidStates[state]
	return FluidState{Kind: FluidKind(p >> 4 & 3), Level: p & 15, Pure: p&64 != 0}
}

// One byte per state keeps entity contact and fluid spread reads allocation-free.
var fluidStates = func() *[1 << 16]uint8 {
	t := new([1 << 16]uint8)
	for id := range t {
		s := uint16(id)
		b := registry.BlockOf(s)
		switch b {
		case registry.BlockWater, registry.BlockLava:
			kind := FluidWater
			if b == registry.BlockLava {
				kind = FluidLava
			}
			level, _ := registry.StateInt(s, registry.PropLevel)
			if level < 0 || level > 15 {
				panic("fluid level does not fit a nibble")
			}
			t[id] = uint8(kind)<<4 | uint8(level) | 64
		default:
			if registry.FluidStates[s] || IsWaterPlant(b) {
				t[id] = uint8(FluidWater) << 4
			}
		}
	}
	return t
}()

// FluidAmount is a fluid's depth in eighths; falling columns are full.
func FluidAmount(level int) int {
	if level > 0 && level < 8 {
		return 8 - level
	}
	return 8
}

// IsWaterPlant reports blocks that always hold a water source.
// vanilla: KelpBlock.getFluidState, SeagrassBlock.getFluidState, BubbleColumnBlock.getFluidState
func IsWaterPlant(b registry.Block) bool {
	switch b {
	case registry.BlockKelp, registry.BlockKelpPlant, registry.BlockSeagrass,
		registry.BlockTallSeagrass, registry.BlockBubbleColumn:
		return true
	}
	return false
}

// PlaceWater returns a container's waterlogged state and placement sound.
// An already waterlogged container has no placement effects.
// vanilla: SimpleWaterloggedBlock.placeLiquid, CampfireBlock.placeLiquid, CandleBlock.placeLiquid
func PlaceWater(state uint16) (next uint16, sound registry.Sound, ok bool) {
	next, ok = registry.WithBool(state, registry.PropWaterlogged, true)
	if !ok || registry.StateBool(state, registry.PropWaterlogged) {
		return next, registry.SoundNone, ok
	}
	b := registry.BlockOf(state)
	switch {
	case b.InTag(registry.TagIDCampfires), b.InTag(registry.TagIDCandles):
		next, _ = registry.WithBool(next, registry.PropLit, false)
		if registry.StateLit(state) {
			sound = registry.SoundEntityGenericExtinguishFire
			if b.InTag(registry.TagIDCandles) {
				sound = registry.SoundBlockCandleExtinguish
			}
		}
	case b == registry.BlockDriedGhast:
		sound = registry.SoundBlockDriedGhastPlaceInWater
	}
	return next, sound, true
}
