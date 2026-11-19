package world

import (
	"sync"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

const monsterRoomLootTable = "minecraft:chests/simple_dungeon"

// monsterRoomMobs is vanilla's MOBS array, the zombie listed twice.
var monsterRoomMobs = [...]string{"minecraft:skeleton", "minecraft:zombie", "minecraft:zombie", "minecraft:spider"}

type monsterRoomStates struct {
	caveAir, cobblestone, mossy, spawner uint16
	chestBlock, spawnerBlock             registry.Block
	chest                                [4]uint16 // by monsterRoomDirs index
}

// monsterRoomDirs are Direction.Plane.HORIZONTAL in order, so index+1 is
// clockwise and index+2 opposite.
var monsterRoomDirs = [4]struct {
	dx, dz int32
	facing string
}{{0, -1, "north"}, {1, 0, "east"}, {0, 1, "south"}, {-1, 0, "west"}}

var monsterRoomBlocks = sync.OnceValue(func() *monsterRoomStates {
	find := func(name string, props map[string]string) uint16 {
		state, ok := registry.FindState(name, props)
		if !ok {
			panic("the block registry has no " + name)
		}
		return state
	}
	s := &monsterRoomStates{
		caveAir:     find("minecraft:cave_air", nil),
		cobblestone: find("minecraft:cobblestone", nil),
		mossy:       find("minecraft:mossy_cobblestone", nil),
		spawner:     find("minecraft:spawner", nil),
	}
	s.chestBlock = registry.BlockOf(find("minecraft:chest", nil))
	s.spawnerBlock = registry.BlockOf(s.spawner)
	for i, d := range monsterRoomDirs {
		s.chest[i] = find("minecraft:chest", map[string]string{"facing": d.facing})
	}
	return s
})

// placeMonsterRoom builds one room at the origin, or none where the ground
// does not fit it.
// vanilla: MonsterRoomFeature.place
func (r *decorRegion) placeMonsterRoom(p [3]int32) bool {
	st := monsterRoomBlocks()
	rng := r.rng
	ox, oy, oz := p[0], p[1], p[2]
	xr := rng.NextIntN(2) + 2
	zr := rng.NextIntN(2) + 2
	minX, maxX, minZ, maxZ := -xr-1, xr+1, -zr-1, zr+1
	holes := 0
	for dx := minX; dx <= maxX; dx++ {
		for dy := int32(-1); dy <= 4; dy++ {
			for dz := minZ; dz <= maxZ; dz++ {
				x, y, z := ox+dx, oy+dy, oz+dz
				if (dy == -1 || dy == 4) && !registry.IsSolid(r.get(x, y, z)) {
					return false
				}
				if (dx == minX || dx == maxX || dz == minZ || dz == maxZ) && dy == 0 &&
					r.isAir(x, y, z) && r.isAir(x, y+1, z) {
					holes++
				}
			}
		}
	}
	if holes < 1 || holes > 5 {
		return false
	}

	// safeSetBlock: the blocks features cannot replace stay.
	set := func(x, y, z int32, state uint16) {
		if !stateInTag(r.get(x, y, z), registry.TagIDFeaturesCannotReplace) {
			r.set(x, y, z, state)
		}
	}
	for dx := minX; dx <= maxX; dx++ {
		for dy := int32(3); dy >= -1; dy-- {
			for dz := minZ; dz <= maxZ; dz++ {
				x, y, z := ox+dx, oy+dy, oz+dz
				here := r.get(x, y, z)
				if dx == minX || dy == -1 || dz == minZ || dx == maxX || dy == 4 || dz == maxZ {
					// Read live and top-down: a wall block over a hole in the
					// ground is left open.
					if y >= r.minY && !registry.IsSolid(r.get(x, y-1, z)) {
						r.set(x, y, z, st.caveAir)
						continue
					}
					if !registry.IsSolid(here) || registry.BlockOf(here) == st.chestBlock {
						continue
					}
					if dy == -1 && rng.NextIntN(4) != 0 {
						set(x, y, z, st.mossy)
					} else {
						set(x, y, z, st.cobblestone)
					}
					continue
				}
				if b := registry.BlockOf(here); b == st.chestBlock || b == st.spawnerBlock {
					continue
				}
				set(x, y, z, st.caveAir)
			}
		}
	}

	for range 2 {
		for range 3 {
			x := ox + rng.NextIntN(xr*2+1) - xr
			z := oz + rng.NextIntN(zr*2+1) - zr
			if !r.isAir(x, oy, z) {
				continue
			}
			walls := 0
			for _, d := range monsterRoomDirs {
				if registry.IsSolid(r.get(x+d.dx, oy, z+d.dz)) {
					walls++
				}
			}
			if walls != 1 {
				continue
			}
			set(x, oy, z, st.chest[r.chestFacing(x, oy, z)])
			if registry.BlockOf(r.get(x, oy, z)) == st.chestBlock {
				r.placeBlockEntity(x, oy, z, r.get(x, oy, z), nbt.Compound{
					"id": nbt.String(KindChest.Name()), "LootTable": nbt.String(monsterRoomLootTable),
				})
			}
			break
		}
	}

	set(ox, oy, oz, st.spawner)
	if registry.BlockOf(r.get(ox, oy, oz)) == st.spawnerBlock {
		mob := monsterRoomMobs[rng.NextIntN(int32(len(monsterRoomMobs)))]
		r.placeBlockEntity(ox, oy, oz, st.spawner, nbt.Compound{
			"id":        nbt.String(KindSpawner.Name()),
			"SpawnData": nbt.Compound{"entity": nbt.Compound{"id": nbt.String(mob)}},
		})
	}
	return true
}

// chestFacing is the monsterRoomDirs index a chest faces: away from its one
// solid-rendering neighbour, else the first free side.
// vanilla: StructurePiece.reorient
func (r *decorRegion) chestFacing(x, y, z int32) int {
	chest := monsterRoomBlocks().chestBlock
	solidRender := func(i int) bool {
		d := monsterRoomDirs[i]
		return registry.IsSolidRender(r.get(x+d.dx, y, z+d.dz))
	}
	wall := -1
	for i, d := range monsterRoomDirs {
		if registry.BlockOf(r.get(x+d.dx, y, z+d.dz)) == chest {
			return 0
		}
		if !solidRender(i) {
			continue
		}
		if wall >= 0 {
			wall = -1
			break
		}
		wall = i
	}
	if wall >= 0 {
		return (wall + 2) % 4
	}
	lock := 0
	if solidRender(lock) {
		lock = (lock + 2) % 4
	}
	if solidRender(lock) {
		lock = (lock + 1) % 4
	}
	if solidRender(lock) {
		lock = (lock + 2) % 4
	}
	return lock
}
