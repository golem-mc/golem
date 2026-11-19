package world

import (
	"slices"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

type desertPyramidStructure struct{}

func init() { overworldStructureTypes["minecraft:desert_pyramid"] = desertPyramidStructure{} }

// vanilla: DesertPyramidStructure.findGenerationPoint, SinglePieceStructure
func (desertPyramidStructure) generationPoint(ctx *structureContext) ([3]int32, func() []structurePiece, bool) {
	x, z := ctx.chunkX*16, ctx.chunkZ*16
	if min(ctx.g.firstFreeHeight(x, z), ctx.g.firstFreeHeight(x+21, z), ctx.g.firstFreeHeight(x, z+21), ctx.g.firstFreeHeight(x+21, z+21))-1 < ctx.g.SeaLevel() {
		return [3]int32{}, nil, false
	}
	return [3]int32{x + 8, ctx.g.firstFreeHeight(x+8, z+8) - 1, z + 8}, func() []structurePiece {
		return []structurePiece{newDesertPyramid(ctx.rng, x, z)}
	}, true
}

// afterPlace turns a deterministic subset of the saved sand candidates into
// archaeology blocks. vanilla: DesertPyramidStructure.afterPlace
func (desertPyramidStructure) afterPlace(r *decorRegion, chunk jbox, s *structureStart) {
	var candidates [][3]int32
	for _, piece := range s.pieces {
		p, ok := piece.(*desertPyramidPiece)
		if !ok {
			continue
		}
		collapsed := p.collapsedRoofPos()
		if collapsed != ([3]int32{}) {
			desertSuspiciousSand(r, chunk, collapsed)
		}
		candidates = append(candidates, p.potentialSand()...)
	}
	slices.SortFunc(candidates, func(a, b [3]int32) int {
		if a[1] != b[1] {
			return int(a[1] - b[1])
		}
		if a[2] != b[2] {
			return int(a[2] - b[2])
		}
		return int(a[0] - b[0])
	})
	candidates = slices.Compact(candidates)
	if len(candidates) == 0 {
		return
	}
	b := s.pieces[0].bounds()
	cx := int32(b.minX + (b.maxX-b.minX+1)/2)
	cy := int32(b.minY + (b.maxY-b.minY+1)/2)
	cz := int32(b.minZ + (b.maxZ-b.minZ+1)/2)
	rng := levelgen.NewLegacyRandom(r.worldSeed).ForkPositional().At(cx, cy, cz)
	for i := len(candidates); i > 1; i-- {
		j := int(rng.NextIntN(int32(i)))
		candidates[i-1], candidates[j] = candidates[j], candidates[i-1]
	}
	count := int(rng.NextIntN(3)) + 5
	for _, p := range candidates {
		if count > 0 {
			count--
			desertSuspiciousSand(r, chunk, p)
			continue
		}
		if chunk.inside(p[0], p[1], p[2]) {
			r.set(p[0], p[1], p[2], registry.BlockSand.DefaultState())
			r.markShape(p[0], p[1], p[2])
		}
	}
}

func desertSuspiciousSand(r *decorRegion, chunk jbox, p [3]int32) {
	if !chunk.inside(p[0], p[1], p[2]) {
		return
	}
	state := registry.BlockSuspiciousSand.DefaultState()
	r.set(p[0], p[1], p[2], state)
	seed := int64(p[0]&0x3FFFFFF)<<38 | int64(p[2]&0x3FFFFFF)<<12 | int64(p[1]&0xFFF)
	r.placeBlockEntity(p[0], p[1], p[2], state, nbt.Compound{"id": nbt.String(KindBrushable.Name()),
		"LootTable": nbt.String("minecraft:archaeology/desert_pyramid"), "LootTableSeed": nbt.Long(seed)})
}

// vanilla: DesertPyramidPiece
type desertPyramidPiece struct {
	scatteredPiece
	chests    [4]bool
	sand      [][3]int32
	collapsed [3]int32
}

func newDesertPyramid(rng levelgen.RandomSource, x, z int32) *desertPyramidPiece {
	return &desertPyramidPiece{box: jbox{x, 64, z, x + 20, 78, z + 20}, dir: int(rng.NextIntN(4)), heightPosition: -1}
}

func (p *desertPyramidPiece) nbt() nbt.Compound {
	p.mu.Lock()
	defer p.mu.Unlock()
	tag := p.header("minecraft:tedp", 21, 15, 21)
	tag["hasPlacedChest0"] = nbtBool(p.chests[0])
	tag["hasPlacedChest1"] = nbtBool(p.chests[1])
	tag["hasPlacedChest2"] = nbtBool(p.chests[2])
	tag["hasPlacedChest3"] = nbtBool(p.chests[3])
	return tag
}

func (p *desertPyramidPiece) potentialSand() [][3]int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.sand)
}

