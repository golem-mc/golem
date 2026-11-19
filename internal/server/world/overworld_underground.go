package world

import (
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// placedDef is a placed feature's modifiers short of the biome filter, and the
// feature they feed.
type placedDef = struct {
	mods  []placementMod
	place func(*decorRegion, [3]int32)
}

// localDefs are the lakes' neighbours in the decoration steps: the monster
// rooms, the geodes, the forest rock.
func localDefs() map[string]placedDef {
	const top = worldMinY + worldHeight - 1
	room := func(count, lo, hi int32) placedDef {
		return placedDef{[]placementMod{countMod(count), inSquareMod, heightUniformMod(lo, hi)},
			(*decorRegion).monsterRoomAt}
	}
	return map[string]placedDef{
		"minecraft:monster_room":      room(10, 0, top),
		"minecraft:monster_room_deep": room(4, worldMinY+6, -1),
		"minecraft:amethyst_geode": {[]placementMod{
			rarityMod(24), inSquareMod, heightUniformMod(worldMinY+6, 30),
		}, func(r *decorRegion, p [3]int32) { r.placeGeode(p) }},
		"minecraft:blue_ice": {[]placementMod{
			countUniformMod(0, 19), inSquareMod, heightUniformMod(30, 61),
		}, (*decorRegion).placeBlueIce},
		"minecraft:forest_rock": {[]placementMod{
			countMod(2), inSquareMod, heightmapMotionBlockingMod,
		}, (*decorRegion).placeForestRock},
	}
}

func (r *decorRegion) monsterRoomAt(p [3]int32) { r.placeMonsterRoom(p) }

// placeForestRock is BlockBlobFeature with mossy cobblestone, on the blocks
// the tag forest_rock_can_place_on lists.
// vanilla: BlockBlobFeature.place
func (r *decorRegion) placeForestRock(p [3]int32) {
	x, y, z := p[0], p[1], p[2]
	for y > r.minY+3 && !stateInTag(r.get(x, y-1, z), registry.TagIDForestRockCanPlaceOn) {
		y--
	}
	if y <= r.minY+3 {
		return
	}
	mossy := registry.BlockMossyCobblestone.DefaultState()
	for range 3 {
		xr, yr, zr := r.rng.NextIntN(2), r.rng.NextIntN(2), r.rng.NextIntN(2)
		tr := float32(float32(xr+yr+zr)*0.333) + 0.5
		for dx := -xr; dx <= xr; dx++ {
			for dy := -yr; dy <= yr; dy++ {
				for dz := -zr; dz <= zr; dz++ {
					if float64(dx*dx+dy*dy+dz*dz) <= float64(tr*tr) {
						r.set(x+dx, y+dy, z+dz, mossy)
					}
				}
			}
		}
		x += -1 + r.rng.NextIntN(2)
		y -= r.rng.NextIntN(2)
		z += -1 + r.rng.NextIntN(2)
	}
}

// placeBlueIce is BlueIceFeature: a blue ice core grown through the water and
// packed ice under the sea.
// vanilla: BlueIceFeature.place
func (r *decorRegion) placeBlueIce(p [3]int32) {
	if p[1] > levelgen.OverworldSeaLevel-1 {
		return
	}
	water, packed, ice, blue := registry.BlockWater, registry.BlockPackedIce, registry.BlockIce, registry.BlockBlueIce
	is := func(x, y, z int32, b registry.Block) bool { return registry.BlockOf(r.get(x, y, z)) == b }
	x, y, z := p[0], p[1], p[2]
	if !is(x, y, z, water) && !is(x, y-1, z, water) {
		return
	}
	// The six neighbours in Direction.values() order, DOWN skipped for the
	// packed ice test only.
	dirs := [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}
	touches := func(x, y, z int32, b registry.Block, skipDown bool) bool {
		for i, d := range dirs {
			if (i > 0 || !skipDown) && is(x+d[0], y+d[1], z+d[2], b) {
				return true
			}
		}
		return false
	}
	if !touches(x, y, z, packed, true) {
		return
	}
	blueState := blue.DefaultState()
	r.set(x, y, z, blueState)
	for range 200 {
		yOff := r.rng.NextIntN(5) - r.rng.NextIntN(6)
		xz := int32(3)
		if yOff < 2 {
			xz += yOff / 2
		}
		if xz < 1 {
			continue
		}
		px := x + r.rng.NextIntN(xz) - r.rng.NextIntN(xz)
		py := y + yOff
		pz := z + r.rng.NextIntN(xz) - r.rng.NextIntN(xz)
		if !r.isAir(px, py, pz) && !is(px, py, pz, water) && !is(px, py, pz, packed) && !is(px, py, pz, ice) {
			continue
		}
		if touches(px, py, pz, blue, false) {
			r.set(px, py, pz, blueState)
		}
	}
}
