package world

import (
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The overworld's structure layer: the chunks each structure set may start
// in, the start a structure type grows there, the starts and references a
// chunk saves, the pieces each decoration step places and the terrain
// adaptation around them. A structure starts only once it has an entry in
// overworldStructureTypes; one without still takes its turn in its set's
// weighted pick and fails it.

// StructureFinalizeKey marks generated mobs that must finalize once when
// loaded: those of a generator with no StructureMobFinalizer.
const StructureFinalizeKey = "golem:finalize_structure"

// StructureMobFinalizer finalizes a structure mob's saved tag in place, in the
// biome at its block, on rng, the WorldGenRegion's random, and returns the
// compound to place: the tag or a vehicle carrying it. blackCat is set for
// the swamp hut's cat. vanilla: Mob.finalizeSpawn(STRUCTURE)
type StructureMobFinalizer func(tag nbt.Compound, biome registry.Biome, blackCat bool, rng levelgen.RandomSource) nbt.Compound

// SetStructureMobFinalizer makes the generator finalize structure mobs as it
// places them; without one they are marked with StructureFinalizeKey.
func (g *OverworldGenerator) SetStructureMobFinalizer(f StructureMobFinalizer) { g.mobFinalizer = f }

// finalizeStructureMob finalizes tag at its block x, y, z when the generator
// has a finalizer, and otherwise marks it to finalize on load. It returns
// the compound to place.
func (r *decorRegion) finalizeStructureMob(tag nbt.Compound, x, y, z int32, blackCat bool) nbt.Compound {
	g, ok := r.src.(*OverworldGenerator)
	if !ok || g.mobFinalizer == nil {
		tag[StructureFinalizeKey] = nbt.Byte(1)
		if id, _ := nbt.Get[nbt.String](tag, "id"); id == "minecraft:villager" {
			// vanilla: Villager.finalizeSpawn(STRUCTURE)
			tag["AssignProfessionWhenSpawned"] = nbt.Byte(1)
		}
		return tag
	}
	return g.mobFinalizer(tag, registry.Biome(g.biomeIDs[g.biomeIndex(r, x, y, z)]), blackCat, r.regionRandom())
}

// spawnStructureMob queues a persistent mob, finalized as it is placed.
// Vanilla draws UUIDs from an unseeded random; a seed/position key keeps regeneration deterministic.
func (r *decorRegion) spawnStructureMob(id, uuidKey string, x, y, z int32, blackCat bool) {
	tag := nbt.Compound{"id": nbt.String(id), "PersistenceRequired": nbt.Byte(1),
		"UUID": nbt.UUIDToIntArray(uuid.NewSHA1(uuid.NameSpaceOID, []byte(uuidKey))),
		"Pos":  nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{nbt.Double(float64(x) + 0.5), nbt.Double(y), nbt.Double(float64(z) + 0.5)}}}
	tag = r.finalizeStructureMob(tag, x, y, z, blackCat)
	proto := r.protoAt(x, z)
	proto.entities = append(proto.entities, protoEntity{x: x & 15, y: y - r.minY, z: z & 15, tag: tag})
}

// structureReach is how far, in chunks, a start can be referenced from.
// vanilla: ChunkGenerator.createReferences
const structureReach = 8

// overworldStructureTypes are the structure types the overworld grows, by
// structure id.
var overworldStructureTypes = map[string]structureType{}

// structureType grows one kind of structure. vanilla: Structure
type structureType interface {
	// generationPoint returns the block whose biome the start needs and a
	// builder of its pieces, or false where the structure does not start.
	// vanilla: Structure.findGenerationPoint
	generationPoint(ctx *structureContext) (pos [3]int32, pieces func() []structurePiece, ok bool)
}

// structureAfterPlacer is a structure type that touches the chunk again once
// its pieces are placed. vanilla: Structure.afterPlace
type structureAfterPlacer interface {
	afterPlace(r *decorRegion, chunk jbox, s *structureStart)
}

