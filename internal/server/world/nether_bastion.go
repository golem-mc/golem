package world

import (
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The nether's bastion remnant: the jigsaw structure sharing the
// nether_complexes placement with the fortress, written into each chunk its
// pieces reach.

// bastionDef holds the structure's index within its step, which seeds the
// placement random.
var bastionDef = levelgen.StructureNamed(bastionID)

const (
	// bastionStep is the surface_structures step the bastion is in.
	// vanilla: ChunkGenerator.applyBiomeDecoration
	bastionStep = 4
	// netherBastionReach is how far, in chunks, a bastion's pieces reach from
	// its start chunk: structure references reach 8 chunks.
	// vanilla: ChunkGenerator.createReferences
	netherBastionReach = 8
)

// netherBastion holds the bastions grown so far, the same for every chunk
// reading them.
type netherBastion struct {
	def    *jigsawStructure
	mu     sync.Mutex
	starts map[[2]int32]*jigsawStart
}

func (g *NetherGenerator) initBastion() {
	b := &g.bastion
	b.starts = map[[2]int32]*jigsawStart{}
}

// bastionInCell returns the bastion starting in a nether_complexes cell, or nil
// when the cell's start is the fortress.
// vanilla: ChunkGenerator.createStructures
func (g *NetherGenerator) bastionInCell(gx, gz int32) *jigsawStart {
	b := &g.bastion
	if g.noStructures {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	key := [2]int32{gx, gz}
	if s, ok := b.starts[key]; ok {
		return s
	}
	sx, sz := levelgen.RandomSpreadChunk(g.seed, gx*levelgen.NetherComplexSpacing, gz*levelgen.NetherComplexSpacing,
		levelgen.NetherComplexSpacing, levelgen.NetherComplexSeparation, levelgen.NetherComplexSalt)
	var s *jigsawStart
	if !g.complexIsFortress(sx, sz) {
		if b.def == nil {
			// Read on the first bastion lookup: most servers never generate a nether chunk.
			b.def = jigsawStructureNamed(bastionID)
		}
		s = buildJigsawStart(b.def, g.seed, sx, sz, 0, netherGenHeight-1, nil)
	}
	b.starts[key] = s
	return s
}

// referencedBy reports whether chunk (cx, cz), whose block box is box, holds a
// reference to the start: it lies within netherBastionReach chunks of the start's
// chunk and the start's box meets it.
// vanilla: ChunkGenerator.createReferences
func (s *jigsawStart) referencedBy(cx, cz int32, box jbox) bool {
	return absInt32(s.chunkX-cx) <= netherBastionReach && absInt32(s.chunkZ-cz) <= netherBastionReach && s.box.intersects(box)
}

// placeBastions writes the parts of bastions in chunk (nx, nz), before the
// features of the surface_structures step.
// vanilla: ChunkGenerator.applyBiomeDecoration, StructureStart.placeInChunk
func (r *decorRegion) placeBastions(decorationSeed int64, nx, nz int32) {
	g := r.g
	const reach = netherBastionReach
	r.rng.SetFeatureSeed(decorationSeed, bastionDef.Index, bastionDef.Step)
	// The writable area leaves out the lowest layer.
	// vanilla: ChunkGenerator.getWritableArea
	chunk := jbox{nx * 16, 1, nz * 16, nx*16 + 15, netherGenHeight - 1, nz*16 + 15}
	for gz := floorDiv(nz-reach, levelgen.NetherComplexSpacing); gz <= floorDiv(nz+reach, levelgen.NetherComplexSpacing); gz++ {
		for gx := floorDiv(nx-reach, levelgen.NetherComplexSpacing); gx <= floorDiv(nx+reach, levelgen.NetherComplexSpacing); gx++ {
			if s := g.bastionInCell(gx, gz); s != nil && s.referencedBy(nx, nz, chunk) {
				r.placeJigsawStart(s, chunk)
			}
		}
	}
}

// placeJigsawStart writes each piece of the start that meets the chunk.
// vanilla: StructureStart.placeInChunk
func (r *decorRegion) placeJigsawStart(s *jigsawStart, chunk jbox) {
	centre := s.pieces[0].box
	// BoundingBox.getCenter, at the bottom of the box.
	ref := [3]int32{0, centre.minY, 0}
	for _, p := range s.pieces {
		if p.box.intersects(chunk) {
			p.place(r, chunk, ref)
		}
	}
}

// process runs the processors every jigsaw element has: structure blocks are
// dropped and a jigsaw block becomes its final_state, without its data.
// vanilla: BlockIgnoreProcessor.STRUCTURE_BLOCK, JigsawReplacementProcessor
func processTemplateBlock(blk TemplateBlock, x, y, z int32) (state uint16, tag nbt.Compound, ok bool) {
	switch registry.BlockOf(blk.State) {
	case registry.BlockStructureBlock:
		return 0, nil, false
	case registry.BlockJigsaw:
		if blk.NBT == nil {
			return blk.State, nil, true
		}
		final, _ := nbt.Get[nbt.String](blk.NBT, "final_state")
		if final == "" {
			final = "minecraft:air"
		}
		name, props, _ := strings.Cut(strings.TrimSuffix(string(final), "]"), "[")
		if b, ok := registry.BlockByName(registry.Qualify(name)); ok && b == registry.BlockStructureVoid {
			return 0, nil, false
		}
		state, found := registry.FindState(registry.Qualify(name), registry.ParseStateProps(props))
		if !found {
			return 0, nil, false
		}
		return state, nil, true
	}
	return blk.State, blk.NBT, true
}

// randomizableContainers are the blocks besides chests and shulker boxes whose
// block entity is a RandomizableContainer.
var randomizableContainers = blockSet(registry.BlockBarrel,
	registry.BlockDispenser, registry.BlockDropper, registry.BlockHopper, registry.BlockCrafter, registry.BlockDecoratedPot)

// placeBlockEntity records the block entity a template block with data
// carries: a container (a RandomizableContainer) takes a loot seed from the
// placement random. Data on a block without a modelled entity is dropped.
// vanilla: StructureTemplate.placeInWorld
func (r *decorRegion) placeBlockEntity(x, y, z int32, state uint16, tag nbt.Compound) {
	b := registry.BlockOf(state)
	switch {
	case randomizableContainers[b] || IsChestBlock(b) || b.IsShulkerBox():
		tag = nbt.Copy(tag).(nbt.Compound)
		tag["LootTableSeed"] = nbt.Long(r.rng.NextLong())
	case b == registry.BlockSpawner || b == registry.BlockTrialSpawner || b == registry.BlockVault:
		tag = nbt.Copy(tag).(nbt.Compound)
	case b == registry.BlockSuspiciousSand || b == registry.BlockSuspiciousGravel:
		tag = nbt.Copy(tag).(nbt.Compound)
		tag["id"] = nbt.String(KindBrushable.Name())
	default:
		// Any other block entity loads the data over its fresh entity, so the
		// id is the block's whatever the template wrote.
		kind := dummyEntityKinds[b]
		if kind == KindNone {
			return
		}
		tag = nbt.Copy(tag).(nbt.Compound)
		tag["id"] = nbt.String(kind.Name())
		if !containerKind(kind) {
			// A banner or skull stays carried, so it is kept as vanilla saves
			// it once loaded (place adds the components).
			// vanilla: BannerBlockEntity.saveAdditional
			if l, ok := nbt.Get[nbt.List](tag, "patterns"); ok && len(l.Items) == 0 {
				delete(tag, "patterns")
			}
		}
	}
	p := r.protoAt(x, z)
	p.entities = append(p.entities, protoEntity{x: x & 15, y: y - r.minY, z: z & 15, tag: tag})
}

// saveStructures records the bastion or fortress that starts in the chunk, and
// the ones that reach into it, in the chunk's structures tag.
// vanilla: ChunkStatus.STRUCTURE_STARTS, STRUCTURE_REFERENCES
func (g *NetherGenerator) saveStructures(c *Chunk) {
	const reach = netherBastionReach
	chunk := jbox{c.x * 16, 0, c.z * 16, c.x*16 + 15, netherGenHeight - 1, c.z*16 + 15}
	var refs, fortressRefs []int64
	for gz := floorDiv(c.z-reach, levelgen.NetherComplexSpacing); gz <= floorDiv(c.z+reach, levelgen.NetherComplexSpacing); gz++ {
		for gx := floorDiv(c.x-reach, levelgen.NetherComplexSpacing); gx <= floorDiv(c.x+reach, levelgen.NetherComplexSpacing); gx++ {
			if f := g.fortressInCell(gx, gz); f != nil {
				if f.ChunkX == c.x && f.ChunkZ == c.z {
					c.setLiveStructureStart(fortressID, func() nbt.Compound { return fortressStartNBT(f) })
				}
				if absInt32(f.ChunkX-c.x) <= reach && absInt32(f.ChunkZ-c.z) <= reach && f.Intersects(c.x, c.z) {
					fortressRefs = append(fortressRefs, packChunk(f.ChunkX, f.ChunkZ))
				}
			}
			s := g.bastionInCell(gx, gz)
			if s == nil {
				continue
			}
			if s.chunkX == c.x && s.chunkZ == c.z {
				c.setStructureStart(bastionID, s.nbt(bastionID))
			}
			if s.referencedBy(c.x, c.z, chunk) {
				refs = append(refs, packChunk(s.chunkX, s.chunkZ))
			}
		}
	}
	if len(refs) > 0 {
		c.setStructureReferences(bastionID, refs)
	}
	if len(fortressRefs) > 0 {
		c.setStructureReferences(fortressID, fortressRefs)
	}
	g.saveRuinedPortals(c)
}

// SetStructureMobFinalizer makes GeneratedEntities finalize the bastions'
// mobs; without one they are marked with StructureFinalizeKey.
func (g *NetherGenerator) SetStructureMobFinalizer(f StructureMobFinalizer) { g.mobFinalizer = f }

// GeneratedEntities returns the mobs the bastions' templates stand in the
// chunk, finalized on the chunk's region random in placement order, or marked
// for finalization when loaded. UUIDs derived from the structure
// keep proto regeneration deterministic; vanilla draws them from an unseeded random.
// vanilla: StructureTemplate.placeEntities
func (g *NetherGenerator) GeneratedEntities(pos ChunkPos) []nbt.Compound {
	if g.noStructures {
		return nil
	}
	const reach = netherBastionReach
	chunk := jbox{pos.X * 16, 1, pos.Z * 16, pos.X*16 + 15, netherGenHeight - 1, pos.Z*16 + 15}
	var out []nbt.Compound
	var rng levelgen.RandomSource
	for gz := floorDiv(pos.Z-reach, levelgen.NetherComplexSpacing); gz <= floorDiv(pos.Z+reach, levelgen.NetherComplexSpacing); gz++ {
		for gx := floorDiv(pos.X-reach, levelgen.NetherComplexSpacing); gx <= floorDiv(pos.X+reach, levelgen.NetherComplexSpacing); gx++ {
			s := g.bastionInCell(gx, gz)
			if s == nil || !s.referencedBy(pos.X, pos.Z, chunk) {
				continue
			}
			for i, p := range s.pieces {
				if !p.box.intersects(chunk) {
					continue
				}
				for j, e := range p.element.template.Entities {
					x, y, z := TransformTemplatePos(e.BlockPos[0], e.BlockPos[1], e.BlockPos[2], MirrorNone, p.rotation)
					if !chunk.inside(x+p.pos[0], y+p.pos[1], z+p.pos[2]) {
						continue
					}
					placed, ok := e.place(TemplatePlacement{Origin: p.pos, Rotation: p.rotation})
					if !ok {
						continue
					}
					id := uuid.NewSHA1(uuid.NameSpaceOID, fmt.Appendf(nil, "bastion/%d/%d/%d/%d/%d", g.seed, s.chunkX, s.chunkZ, i, j))
					placed.Tag["UUID"] = nbt.UUIDToIntArray(id)
					tag := placed.Tag
					if g.mobFinalizer == nil {
						tag[StructureFinalizeKey] = nbt.Byte(1)
					} else {
						if rng == nil {
							// vanilla: WorldGenRegion's random for the chunk's FEATURES;
							// the nether's RandomState is legacy, so it is a LegacyRandomSource.
							rng = g.state.RandomFactory("minecraft:worldgen_region_random").At(pos.X*16, 0, pos.Z*16)
						}
						tag = g.mobFinalizer(placed.Tag, g.biomeAt(placed.Pos), false, rng)
					}
					out = append(out, tag)
				}
			}
		}
	}
	return out
}
