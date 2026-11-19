package rules

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// The beacon's decisions: pyramid level, a clear beam, and which choices a
// level allows. Applying effects to players is the server's.

// NoBeaconEffect is the effect ID of a side no choice was made for; -1
// because zero is speed's own ID.
const NoBeaconEffect = -1

// BeaconValidateEffects reports whether a beacon of levels may hold the
// choices: a secondary needs level 4 or to equal the primary, nothing above
// what the levels offer, and never regeneration as primary. An unset pair
// passes, clearing the choices.
// vanilla: BeaconBlockEntity.validateEffects
func BeaconValidateEffects(primary, secondary int32, levels int) bool {
	if secondary != NoBeaconEffect && levels < 4 {
		return false
	}
	required := registry.BeaconEffectLevels(primary)
	if required > levels {
		return false
	}
	secondRequired := registry.BeaconEffectLevels(secondary)
	if secondRequired > levels {
		return false
	}
	if required >= 4 {
		return false
	}
	return secondRequired == 0 || secondRequired >= 4 || primary == secondary
}

// BeaconPyramidLevel counts unbroken base layers under a beacon at (x, y,
// z): 3x3, 5x5, 7x7, 9x9; 0 for a gap in the first.
// vanilla: BeaconBlockEntity.updateBase
func BeaconPyramidLevel(w BlockReader, x, y, z, minY int32) int {
	levels := 0
	for step := int32(1); step <= 4; step++ {
		layerY := y - step
		if layerY < minY {
			break
		}
		whole := true
	squares:
		for lx := x - step; lx <= x+step; lx++ {
			for lz := z - step; lz <= z+step; lz++ {
				if !registry.BlockOf(w.BlockAt(lx, layerY, lz)).InTag(registry.TagIDBeaconBaseBlocks) {
					whole = false
					break squares
				}
			}
		}
		if !whole {
			break
		}
		levels = int(step)
	}
	return levels
}

// BeaconBeamClear reports whether the beam reaches top: a fully
// light-occluding block stops it, except bedrock; glass dampens nothing.
// Vanilla scans ten blocks a tick, this the whole column; the answer at the
// reading update is the same.
// vanilla: BeaconBlockEntity.tick, its beam sections
func BeaconBeamClear(w BlockReader, x, y, z, top int32) bool {
	for ly := y + 1; ly <= top; ly++ {
		state := w.BlockAt(x, ly, z)
		if registry.LightDampening(state) < 15 {
			continue
		}
		if registry.BlockOf(state) == registry.BlockBedrock {
			continue
		}
		return false
	}
	return true
}

// BeaconRange is how far a beacon of the given level reaches, the blocks
// around its own a cycle applies its effects in.
// vanilla: BeaconBlockEntity.applyEffects (levels * 10 + 10)
func BeaconRange(levels int) float64 {
	return float64(levels*10 + 10)
}

// BeaconEffectDuration is how long one cycle's application of a beacon's
// effects lasts, in ticks: eleven seconds at one level, two more per level
// above it. vanilla: BeaconBlockEntity.applyEffects ((9 + levels * 2) * 20)
func BeaconEffectDuration(levels int) int32 {
	return int32((9 + levels*2) * 20)
}

// BeaconEffectAmplifier is the tier the primary applies at: the second only
// when a fourth level feeds it with its own choice as well.
// vanilla: BeaconBlockEntity.applyEffects
func BeaconEffectAmplifier(levels int, primary, secondary int32) int32 {
	if levels >= 4 && primary == secondary {
		return 1
	}
	return 0
}

// BeaconInReach reports whether a player box (0.6 by 1.8) with feet at (px,
// py, pz) meets the beacon's block inflated by reach, extended to any height
// above.
// vanilla: BeaconBlockEntity.applyEffects
// (AABB(worldPos).inflate(range).expandTowards(0, height, 0))
func BeaconInReach(reach, px, py, pz float64, bx, by, bz int32) bool {
	minX, maxX := float64(bx)-reach, float64(bx)+1+reach
	minZ, maxZ := float64(bz)-reach, float64(bz)+1+reach
	return px+0.3 > minX && px-0.3 < maxX &&
		py+1.8 > float64(by)-reach &&
		pz+0.3 > minZ && pz-0.3 < maxZ
}

// BeaconUpdateTicks is the interval the beacon counts its pyramid and
// applies its choices on, in ticks of the game time every dimension
// shares. vanilla: BeaconBlockEntity.tick (gameTime % 80)
const BeaconUpdateTicks = 80

// BeaconEncodeEffect is the effect ID the window's property carries: the
// mob effect's own ID plus one, zero for the none an unset side is.
// vanilla: BeaconMenu.encodeEffect
func BeaconEncodeEffect(effect int32) int32 {
	if effect < 0 {
		return 0
	}
	return effect + 1
}

// BeaconProps is what the window draws itself with, in the property form
// the wire wants: the level counted and the two choices.
func BeaconProps(levels int, primary, secondary int32) [protocol.BeaconPropertyCount]int32 {
	return [protocol.BeaconPropertyCount]int32{
		protocol.BeaconPropertyLevels:    int32(levels),
		protocol.BeaconPropertyPrimary:   BeaconEncodeEffect(primary),
		protocol.BeaconPropertySecondary: BeaconEncodeEffect(secondary),
	}
}

// BeaconAppliesSecondary reports whether a cycle applies the secondary as
// an effect of its own: a fourth level, a choice made, and not the primary's
// own effect again. vanilla: BeaconBlockEntity.applyEffects
func BeaconAppliesSecondary(levels int, primary, secondary int32) bool {
	return secondary >= 0 && levels >= 4 && secondary != primary
}