// structureContext is what a structure type reads while it finds its start.
// vanilla: Structure.GenerationContext
type structureContext struct {
	g              *OverworldGenerator
	chunkX, chunkZ int32
	// rng is seeded with the chunk's large-feature seed.
	// vanilla: Structure.GenerationContext.makeRandom
	rng *levelgen.LegacyRandomSource
}

// structurePiece is one piece of a start. vanilla: StructurePiece
type structurePiece interface {
	bounds() jbox
	// nbt is the piece as structures.starts saves it.
	nbt() nbt.Compound
	// place writes the piece's blocks inside chunk; ref is the centre of the
	// start's first piece at its bottom. vanilla: StructurePiece.postProcess
	place(r *decorRegion, chunk jbox, ref [3]int32)
}

// structureBeardPiece is a piece that bends the terrain otherwise than a
// rigid box: a pool element, with its ground delta and junctions.
type structureBeardPiece interface {
	beard() levelgen.BeardPiece
}

// structureStart is a structure grown in its start chunk.
// vanilla: StructureStart
type structureStart struct {
	def            *levelgen.StructureDef
	typ            structureType
	chunkX, chunkZ int32
	pieces         []structurePiece
	// box holds the pieces, grown by 12 blocks for a structure that adapts
	// the terrain. vanilla: StructureStart.getBoundingBox
	box jbox
}

// noStartsCacheMax is how many chunks without a start are remembered before
// the memo is cleared and fills again.
const noStartsCacheMax = 1 << 16

// overworldStructures holds the sets the overworld places and the starts
// found so far, the same for every chunk reading them.
type overworldStructures struct {
	types     map[string]structureType
	readStart func(string, int32, int32) nbt.Compound
	saved     map[[2]int32]nbt.Compound
	// current is the sets for one packGeneration, found on first use.
	current atomic.Pointer[structureSetList]
	mu      sync.Mutex
	// starts holds the chunks that have a start.
	starts map[[2]int32][]*structureStart
	// noStarts holds chunks asked about that grew none, at most
	// noStartsCacheMax of them.
	noStarts map[[2]int32]struct{}
}

// typeOf returns the type that grows structure id, a data pack's included.
func (s *overworldStructures) typeOf(id string) structureType {
	if t := s.types[id]; t != nil {
		return t
	}
	if p := packJigsaw.Load(); p != nil {
		return p.types[id]
	}
	return nil
}

// structureSetList is the sets found for one packGeneration.
type structureSetList struct {
	gen  uint64
	sets []*levelgen.StructureSet
}

func newOverworldStructures() *overworldStructures {
	return &overworldStructures{types: overworldStructureTypes, starts: map[[2]int32][]*structureStart{}, noStarts: map[[2]int32]struct{}{}}
}

// structureSets are the sets the overworld can place: a structure of the set
// has a type and a biome the overworld has. A flat world offers only its
// preset's sets.
// vanilla: ChunkGeneratorStructureState.createForNormal, ChunkGeneratorStructureState.createForFlat
func (g *OverworldGenerator) structureSets() []*levelgen.StructureSet {
	s := g.structures
	gen := packGeneration.Load()
	if cur := s.current.Load(); cur != nil && cur.gen == gen {
		return cur.sets
	}
	biomes := g.possibleBiomes()
	all := levelgen.AllStructureSets()
	var sets []*levelgen.StructureSet
	for i := range all {
		set := &all[i]
		if g.flat != nil && !g.flat.allStructureSets && !slices.Contains(g.flat.structureSets, set.Name) {
			continue
		}
		if slices.ContainsFunc(set.Entries, func(e levelgen.StructureSetEntry) bool {
			if s.typeOf(e.Structure) == nil {
				return false
			}
			def := levelgen.StructureNamed(e.Structure)
			return slices.ContainsFunc(biomes, func(b registry.Biome) bool { return structureAllowsBiome(def, b) })
		}) {
			sets = append(sets, set)
		}
	}
	// Starts found under the previous packs' sets no longer hold.
	s.mu.Lock()
	clear(s.starts)
	clear(s.noStarts)
	s.mu.Unlock()
	s.current.Store(&structureSetList{gen, sets})
	return sets
}

