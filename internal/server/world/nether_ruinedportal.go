package world

import (
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The nether's ruined portal: a template stood on the terrain, written into
// the chunk holding its centre.

// ruinedPortalNetherDef holds the structure's index within its step, which seeds
// the placement random.
var ruinedPortalNetherDef = levelgen.StructureNamed(ruinedPortalID)

const (
	// The ruined_portals structure set's placement.
	// vanilla: RandomSpreadStructurePlacement
	ruinedPortalSpacing    = 40
	ruinedPortalSeparation = 15
	ruinedPortalSalt       = 34222645
	// ruinedPortalReach is how far, in chunks, a portal's box reaches from its
	// start chunk: structure references reach 8 chunks.
	// vanilla: ChunkGenerator.createReferences
	ruinedPortalReach = 8
	// ruinedPortalSpread is how far, in blocks, netherrack spreads from the
	// centre.
	ruinedPortalSpread = 14
	// ruinedPortalID is the structure's registry id.
	ruinedPortalID = "minecraft:ruined_portal_nether"
)

// ruinedPortalNetherrackChance is the chance a block at an adjusted Manhattan
// distance from the centre turns to netherrack.
// vanilla: RuinedPortalPiece.spreadNetherrack
var ruinedPortalNetherrackChance = [ruinedPortalSpread]float32{1, 1, 1, 1, 1, 1, 1, 0.9, 0.9, 0.8, 0.7, 0.6, 0.4, 0.2}

// ruinedPortal is a portal start: the single piece the structure holds.
// vanilla: RuinedPortalPiece
type ruinedPortal struct {
	chunkX, chunkZ int32
	template       *ruinedPortalTemplate
	rotation       Rotation
	mirror         Mirror
	// pivot is the template-space x and z the piece turns about.
	pivot     [2]int32
	pos       [3]int32
	box       jbox
	airPocket bool
	setup     ruinedPortalSetup
	cold      bool
}

// netherRuinedPortals holds the portals started so far, by placement cell.
type netherRuinedPortals struct {
	mu     sync.Mutex
	starts map[[2]int32]*ruinedPortal
}

// ruinedPortalInCell returns the portal starting in a placement cell, or nil
// when structures are off.
// vanilla: ChunkGenerator.createStructures
func (g *NetherGenerator) ruinedPortalInCell(gx, gz int32) *ruinedPortal {
	if g.noStructures {
		return nil
	}
	rp := &g.portals
	rp.mu.Lock()
	defer rp.mu.Unlock()
	key := [2]int32{gx, gz}
	if p, ok := rp.starts[key]; ok {
		return p
	}
	cx, cz := levelgen.RandomSpreadChunk(g.seed, gx*ruinedPortalSpacing, gz*ruinedPortalSpacing,
		ruinedPortalSpacing, ruinedPortalSeparation, ruinedPortalSalt)
	p := g.newRuinedPortal(cx, cz)
	if rp.starts == nil {
		rp.starts = map[[2]int32]*ruinedPortal{}
	}
	rp.starts[key] = p
	return p
}

// newRuinedPortal draws the portal a chunk starts. The structure set holds
// seven structures but only this one's biomes include the nether's, so the
// others fail their biome check without touching anything this reads.
// vanilla: RuinedPortalStructure.findGenerationPoint
func (g *NetherGenerator) newRuinedPortal(cx, cz int32) *ruinedPortal {
	rng := levelgen.NewLegacyRandom(0)
	rng.SetLargeFeatureSeed(g.seed, cx, cz)
	p := newRuinedPortalPiece(rng, cx, cz, ruinedPortals().setups[ruinedPortalID][0])
	box := p.box
	y := g.ruinedPortalY(rng, p.airPocket, box)
	p.pos = [3]int32{cx * 16, y, cz * 16}
	p.box = box.moved(0, y, 0)
	return p
}

// newRuinedPortalPiece draws the template and its transform before projecting it.
// vanilla: RuinedPortalStructure.findGenerationPoint, sample
func newRuinedPortalPiece(rng *levelgen.LegacyRandomSource, cx, cz int32, setup ruinedPortalSetup) *ruinedPortal {
	def := ruinedPortals()
	p := &ruinedPortal{chunkX: cx, chunkZ: cz, setup: setup}
	p.airPocket = setup.AirPocket == 1 || setup.AirPocket != 0 && rng.NextFloat() < setup.AirPocket
	if rng.NextFloat() < 0.05 {
		p.template = def.giants[rng.NextIntN(int32(len(def.giants)))]
	} else {
		p.template = def.portals[rng.NextIntN(int32(len(def.portals)))]
	}
	p.rotation = Rotation(rng.NextIntN(4))
	if rng.NextFloat() >= 0.5 {
		p.mirror = MirrorFrontBack
	}
	size := p.template.Size
	p.pivot = [2]int32{size[0] / 2, size[2] / 2}
	x1, _, z1 := p.transform(0, 0, 0)
	x2, _, z2 := p.transform(size[0]-1, size[1]-1, size[2]-1)
	p.box = jbox{min(x1, x2), 0, min(z1, z2), max(x1, x2), size[1] - 1, max(z1, z2)}.moved(cx*16, 0, cz*16)
	return p
}

// transform mirrors a template position about the origin, then turns it about
// the pivot.
// vanilla: StructureTemplate.transform
func (p *ruinedPortal) transform(x, y, z int32) (int32, int32, int32) {
	switch p.mirror {
	case MirrorLeftRight:
		z = -z
	case MirrorFrontBack:
		x = -x
	}
	px, pz := p.pivot[0], p.pivot[1]
	switch p.rotation {
	case Rotate180:
		return px + px - x, y, pz + pz - z
	case RotateCounterclockwise90:
		return px - pz + z, y, px + pz - x
	case RotateClockwise90:
		return px + pz - z, y, pz - px + x
	}
	return x, y, z
}

// ruinedPortalY picks the portal's height: a random start, then down to the
// first level where three of the box's four bottom corners stand in the
// terrain's base column.
// vanilla: RuinedPortalStructure.findSuitableY
func (g *NetherGenerator) ruinedPortalY(rng *levelgen.LegacyRandomSource, airPocket bool, box jbox) int32 {
	var y int32
	switch {
	case airPocket:
		y = rng.NextIntN(69) + 32
	case rng.NextFloat() < 0.5:
		y = rng.NextIntN(3) + 27
	default:
		y = rng.NextIntN(72) + 29
	}
	// The base column is the noise terrain before carvers and surface.
	// vanilla: NoiseBasedChunkGenerator.getBaseColumn
	corners := [4][2]int32{{box.minX, box.minZ}, {box.maxX, box.minZ}, {box.minX, box.maxZ}, {box.maxX, box.maxZ}}
	columns := g.baseColumns(corners)
	// The world's bottom plus 15.
	for ; y > netherBottom+15; y-- {
		solid := 0
		for i, c := range corners {
			// WORLD_SURFACE_WG counts anything but air, lava included.
			if columns[i].BlockAt(c[0]&15, y, c[1]&15) != airState {
				solid++
				if solid == 3 {
					return y
				}
			}
		}
	}
	return y
}

// eachRuinedPortal calls fn for each portal whose box reaches chunk (cx, cz).
// vanilla: ChunkGenerator.createReferences
func (g *NetherGenerator) eachRuinedPortal(cx, cz int32, fn func(*ruinedPortal)) {
	const reach = ruinedPortalReach
	for gz := floorDiv(cz-reach, ruinedPortalSpacing); gz <= floorDiv(cz+reach, ruinedPortalSpacing); gz++ {
		for gx := floorDiv(cx-reach, ruinedPortalSpacing); gx <= floorDiv(cx+reach, ruinedPortalSpacing); gx++ {
			p := g.ruinedPortalInCell(gx, gz)
			if p == nil || absInt32(p.chunkX-cx) > reach || absInt32(p.chunkZ-cz) > reach {
				continue
			}
			if p.box.intersects(jbox{cx * 16, 0, cz * 16, cx*16 + 15, netherGenHeight - 1, cz*16 + 15}) {
				fn(p)
			}
		}
	}
}

// placeSurfaceStructures writes the structures of the surface_structures step
// that reach chunk (nx, nz), before the step's features.
func (r *decorRegion) placeSurfaceStructures(decorationSeed int64, nx, nz int32) {
	r.placeBastions(decorationSeed, nx, nz)
	r.placeRuinedPortals(decorationSeed, nx, nz)
}

// placeRuinedPortals writes the portals whose centre is in chunk (nx, nz).
// vanilla: ChunkGenerator.applyBiomeDecoration, StructureStart.placeInChunk
func (r *decorRegion) placeRuinedPortals(decorationSeed int64, nx, nz int32) {
	r.rng.SetFeatureSeed(decorationSeed, ruinedPortalNetherDef.Index, ruinedPortalNetherDef.Step)
	// vanilla: ChunkGenerator.getWritableArea
	writable := jbox{nx * 16, 1, nz * 16, nx*16 + 15, netherGenHeight - 1, nz*16 + 15}
	r.g.eachRuinedPortal(nx, nz, func(p *ruinedPortal) {
		// Only the chunk holding the box's centre writes the portal, all of it.
		// vanilla: RuinedPortalPiece.postProcess
		b := p.box
		if cx, cy, cz := b.minX+(b.maxX-b.minX+1)/2, b.minY+(b.maxY-b.minY+1)/2, b.minZ+(b.maxZ-b.minZ+1)/2; b.intersects(writable) && writable.inside(cx, cy, cz) {
			r.placeRuinedPortal(p)
		}
	})
}

// placeRuinedPortal runs the template's blocks through the processors against
// the world as it stands, writes them, settles the shapes of stairs and
// walls, then spreads netherrack below.
// vanilla: RuinedPortalPiece.postProcess, StructureTemplate.placeInWorld
func (r *decorRegion) placeRuinedPortal(p *ruinedPortal) {
	placed := &PlacedTemplate{}
	for _, blk := range p.template.Palettes[0] {
		tx, ty, tz := p.transform(blk.Pos[0], blk.Pos[1], blk.Pos[2])
		x, y, z := tx+p.pos[0], ty+p.pos[1], tz+p.pos[2]
		if state, ok := r.processPortalBlock(p, blk.State, x, y, z); ok {
			state = RotateState(MirrorState(state, p.mirror), p.rotation)
			placed.Changes = append(placed.Changes, BlockChange{X: x, Y: y, Z: z, State: state})
			placed.Tags = append(placed.Tags, blk.NBT)
		}
	}
	r.placeFeatureTemplate(placed)
	var shapes []posKey
	for _, b := range placed.Changes {
		shapes = append(shapes, posKey{b.X, b.Y, b.Z})
	}
	r.templateShapes(shapes)
	// A jigsaw block stands during the placement, then becomes its final state.
	// vanilla: TemplateStructurePiece.postProcess
	for _, blk := range p.template.Palettes[0] {
		if registry.BlockOf(blk.State) != registry.BlockJigsaw || blk.NBT == nil {
			continue
		}
		tx, ty, tz := p.transform(blk.Pos[0], blk.Pos[1], blk.Pos[2])
		r.set(tx+p.pos[0], ty+p.pos[1], tz+p.pos[2], jigsawFinalState(blk.NBT))
	}
	r.spreadNetherrack(p)
	r.addNetherrackDripColumnsBelowPortal(p)
	r.growRuinedPortal(p)
}

// growRuinedPortal adds the vines and overgrown leaves of the portal's setup.
// vanilla: RuinedPortalPiece.postProcess
func (r *decorRegion) growRuinedPortal(p *ruinedPortal) {
	if !p.setup.Vines && !p.setup.Overgrown {
		return
	}
	b := p.box
	// BlockPos.betweenClosed: x fastest, then y, then z.
	for z := b.minZ; z <= b.maxZ; z++ {
		for y := b.minY; y <= b.maxY; y++ {
			for x := b.minX; x <= b.maxX; x++ {
				if p.setup.Vines {
					r.portalVines(x, y, z)
				}
				if p.setup.Overgrown {
					r.portalLeaves(x, y, z)
				}
			}
		}
	}
}

// jigsawFinalState is the block a jigsaw block of a ruined portal template
// leaves: netherrack or air, as the templates name it.
func jigsawFinalState(tag nbt.Compound) uint16 {
	if final, _ := nbt.Get[nbt.String](tag, "final_state"); final == "minecraft:netherrack" {
		return registry.BlockNetherrack.DefaultState()
	}
	return airState
}

var (
	// ruinedPortalRules are the piece's rule processor: gold may go, lava and
	// netherrack may turn to magma.
	// vanilla: RuinedPortalPiece.makeSettings
	ruinedPortalRules = ruleProcessor{rules: []processorRule{
		{input: registry.BlockGoldBlock, probability: 0.3, output: airState},
		{input: registry.BlockLava, probability: 0.2, output: registry.BlockMagmaBlock.DefaultState()},
		{input: registry.BlockNetherrack, probability: 0.07, output: registry.BlockMagmaBlock.DefaultState()},
	}}
)

var (
	coldOceanPortalRules = ruleProcessor{rules: oceanPortalRules.rules[:2]}
	oceanPortalRules     = ruleProcessor{rules: []processorRule{
		{input: registry.BlockGoldBlock, probability: 0.3, output: airState},
		{kind: inputBlock, input: registry.BlockLava, output: registry.BlockMagmaBlock.DefaultState()},
		{input: registry.BlockNetherrack, probability: 0.07, output: registry.BlockMagmaBlock.DefaultState()},
	}}
	coldPortalRules = ruleProcessor{rules: []processorRule{
		{input: registry.BlockGoldBlock, probability: 0.3, output: airState},
		{kind: inputBlock, input: registry.BlockLava, output: registry.BlockNetherrack.DefaultState()},
	}}
)

// processPortalBlock runs a template block through the piece's processors in
// order; false when one drops it. Each draws from its own random seeded by the
// position, and reads the world before the piece wrote anything.
// vanilla: StructureTemplate.processBlockInfos
func (r *decorRegion) processPortalBlock(p *ruinedPortal, state uint16, x, y, z int32) (uint16, bool) {
	// vanilla: BlockIgnoreProcessor
	if b := registry.BlockOf(state); b == registry.BlockStructureBlock || b == registry.BlockAir && !p.airPocket {
		return 0, false
	}
	world := r.get(x, y, z)
	rules := &ruinedPortalRules
	if p.setup.Placement == "on_ocean_floor" {
		rules = &oceanPortalRules
		if p.cold {
			rules = &coldOceanPortalRules
		}
	} else if p.cold {
		rules = &coldPortalRules
	}
	state = rules.process(state, world, x, y, z, 0)
	state = ruinedPortalAge(state, levelgen.NewLegacyRandom(blockPosSeed(x, y, z)), p.setup.Mossiness)
	// vanilla: ProtectedBlockProcessor
	if stateInTag(world, registry.TagIDFeaturesCannotReplace) {
		return 0, false
	}
	// vanilla: LavaSubmergedBlockProcessor
	if registry.BlockOf(world) == registry.BlockLava && !registry.CollisionShapeFull(state) {
		return registry.BlockLava.DefaultState(), true
	}
	if p.setup.Blackstone {
		state = blackstoneReplace(state)
	}
	return state, true
}

// ruinedPortalAge cracks and breaks the stone of the portal, and makes some
// obsidian cry.
// vanilla: BlockAgeProcessor.processBlock
func ruinedPortalAge(state uint16, rng *levelgen.LegacyRandomSource, mossiness float32) uint16 {
	b := registry.BlockOf(state)
	switch {
	case b == registry.BlockStone || b == registry.BlockStoneBricks || b == registry.BlockChiseledStoneBricks:
		if rng.NextFloat() >= 0.5 {
			return state
		}
		stairs := randomFacingStairs(rng, registry.BlockStoneBrickStairs)
		mossyStairs := randomFacingStairs(rng, registry.BlockMossyStoneBrickStairs)
		blocks := [2]uint16{registry.BlockCrackedStoneBricks.DefaultState(), stairs}
		if rng.NextFloat() < mossiness {
			blocks = [2]uint16{registry.BlockMossyStoneBricks.DefaultState(), mossyStairs}
		}
		return blocks[rng.NextIntN(2)]
	case b.IsStairs():
		if rng.NextFloat() >= 0.5 {
			return state
		}
		blocks := [2]uint16{registry.BlockStoneSlab.DefaultState(), registry.BlockStoneBrickSlab.DefaultState()}
		if rng.NextFloat() < mossiness {
			blocks = [2]uint16{portalProperties(registry.BlockMossyStoneBrickStairs, state), registry.BlockMossyStoneBrickSlab.DefaultState()}
		}
		return blocks[rng.NextIntN(2)]
	case b.InTag(portalSlabs):
		if rng.NextFloat() < mossiness {
			return portalProperties(registry.BlockMossyStoneBrickSlab, state)
		}
	case b.IsWall():
		if rng.NextFloat() < mossiness {
			return portalProperties(registry.BlockMossyStoneBrickWall, state)
		}
	case b == registry.BlockObsidian && rng.NextFloat() < 0.15:
		return registry.BlockCryingObsidian.DefaultState()
	}
	return state
}

// randomFacingStairs is a stair block facing a random side, bottom or top.
// vanilla: BlockAgeProcessor.getRandomFacingStairs
func randomFacingStairs(rng *levelgen.LegacyRandomSource, b registry.Block) uint16 {
	facing := [4]string{"north", "east", "south", "west"}[rng.NextIntN(4)]
	half := [2]string{"top", "bottom"}[rng.NextIntN(2)]
	state, _ := registry.WithProps(b.DefaultState(), registry.PropValue{P: registry.PropFacing, V: facing}, registry.PropValue{P: registry.PropHalf, V: half})
	return state
}

// blackstoneReplacements maps the portal's stone to its blackstone kin.
// vanilla: BlackstoneReplaceProcessor
var blackstoneReplacements = func() (m [registry.BlockCount]registry.Block) {
	for i := range m {
		m[i] = registry.Block(i)
	}
	for _, pair := range [][2]string{
		{"cobblestone", "blackstone"}, {"mossy_cobblestone", "blackstone"}, {"stone", "polished_blackstone"},
		{"stone_bricks", "polished_blackstone_bricks"}, {"mossy_stone_bricks", "polished_blackstone_bricks"},
		{"cobblestone_stairs", "blackstone_stairs"}, {"mossy_cobblestone_stairs", "blackstone_stairs"},
		{"stone_stairs", "polished_blackstone_stairs"}, {"stone_brick_stairs", "polished_blackstone_brick_stairs"},
		{"mossy_stone_brick_stairs", "polished_blackstone_brick_stairs"},
		{"cobblestone_slab", "blackstone_slab"}, {"mossy_cobblestone_slab", "blackstone_slab"},
		{"smooth_stone_slab", "polished_blackstone_slab"}, {"stone_slab", "polished_blackstone_slab"},
		{"stone_brick_slab", "polished_blackstone_brick_slab"}, {"mossy_stone_brick_slab", "polished_blackstone_brick_slab"},
		{"stone_brick_wall", "polished_blackstone_brick_wall"}, {"mossy_stone_brick_wall", "polished_blackstone_brick_wall"},
		{"cobblestone_wall", "blackstone_wall"}, {"mossy_cobblestone_wall", "blackstone_wall"},
		{"chiseled_stone_bricks", "chiseled_polished_blackstone"}, {"cracked_stone_bricks", "cracked_polished_blackstone_bricks"},
		{"iron_bars", "iron_chain"},
	} {
		from, ok1 := registry.BlockByName("minecraft:" + pair[0])
		to, ok2 := registry.BlockByName("minecraft:" + pair[1])
		if !ok1 || !ok2 {
			panic("blackstone replacement: unknown block " + pair[0] + " or " + pair[1])
		}
		m[from] = to
	}
	return m
}()

// blackstoneReplace swaps a block for its blackstone kin, keeping a stair's
// facing and half and a slab's type and nothing else.
func blackstoneReplace(state uint16) uint16 {
	b := registry.BlockOf(state)
	to := blackstoneReplacements[b]
	if to == b {
		return state
	}
	out := to.DefaultState()
	for _, p := range [...]registry.Prop{registry.PropFacing, registry.PropHalf, registry.PropType} {
		if v := registry.StateValue(state, p); v != "" {
			out, _ = registry.WithProp(out, p, v)
		}
	}
	return out
}

// spreadNetherrack scatters netherrack and magma over the ground around the
// portal, falling off with distance.
// vanilla: RuinedPortalPiece.spreadNetherrack
func (r *decorRegion) spreadNetherrack(p *ruinedPortal) {
	b := p.box
	cx, cz := b.minX+(b.maxX-b.minX+1)/2, b.minZ+(b.maxZ-b.minZ+1)/2
	averageWidth := (b.maxX - b.minX + 1 + b.maxZ - b.minZ + 1) / 2
	adjustment := r.rng.NextIntN(max(1, 8-averageWidth/2))
	for x := cx - ruinedPortalSpread; x <= cx+ruinedPortalSpread; x++ {
		for z := cz - ruinedPortalSpread; z <= cz+ruinedPortalSpread; z++ {
			distance := max(0, absInt32(x-cx)+absInt32(z-cz)+adjustment)
			if distance >= ruinedPortalSpread || !(r.rng.NextDouble() < float64(ruinedPortalNetherrackChance[distance])) {
				continue
			}
			hm := hmWorldSurfaceWG
			if p.setup.Placement == "on_ocean_floor" {
				hm = hmOceanFloorWG
			}
			y := r.heightOf(hm, x, z) - 1
			if p.setup.Placement != "on_land_surface" && p.setup.Placement != "on_ocean_floor" {
				y = min(b.minY, y)
			}
			if absInt32(y-b.minY) > 3 || !r.canBecomeNetherrack(p, x, y, z) {
				continue
			}
			r.placeNetherrackOrMagma(p, x, y, z)
			if p.setup.Overgrown {
				r.portalLeaves(x, y, z)
			}
			r.addNetherrackDripColumn(p, x, y-1, z)
		}
	}
}

// canBecomeNetherrack is the test a block must pass to be replaced; lava may
// be, in the nether.
// vanilla: RuinedPortalPiece.canBlockBeReplacedByNetherrackOrMagma
func (r *decorRegion) canBecomeNetherrack(p *ruinedPortal, x, y, z int32) bool {
	state := r.get(x, y, z)
	return registry.BlockOf(state) != registry.BlockAir && registry.BlockOf(state) != registry.BlockObsidian &&
		!stateInTag(state, registry.TagIDFeaturesCannotReplace) && (p.setup.Placement == "in_nether" || registry.BlockOf(state) != registry.BlockLava)
}

func (r *decorRegion) placeNetherrackOrMagma(p *ruinedPortal, x, y, z int32) {
	if !r.canBecomeNetherrack(p, x, y, z) {
		return
	}
	if !p.cold && r.rng.NextFloat() < 0.07 {
		r.set(x, y, z, registry.BlockMagmaBlock.DefaultState())
	} else {
		r.set(x, y, z, registry.BlockNetherrack.DefaultState())
	}
}

// addNetherrackDripColumn places netherrack down from a block, on and on with
// even odds for at most eight more.
// vanilla: RuinedPortalPiece.addNetherrackDripColumn
func (r *decorRegion) addNetherrackDripColumn(p *ruinedPortal, x, y, z int32) {
	r.placeNetherrackOrMagma(p, x, y, z)
	for remaining := 8; remaining > 0 && r.rng.NextFloat() < 0.5; remaining-- {
		y--
		r.placeNetherrackOrMagma(p, x, y, z)
	}
}

// addNetherrackDripColumnsBelowPortal hangs a column under each netherrack
// block of the box's bottom layer, edges left out.
// vanilla: RuinedPortalPiece.addNetherrackDripColumnsBelowPortal
func (r *decorRegion) addNetherrackDripColumnsBelowPortal(p *ruinedPortal) {
	b := p.box
	for x := b.minX + 1; x < b.maxX; x++ {
		for z := b.minZ + 1; z < b.maxZ; z++ {
			if r.get(x, b.minY, z) == registry.BlockNetherrack.DefaultState() {
				r.addNetherrackDripColumn(p, x, b.minY-1, z)
			}
		}
	}
}

// saveRuinedPortals records the portal that starts in the chunk, and the ones
// that reach into it, in the chunk's structures tag.
// vanilla: ChunkStatus.STRUCTURE_STARTS, STRUCTURE_REFERENCES
func (g *NetherGenerator) saveRuinedPortals(c *Chunk) {
	var refs []int64
	g.eachRuinedPortal(c.x, c.z, func(p *ruinedPortal) {
		if p.chunkX == c.x && p.chunkZ == c.z {
			c.setStructureStart(ruinedPortalID, p.nbt())
		}
		refs = append(refs, int64(uint32(p.chunkX))|int64(p.chunkZ)<<32)
	})
	if len(refs) > 0 {
		c.setStructureReferences(ruinedPortalID, refs)
	}
}

var mirrorNames = [3]string{"NONE", "LEFT_RIGHT", "FRONT_BACK"}

// pieceNBT is the saved ruined portal piece.
// vanilla: RuinedPortalPiece.addAdditionalSaveData
func (p *ruinedPortal) pieceNBT() nbt.Compound {
	flag := func(v bool) nbt.Byte {
		if v {
			return 1
		}
		return 0
	}
	return nbt.Compound{
		"id":                nbt.String("minecraft:rupo"),
		"BB":                nbt.IntArray{p.box.minX, p.box.minY, p.box.minZ, p.box.maxX, p.box.maxY, p.box.maxZ},
		"GD":                nbt.Int(0),
		"O":                 nbt.Int(2),
		"TPX":               nbt.Int(p.pos[0]),
		"TPY":               nbt.Int(p.pos[1]),
		"TPZ":               nbt.Int(p.pos[2]),
		"Template":          nbt.String(p.template.id),
		"Rotation":          nbt.String(rotationNames[p.rotation]),
		"Mirror":            nbt.String(mirrorNames[p.mirror]),
		"VerticalPlacement": nbt.String(p.setup.Placement),
		"Properties": nbt.Compound{
			"cold": flag(p.cold), "mossiness": nbt.Float(p.setup.Mossiness), "air_pocket": flag(p.airPocket),
			"overgrown": flag(p.setup.Overgrown), "vines": flag(p.setup.Vines), "replace_with_blackstone": flag(p.setup.Blackstone),
		},
	}
}

func (p *ruinedPortal) nbt() nbt.Compound {
	return structureStartNBT(ruinedPortalID, p.chunkX, p.chunkZ, []nbt.Tag{p.pieceNBT()})
}

// baseColumns returns the chunk of each column; a chunk shared by several
// columns is generated once. It leaves out the beard.
// vanilla: NoiseBasedChunkGenerator.iterateNoiseColumn (BeardifierMarker)
func (g *NetherGenerator) baseColumns(corners [4][2]int32) [4]*Chunk {
	chunks := map[ChunkPos]*Chunk{}
	var columns [4]*Chunk
	for i, c := range corners {
		pos := ChunkPos{X: c[0] >> 4, Z: c[1] >> 4}
		if chunks[pos] == nil {
			chunks[pos] = newChunk(pos.X, pos.Z)
			g.terrain(chunks[pos], false)
		}
		columns[i] = chunks[pos]
	}
	return columns
}

func portalProperties(b registry.Block, from uint16) uint16 {
	state, _ := registry.WithPropertiesOf(b, from)
	return state
}

// vanilla: RuinedPortalPiece.maybeAddLeavesAbove
func (r *decorRegion) portalLeaves(x, y, z int32) {
	if r.rng.NextFloat() < 0.5 && registry.BlockOf(r.get(x, y, z)) == registry.BlockNetherrack && r.isAir(x, y+1, z) {
		r.set(x, y+1, z, portalJungleLeaves)
	}
}

// vanilla: RuinedPortalPiece.maybeAddVines
func (r *decorRegion) portalVines(x, y, z int32) {
	state := r.get(x, y, z)
	if r.src.isAir(state) || registry.BlockOf(state) == registry.BlockVine {
		return
	}
	// Direction.Plane.HORIZONTAL: north, east, south, west.
	i := r.rng.NextIntN(4)
	d := [4]posKey{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}[i]
	nx, nz := x+d[0], z+d[2]
	if r.isAir(nx, y, nz) && registry.CollisionFaceFull(state, [4]int{2, 5, 3, 4}[i]) {
		vine, _ := registry.WithBool(registry.BlockVine.DefaultState(), [4]registry.Prop{registry.PropSouth, registry.PropWest, registry.PropNorth, registry.PropEast}[i], true)
		r.set(nx, y, nz, vine)
	}
}

var portalJungleLeaves, _ = registry.WithBool(registry.BlockJungleLeaves.DefaultState(), registry.PropPersistent, true)

var portalSlabs = registry.BlockTagOf("minecraft:slabs")