func (p *desertPyramidPiece) collapsedRoofPos() [3]int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.collapsed
}

// vanilla: DesertPyramidPiece.postProcess
func (p *desertPyramidPiece) place(r *decorRegion, chunk jbox, _ [3]int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.lowestGround(r, r.rng.NextIntN(3)) {
		return
	}
	pl := &strongholdPlacer{r: r, st: shStatesOnce(), frame: levelgen.PieceFrame{BB: toBoundingBox(p.box), Dir: p.dir}, dir: p.dir, chunk: chunk, rng: r.rng}
	same := func(x0, y0, z0, x1, y1, z1 int32, state uint16) {
		pl.solid(x0, y0, z0, x1, y1, z1, state)
	}
	box := func(x0, y0, z0, x1, y1, z1 int32, edge, fill uint16) {
		pl.box(x0, y0, z0, x1, y1, z1, edge, fill)
	}
	sandstone := registry.BlockSandstone.DefaultState()
	air := uint16(airState)
	same(0, -4, 0, 20, 0, 20, sandstone)
	for pos := int32(1); pos <= 9; pos++ {
		same(pos, pos, pos, 20-pos, pos, 20-pos, sandstone)
		same(pos+1, pos, pos+1, 19-pos, pos, 19-pos, air)
	}
	for x := range int32(21) {
		for z := range int32(21) {
			pl.fillColumnDown(sandstone, x, -5, z)
		}
	}
	northStairs, _ := registry.WithProp(registry.BlockSandstoneStairs.DefaultState(), registry.PropFacing, fenceDirNames[shN])
	southStairs, _ := registry.WithProp(registry.BlockSandstoneStairs.DefaultState(), registry.PropFacing, fenceDirNames[shS])
	eastStairs, _ := registry.WithProp(registry.BlockSandstoneStairs.DefaultState(), registry.PropFacing, fenceDirNames[shE])
	westStairs, _ := registry.WithProp(registry.BlockSandstoneStairs.DefaultState(), registry.PropFacing, fenceDirNames[shW])
	northStairs = pl.localState(northStairs)
	southStairs = pl.localState(southStairs)
	eastStairs = pl.localState(eastStairs)
	westStairs = pl.localState(westStairs)
	box(0, 0, 0, 4, 9, 4, sandstone, air)
	same(1, 10, 1, 3, 10, 3, sandstone)
	pl.put(2, 10, 0, northStairs)
	pl.put(2, 10, 4, southStairs)
	pl.put(0, 10, 2, eastStairs)
	pl.put(4, 10, 2, westStairs)
	box(16, 0, 0, 20, 9, 4, sandstone, air)
	same(17, 10, 1, 19, 10, 3, sandstone)
	pl.put(18, 10, 0, northStairs)
	pl.put(18, 10, 4, southStairs)
	pl.put(16, 10, 2, eastStairs)
	pl.put(20, 10, 2, westStairs)
	box(8, 0, 0, 12, 4, 4, sandstone, air)
	same(9, 1, 0, 11, 3, 4, air)
	cut := registry.BlockCutSandstone.DefaultState()
	chiseled := registry.BlockChiseledSandstone.DefaultState()
	for i := int32(9); i <= 11; i += 2 {
		for y := int32(1); y <= 3; y++ {
			pl.put(i, y, 1, cut)
		}
	}
	pl.put(10, 3, 1, cut)
	box(4, 1, 1, 8, 3, 3, sandstone, air)
	same(4, 1, 2, 8, 2, 2, air)
	box(12, 1, 1, 16, 3, 3, sandstone, air)
	same(12, 1, 2, 16, 2, 2, air)
	same(5, 4, 5, 15, 4, 15, sandstone)
	same(9, 4, 9, 11, 4, 11, air)
	same(8, 1, 8, 8, 3, 8, cut)
	same(12, 1, 8, 12, 3, 8, cut)
	same(8, 1, 12, 8, 3, 12, cut)
	same(12, 1, 12, 12, 3, 12, cut)
	same(1, 1, 5, 4, 4, 11, sandstone)
	same(16, 1, 5, 19, 4, 11, sandstone)
	same(6, 7, 9, 6, 7, 11, sandstone)
	same(14, 7, 9, 14, 7, 11, sandstone)
	same(5, 5, 9, 5, 7, 11, cut)
	same(15, 5, 9, 15, 7, 11, cut)
	pl.put(5, 5, 10, air)
	pl.put(5, 6, 10, air)
	pl.put(6, 6, 10, air)
	pl.put(15, 5, 10, air)
	pl.put(15, 6, 10, air)
	pl.put(14, 6, 10, air)
	same(2, 4, 4, 2, 6, 4, air)
	same(18, 4, 4, 18, 6, 4, air)
	pl.put(2, 4, 5, northStairs)
	pl.put(2, 3, 4, northStairs)
	pl.put(18, 4, 5, northStairs)
	pl.put(18, 3, 4, northStairs)
	same(1, 1, 3, 2, 2, 3, sandstone)
	same(18, 1, 3, 19, 2, 3, sandstone)
	pl.put(1, 1, 2, sandstone)
	pl.put(19, 1, 2, sandstone)
	pl.put(1, 2, 2, registry.BlockSandstoneSlab.DefaultState())
	pl.put(19, 2, 2, registry.BlockSandstoneSlab.DefaultState())
	pl.put(2, 1, 2, westStairs)
	pl.put(18, 1, 2, eastStairs)
	same(4, 3, 5, 4, 3, 17, sandstone)
	same(16, 3, 5, 16, 3, 17, sandstone)
	same(3, 1, 5, 4, 2, 16, air)
	same(15, 1, 5, 16, 2, 16, air)
	for z := int32(5); z <= 17; z += 2 {
		pl.put(4, 1, z, cut)
		pl.put(4, 2, z, chiseled)
		pl.put(16, 1, z, cut)
		pl.put(16, 2, z, chiseled)
	}
	orange := registry.BlockOrangeTerracotta.DefaultState()
	blue := registry.BlockBlueTerracotta.DefaultState()
	for _, q := range [][3]int32{
		{10, 0, 7}, {10, 0, 8}, {9, 0, 9}, {11, 0, 9}, {8, 0, 10},
		{12, 0, 10}, {7, 0, 10}, {13, 0, 10}, {9, 0, 11}, {11, 0, 11}, {10, 0, 12}, {10, 0, 13},
	} {
		pl.put(q[0], q[1], q[2], orange)
	}
	pl.put(10, 0, 10, blue)
	for _, x := range []int32{0, 20} {
		for y := int32(2); y <= 8; y++ {
			switch y {
			case 4, 6:
				pl.put(x, y, 1, orange)
				pl.put(x, y, 2, chiseled)
				pl.put(x, y, 3, orange)
			case 8:
				pl.put(x, y, 1, cut)
				pl.put(x, y, 2, cut)
				pl.put(x, y, 3, cut)
			case 7:
				pl.put(x, y, 1, orange)
				pl.put(x, y, 2, orange)
				pl.put(x, y, 3, orange)
			default:
				pl.put(x, y, 1, cut)
				pl.put(x, y, 2, orange)
				pl.put(x, y, 3, cut)
			}
		}
	}
	for _, x := range []int32{2, 18} {
		for y := int32(2); y <= 8; y++ {
			switch y {
			case 4, 6:
				pl.put(x-1, y, 0, orange)
				pl.put(x, y, 0, chiseled)
				pl.put(x+1, y, 0, orange)
			case 8:
				pl.put(x-1, y, 0, cut)
				pl.put(x, y, 0, cut)
				pl.put(x+1, y, 0, cut)
			case 7:
				pl.put(x-1, y, 0, orange)
				pl.put(x, y, 0, orange)
				pl.put(x+1, y, 0, orange)
			default:
				pl.put(x-1, y, 0, cut)
				pl.put(x, y, 0, orange)
				pl.put(x+1, y, 0, cut)
			}
		}
	}
	same(8, 4, 0, 12, 6, 0, cut)
	pl.put(8, 6, 0, air)
	pl.put(12, 6, 0, air)
	pl.put(9, 5, 0, orange)
	pl.put(10, 5, 0, chiseled)
	pl.put(11, 5, 0, orange)
	same(8, -14, 8, 12, -11, 12, cut)
	same(8, -10, 8, 12, -10, 12, chiseled)
	same(8, -9, 8, 12, -9, 12, cut)
	same(8, -8, 8, 12, -1, 12, sandstone)
	same(9, -11, 9, 11, -1, 11, air)
	pl.put(10, -11, 10, registry.BlockStonePressurePlate.DefaultState())
	box(9, -13, 9, 11, -13, 11, registry.BlockTnt.DefaultState(), air)
	pl.put(8, -11, 10, air)
	pl.put(8, -10, 10, air)
	pl.put(7, -10, 10, chiseled)
	pl.put(7, -11, 10, cut)
	pl.put(12, -11, 10, air)
	pl.put(12, -10, 10, air)
	pl.put(13, -10, 10, chiseled)
	pl.put(13, -11, 10, cut)
	pl.put(10, -11, 8, air)
	pl.put(10, -10, 8, air)
	pl.put(10, -10, 7, chiseled)
	pl.put(10, -11, 7, cut)
	pl.put(10, -11, 12, air)
	pl.put(10, -10, 12, air)
	pl.put(10, -10, 13, chiseled)
	pl.put(10, -11, 13, cut)
	chestDirs := [][3]int32{{10, -11, 8}, {12, -11, 10}, {10, -11, 12}, {8, -11, 10}}
	for dir, pos := range chestDirs {
		i := (dir + 2) & 3
		if p.chests[i] {
			continue
		}
		pl.chestAt(pos[0], pos[1], pos[2], "minecraft:chests/desert_pyramid")
		x, y, z := pl.worldPos(pos[0], pos[1], pos[2])
		p.chests[i] = chunk.inside(x, y, z)
	}
	p.addDesertCellar(pl, r)
}