// inStrongholdRing reports whether chunk (x, z) is one of the stronghold
// rings' chunks. It resolves only the starts within ringMoveBlocks of the
// chunk, since no other start can move there.
// vanilla: ConcentricRingsStructurePlacement.isPlacementChunk
func (g *OverworldGenerator) inStrongholdRing(x, z int32) bool {
	r := g.rings
	r.mu.Lock()
	defer r.mu.Unlock()
	g.layOut(r)
	const reach = ringMoveBlocks / 16
	var near []int
	for i, s := range r.starts {
		if abs32(s.X-x) <= reach && abs32(s.Z-z) <= reach {
			near = append(near, i)
		}
	}
	if len(near) > 0 {
		g.resolve(r, near)
	}
	// An unresolved entry of r.chunks is {0, 0}, so only near starts count.
	return slices.ContainsFunc(near, func(i int) bool { return r.chunks[i] == [2]int32{x, z} })
}

// structureStartsIn returns the starts in chunk (cx, cz), growing them on
// first use. vanilla: ChunkGenerator.createStructures
func (g *OverworldGenerator) structureStartsIn(cx, cz int32) []*structureStart {
	if g.noStructures {
		return nil
	}
	sets := g.structureSets()
	if len(sets) == 0 {
		return nil
	}
	s := g.structures
	key := [2]int32{cx, cz}
	s.mu.Lock()
	starts, ok := s.starts[key]
	_, none := s.noStarts[key]
	s.mu.Unlock()
	if ok || none {
		return starts
	}
	for _, set := range sets {
		if !set.IsStructureChunk(g.seed, cx, cz, g.inStrongholdRing) {
			continue
		}
		set.TryEntries(g.seed, cx, cz, func(e levelgen.StructureSetEntry) bool {
			start := g.growStructure(e.Structure, cx, cz)
			if start != nil {
				starts = append(starts, start)
			}
			return start != nil
		})
	}
	// Starts grow outside the lock, so two workers may grow the same chunk;
	// the first stored wins.
	s.mu.Lock()
	defer s.mu.Unlock()
	if prev, ok := s.starts[key]; ok {
		return prev
	}
	if len(starts) > 0 {
		s.starts[key] = starts
		return starts
	}
	// A chunk without a start is remembered too, since the terrain and the
	// placement of every chunk around it ask again.
	if len(s.noStarts) >= noStartsCacheMax {
		clear(s.noStarts)
	}
	s.noStarts[key] = struct{}{}
	return starts
}

// growStructure grows the structure in chunk (cx, cz), or returns nil when it
// has no type, finds no point, its point's biome refuses it or it has no
// pieces. vanilla: Structure.generate
func (g *OverworldGenerator) growStructure(id string, cx, cz int32) *structureStart {
	typ := g.structures.typeOf(id)
	if typ == nil {
		return nil
	}
	if loader, ok := typ.(structureStartLoader); ok {
		s := g.structures
		s.mu.Lock()
		tag, _ := nbt.Get[nbt.Compound](s.saved[[2]int32{cx, cz}], id)
		s.mu.Unlock()
		if tag == nil && s.readStart != nil {
			tag = s.readStart(id, cx, cz)
		}
		if tag != nil {
			if start := g.loadStructureStart(loader, tag); start != nil {
				return start
			}
		}
	}
	return g.buildStart(id, typ, cx, cz, true)
}

