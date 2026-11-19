package world

import (
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// placeDesertWell is DesertWellFeature.place.
func (r *decorRegion) placeDesertWell(p [3]int32) bool {
	p[1]++
	for r.isAir(p[0], p[1], p[2]) && p[1] > r.minY+2 {
		p[1]--
	}
	if registry.BlockOf(r.get(p[0], p[1], p[2])) != registry.BlockSand {
		return false
	}
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			if r.isAir(p[0]+x, p[1]-1, p[2]+z) && r.isAir(p[0]+x, p[1]-2, p[2]+z) {
				return false
			}
		}
	}
	sandstone := registry.BlockSandstone.DefaultState()
	slab := registry.BlockSandstoneSlab.DefaultState()
	set := func(x, y, z int32, state uint16) { r.set(p[0]+x, p[1]+y, p[2]+z, state) }
	for y := int32(-2); y <= 0; y++ {
		for x := int32(-2); x <= 2; x++ {
			for z := int32(-2); z <= 2; z++ {
				set(x, y, z, sandstone)
			}
		}
	}
	set(0, 0, 0, registry.BlockWater.DefaultState())
	set(0, -1, 0, registry.BlockSand.DefaultState())
	for _, d := range monsterRoomDirs {
		set(d.dx, 0, d.dz, registry.BlockWater.DefaultState())
		set(d.dx, -1, d.dz, registry.BlockSand.DefaultState())
	}
	for x := int32(-2); x <= 2; x++ {
		for z := int32(-2); z <= 2; z++ {
			if x == -2 || x == 2 || z == -2 || z == 2 {
				set(x, 1, z, sandstone)
			}
		}
	}
	for _, d := range monsterRoomDirs {
		set(d.dx*2, 1, d.dz*2, slab)
	}
	for x := int32(-1); x <= 1; x++ {
		for z := int32(-1); z <= 1; z++ {
			state := slab
			if x == 0 && z == 0 {
				state = sandstone
			}
			set(x, 4, z, state)
		}
	}
	for y := int32(1); y <= 3; y++ {
		for _, x := range [...]int32{-1, 1} {
			for _, z := range [...]int32{-1, 1} {
				set(x, y, z, sandstone)
			}
		}
	}
	// vanilla: DesertWellFeature.placeSusSand; the list's order fixes the draws.
	water := [...][2]int32{{0, 0}, {1, 0}, {0, 1}, {-1, 0}, {0, -1}}
	for depth := int32(1); depth <= 2; depth++ {
		d := water[r.rng.NextIntN(int32(len(water)))]
		x, y, z := p[0]+d[0], p[1]-depth, p[2]+d[1]
		state := registry.BlockSuspiciousSand.DefaultState()
		r.set(x, y, z, state)
		// vanilla: BlockPos.asLong, not the feature's random stream.
		seed := int64(x&0x3FFFFFF)<<38 | int64(z&0x3FFFFFF)<<12 | int64(y&0xFFF)
		r.placeBlockEntity(x, y, z, state, nbt.Compound{
			"id": nbt.String(KindBrushable.Name()), "LootTable": nbt.String("minecraft:archaeology/desert_well"),
			"LootTableSeed": nbt.Long(seed),
		})
	}
	return true
}
