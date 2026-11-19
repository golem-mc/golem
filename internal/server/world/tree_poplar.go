package world

//go:generate go run ../../../tools/gen poplar-golden

import (
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// poplarTrunk is vanilla: PoplarTrunkPlacer.
type poplarTrunk struct {
	trunkBase
	aboveBranches, branches intProvider
}

func (p poplarTrunk) placeTrunk(t *treeBuild, height int32, origin treePos) []foliageAttach {
	p.placeBelow(t, at(origin, treePos{0, -1, 0}))
	branchHeight := height - p.aboveBranches(t.rng)
	for y := range height {
		p.placeLog(t, at(origin, treePos{0, y, 0}), nil)
		// Vanilla shuffles all six directions at every log, even without a branch.
		dirs := [...]treePos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
		shuffle(dirs[:], t.rng)
		if y != branchHeight-1 {
			continue
		}
		remaining := p.branches(t.rng)
		for _, dir := range dirs {
			if dir[1] != 0 {
				continue
			}
			axis := "z"
			if dir[0] != 0 {
				axis = "x"
			}
			p.placeLog(t, at(origin, treePos{dir[0], y, dir[2]}), withAxis(axis))
			remaining--
			if remaining == 0 {
				break
			}
		}
	}
	return []foliageAttach{{pos: at(origin, treePos{0, branchHeight, 0})}}
}

// poplarFoliage is vanilla: PoplarFoliagePlacer.
type poplarFoliage struct {
	radius, offset, height intProvider
	sideHoleChance         float32
}

func (p poplarFoliage) foliageHeight(rng *levelgen.WorldgenRandom, _ int32) int32 {
	return p.height(rng)
}
func (p poplarFoliage) foliageRadius(rng *levelgen.WorldgenRandom, _ int32) int32 {
	return p.radius(rng)
}

func (p poplarFoliage) createFoliage(t *treeBuild, _ int32, a foliageAttach, height, radius int32) {
	origin := at(a.pos, treePos{0, p.offset(t.rng), 0})
	radius += a.radiusOffset - 1
	flip := t.rng.NextBoolean()
	height += a.heightOffset
	p.row(t, origin, radius-2, height-1, a.doubleTrunk, height, flip)
	p.row(t, origin, radius-1, height-2, a.doubleTrunk, height, flip)
	p.row(t, origin, radius-1, height-3, a.doubleTrunk, height, flip)
	for y := height - 4; y >= 1; y-- {
		p.row(t, origin, radius, y, a.doubleTrunk, height, flip)
	}
	p.replaceLeavesWithLog(t, origin, radius, height-4, a.doubleTrunk, height, flip)
	p.row(t, origin, radius-1, 0, a.doubleTrunk, height, flip)
	p.row(t, origin, min(max(radius-2, 1), 2), -1, a.doubleTrunk, height, flip)
}

func poplarPartial(height, y int32) bool { return y == height-1 || y == height-2 }

// poplarCorner is vanilla: PoplarFoliagePlacer.getCornerBlocksToCutForRhombusShape.
func poplarCorner(dx, dz, radius int32, partial, flip bool) int32 {
	small := dx > 0 && dz < 0 || dz > 0 && dx < 0
	if flip {
		small = dx > 0 && dz > 0 || dx < 0 && dz < 0
	}
	if small {
		return radius - 1
	}
	if partial {
		return radius + 1
	}
	return radius
}

func (p poplarFoliage) row(t *treeBuild, origin treePos, radius, y int32, double bool, height int32, flip bool) {
	extra := int32(0)
	if double {
		extra = 1
	}
	partial := poplarPartial(height, y)
	for dx := -radius; dx <= radius+extra; dx++ {
		for dz := -radius; dz <= radius+extra; dz++ {
			ax, az := abs32(dx), abs32(dz)
			if partial && (ax == radius || az == radius) {
				continue
			}
			removal := int32(0)
			if t.rng.NextFloat() <= p.sideHoleChance {
				removal = 1
			}
			if ax+az > radius*2-poplarCorner(dx, dz, radius, partial, flip)-removal {
				continue
			}
			t.tryPlaceLeaf(at(origin, treePos{dx, y, dz}))
		}
	}
}

// replaceLeavesWithLog counts its logs as foliage, so leaf-distance updates and
// decorators receive the same position sets as vanilla's FoliageSetter.
// vanilla: PoplarFoliagePlacer.replaceLeavesWithLog, tryPlaceLog
func (p poplarFoliage) replaceLeavesWithLog(t *treeBuild, origin treePos, radius, y int32, double bool, height int32, flip bool) {
	extra := int32(0)
	if double {
		extra = 1
	}
	for dx := -radius; dx <= radius+extra; dx++ {
		for dz := -radius; dz <= radius+extra; dz++ {
			ax, az := abs32(dx), abs32(dz)
			if ax+az > radius*2-poplarCorner(dx, dz, radius, poplarPartial(height, y), flip)-2 {
				continue
			}
			if (az != 0 || radius-ax < 4) && (ax != 0 || radius-az < 4) {
				continue
			}
			pos := at(origin, treePos{dx, y, dz})
			// getState is evaluated even when the existing block is not a leaf.
			if t.state(pos) != t.cfg.foliageProvider(t.r, pos) {
				continue
			}
			axis := "z"
			if az == 0 {
				axis = "x"
			}
			state := t.cfg.trunkProvider(t.r, pos)
			state, _ = registry.WithProp(state, registry.PropAxis, axis)
			t.put(&t.foliage, pos, state)
		}
	}
}