// buildStart builds the start of structure id in chunk (cx, cz); checkBiome
// is false where the caller accepts any biome.
// vanilla: Structure.generate
func (g *OverworldGenerator) buildStart(id string, typ structureType, cx, cz int32, checkBiome bool) *structureStart {
	ctx := &structureContext{g: g, chunkX: cx, chunkZ: cz, rng: levelgen.NewLegacyRandom(0)}
	ctx.rng.SetLargeFeatureSeed(g.seed, cx, cz)
	pos, build, ok := typ.generationPoint(ctx)
	if !ok {
		return nil
	}
	def := levelgen.StructureNamed(id)
	// vanilla: Structure.isValidBiome
	if biome := g.biomeSource.At(pos[0]>>2, pos[1]>>2, pos[2]>>2); checkBiome && !structureAllowsBiome(def, registry.Biome(g.biomeIDs[biome])) {
		return nil
	}
	pieces := build()
	if len(pieces) == 0 {
		return nil
	}
	box := pieces[0].bounds()
	for _, p := range pieces[1:] {
		box = box.union(p.bounds())
	}
	if def.Adaptation != levelgen.AdjustNone {
		box = jbox{box.minX - 12, box.minY - 12, box.minZ - 12, box.maxX + 12, box.maxY + 12, box.maxZ + 12}
	}
	return &structureStart{def: def, typ: typ, chunkX: cx, chunkZ: cz, pieces: pieces, box: box}
}

// eachStructureReaching calls fn with each start within structureReach
// chunks whose box meets chunk (cx, cz)'s columns, x outer.
// vanilla: ChunkGenerator.createReferences
func (g *OverworldGenerator) eachStructureReaching(cx, cz int32, fn func(*structureStart)) {
	if g.noStructures || len(g.structureSets()) == 0 {
		return
	}
	minX, minZ := cx*16, cz*16
	for sx := cx - structureReach; sx <= cx+structureReach; sx++ {
		for sz := cz - structureReach; sz <= cz+structureReach; sz++ {
			for _, s := range g.structureStartsIn(sx, sz) {
				b := s.box
				if b.maxX >= minX && b.minX <= minX+15 && b.maxZ >= minZ && b.minZ <= minZ+15 {
					fn(s)
				}
			}
		}
	}
}

// saveStructures records the starts in the chunk and the ones that reach
// into it in the chunk's structures tag.
// vanilla: ChunkStatus.STRUCTURE_STARTS, STRUCTURE_REFERENCES
func (g *OverworldGenerator) saveStructures(c *Chunk) {
	for _, s := range g.structureStartsIn(c.x, c.z) {
		if _, ok := s.typ.(structureStartLoader); ok {
			c.setLiveStructureStart(s.def.Name, s.nbt)
		} else {
			c.setStructureStart(s.def.Name, s.nbt())
		}
	}
	refs := map[string][]int64{}
	g.eachStructureReaching(c.x, c.z, func(s *structureStart) {
		refs[s.def.Name] = append(refs[s.def.Name], packChunk(s.chunkX, s.chunkZ))
	})
	for id, r := range refs {
		c.setStructureReferences(id, r)
	}
}

// placeStructures writes, before step's features, the pieces of the step's
// structures that reach chunk (nx, nz), each structure reseeding the
// placement random with its index in the step.
// vanilla: ChunkGenerator.applyBiomeDecoration, StructureStart.placeInChunk
func (r *decorRegion) placeStructures(reaching []*structureStart, decorationSeed int64, step int32, nx, nz int32) {
	if len(reaching) == 0 {
		return
	}
	// The writable area leaves out the lowest layer.
	// vanilla: ChunkGenerator.getWritableArea
	chunk := jbox{nx * 16, r.minY + 1, nz * 16, nx*16 + 15, r.minY + r.height - 1, nz*16 + 15}
	all := levelgen.AllStructures()
	for i := range all {
		def := &all[i]
		if def.Step != step {
			continue
		}
		r.rng.SetFeatureSeed(decorationSeed, def.Index, step)
		var starts []*structureStart
		for _, s := range reaching {
			if s.def == def {
				starts = append(starts, s)
			}
		}
		// The starts share the random, so their order decides its draws.
		for _, s := range referenceOrder(starts) {
			first := s.pieces[0].bounds()
			// BoundingBox.getCenter, at the box's bottom.
			ref := [3]int32{first.minX + (first.maxX-first.minX+1)/2, first.minY, first.minZ + (first.maxZ-first.minZ+1)/2}
			for _, p := range s.pieces {
				if p.bounds().intersects(chunk) {
					p.place(r, chunk, ref)
				}
			}
			if a, ok := s.typ.(structureAfterPlacer); ok {
				a.afterPlace(r, chunk, s)
			}
		}
	}
}