// addDesertCellar writes the hidden room and records its collapsed roof's
// sand candidates. vanilla: DesertPyramidPiece.addCellar
func (p *desertPyramidPiece) addDesertCellar(pl *strongholdPlacer, r *decorRegion) {
	stairs, _ := registry.WithProp(registry.BlockSandstoneStairs.DefaultState(), registry.PropFacing, fenceDirNames[shW])
	stairs = pl.localState(stairs)
	pl.put(13, -1, 17, stairs)
	pl.put(14, -2, 17, stairs)
	pl.put(15, -3, 17, stairs)
	sand := registry.BlockSand.DefaultState()
	sandstone := registry.BlockSandstone.DefaultState()
	cut := registry.BlockCutSandstone.DefaultState()
	chiseled := registry.BlockChiseledSandstone.DefaultState()
	variant := r.regionRandom().NextBoolean()
	for _, q := range [][3]int32{{12, 0, 17}, {13, 0, 17}, {14, 0, 17}, {15, 0, 17}, {16, 0, 17}, {14, -1, 17}} {
		pl.put(q[0], q[1], q[2], sand)
	}
	if variant {
		pl.put(15, -1, 17, sand)
		pl.put(16, -1, 17, sandstone)
	} else {
		pl.put(15, -1, 17, sandstone)
		pl.put(16, -1, 17, sand)
	}
	pl.put(15, -2, 17, sand)
	pl.put(16, -2, 17, sandstone)
	pl.put(16, -3, 17, sand)
	keepSolid := func(x0, y0, z0, x1, y1, z1 int32, state uint16) {
		for yy := y0; yy <= y1; yy++ {
			for px := x0; px <= x1; px++ {
				for pz := z0; pz <= z1; pz++ {
					if !pl.isAir(px, yy, pz) {
						pl.put(px, yy, pz, state)
					}
				}
			}
		}
	}
	keepSolid(13, -3, 10, 13, -3, 15, cut)
	keepSolid(19, -3, 10, 19, -3, 15, cut)
	keepSolid(13, -3, 10, 19, -3, 12, cut)
	keepSolid(13, -3, 16, 19, -3, 16, cut)
	keepSolid(13, -2, 10, 13, -2, 15, chiseled)
	keepSolid(19, -2, 10, 19, -2, 15, chiseled)
	keepSolid(13, -2, 10, 19, -2, 12, chiseled)
	keepSolid(13, -2, 16, 19, -2, 16, chiseled)
	keepSolid(13, -1, 10, 13, -1, 15, cut)
	keepSolid(19, -1, 10, 19, -1, 15, cut)
	keepSolid(13, -1, 10, 19, -1, 12, cut)
	keepSolid(13, -1, 16, 19, -1, 16, cut)
	for yy := int32(-3); yy <= -1; yy++ {
		for px := int32(14); px <= 18; px++ {
			for pz := int32(11); pz <= 15; pz++ {
				x, y, z := pl.worldPos(px, yy, pz)
				p.sand = append(p.sand, [3]int32{x, y, z})
			}
		}
	}
	cwX, cwY, cwZ := pl.worldPos(14, 0, 11)
	roofRng := r.regionRandom()
	for px := int32(14); px <= 18; px++ {
		for pz := int32(11); pz <= 15; pz++ {
			if roofRng.NextFloat() < 0.33 {
				pl.put(px, 0, pz, sandstone)
			} else {
				pl.put(px, 0, pz, sand)
			}
		}
	}
	collapsedRng := levelgen.NewLegacyRandom(r.worldSeed).ForkPositional().At(cwX, cwY, cwZ)
	px := int32(collapsedRng.NextIntN(5)) + 14
	pz := int32(collapsedRng.NextIntN(5)) + 11
	p.collapsed[0], p.collapsed[1], p.collapsed[2] = pl.worldPos(px, 0, pz)
	orange := registry.BlockOrangeTerracotta.DefaultState()
	blue := registry.BlockBlueTerracotta.DefaultState()
	pl.put(16, -4, 13, blue)
	for _, q := range [][3]int32{{17, -4, 12}, {17, -4, 14}, {15, -4, 12}, {15, -4, 14},
		{18, -4, 13}, {14, -4, 13}, {16, -4, 15}, {16, -4, 11}, {19, -4, 13}} {
		pl.put(q[0], q[1], q[2], orange)
	}
	for _, q := range [][3]int32{{19, -3, 13}, {13, -3, 13}, {16, -3, 16}, {16, -3, 10}} {
		for dy := int32(0); dy <= 1; dy++ {
			x, y, z := pl.worldPos(q[0], q[1]+dy, q[2])
			p.sand = append(p.sand, [3]int32{x, y, z})
		}
	}
	pl.put(20, -3, 13, cut)
	pl.put(20, -2, 13, chiseled)
	pl.put(13, -4, 13, orange)
	pl.put(12, -3, 13, cut)
	pl.put(12, -2, 13, chiseled)
	pl.put(16, -4, 16, orange)
	pl.put(16, -4, 10, orange)
	pl.put(16, -3, 9, cut)
	pl.put(16, -2, 9, chiseled)
}