// referenceOrder orders one structure's starts, given in the order
// createReferences adds them, as a default fastutil LongOpenHashSet of their
// packed chunk positions iterates: the zero key first, then slots from the
// top down. vanilla: StructureManager.fillStartsForStructure
func referenceOrder(starts []*structureStart) []*structureStart {
	if len(starts) < 2 {
		return starts
	}
	// new LongOpenHashSet(): 32 slots, rehashed past 24 keys.
	n, size := 32, 0
	table := make([]*structureStart, n)
	var zero *structureStart
	put := func(t []*structureStart, s *structureStart) {
		mask := len(t) - 1
		pos := int(fastutilMix(packChunk(s.chunkX, s.chunkZ))) & mask
		for t[pos] != nil {
			pos = (pos + 1) & mask
		}
		t[pos] = s
	}
	for _, s := range starts {
		if packChunk(s.chunkX, s.chunkZ) == 0 {
			zero = s
		} else {
			put(table, s)
		}
		// HashCommon.maxFill, then rehash(arraySize(size+1, 0.75)) walking the
		// old slots from the top.
		if size++; size > min(n*3/4, n-1) {
			n *= 2
			grown := make([]*structureStart, n)
			for _, t := range slices.Backward(table) {
				if t != nil {
					put(grown, t)
				}
			}
			table = grown
		}
	}
	out := make([]*structureStart, 0, len(starts))
	if zero != nil {
		out = append(out, zero)
	}
	for _, t := range slices.Backward(table) {
		if t != nil {
			out = append(out, t)
		}
	}
	return out
}

// fastutilMix is fastutil's HashCommon.mix.
func fastutilMix(x int64) int64 {
	h := uint64(x) * 0x9E3779B97F4A7C15
	h ^= h >> 32
	return int64(h ^ h>>16)
}

// beardifierAt is the terrain adaptation of chunk (cx, cz) to the starts
// that reach it. vanilla: Beardifier.forStructuresInChunk
func (g *OverworldGenerator) beardifierAt(cx, cz int32) *terrain.Beardifier {
	var pieces []levelgen.BeardPiece
	g.eachStructureReaching(cx, cz, func(s *structureStart) {
		for _, p := range s.pieces {
			bp := levelgen.BeardPiece{Rigid: true}
			if b, ok := p.(structureBeardPiece); ok {
				bp = b.beard()
			}
			box := p.bounds()
			bp.Box = levelgen.BoundingBox{MinX: int(box.minX), MinY: int(box.minY), MinZ: int(box.minZ), MaxX: int(box.maxX), MaxY: int(box.maxY), MaxZ: int(box.maxZ)}
			bp.Adjustment = s.def.Adaptation
			pieces = append(pieces, bp)
		}
	})
	return terrain.BeardifierForChunk(cx, cz, pieces)
}

// packChunk is a chunk position as a long. vanilla: ChunkPos.pack
func packChunk(x, z int32) int64 { return int64(uint32(x)) | int64(z)<<32 }

// structureStartNBT is a start as a chunk saves it under structures.starts.
// vanilla: StructureStart.createTag
func structureStartNBT(id string, chunkX, chunkZ int32, children []nbt.Tag) nbt.Compound {
	return nbt.Compound{
		"id":         nbt.String(id),
		"ChunkX":     nbt.Int(chunkX),
		"ChunkZ":     nbt.Int(chunkZ),
		"references": nbt.Int(0),
		"Children":   nbt.List{Elem: nbt.TypeCompound, Items: children},
	}
}
