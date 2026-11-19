package world

//go:generate go run ../../../tools/gen netherfossil

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/internal/server/world/terrain"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The nether's fossils: one bone template picked at a random spot of a soul
// sand valley, which the terrain shapes itself around (beard_thin) and the
// underground_decoration step writes.

// netherFossilArchive holds the structure, its set and its templates under
// their data pack paths (tools/gen netherfossil).
//
//go:embed netherfossil.zip
var netherFossilArchive []byte

const (
	// netherFossilReach is how far, in chunks, a start's references reach.
	// vanilla: ChunkGenerator.createReferences
	netherFossilReach = 8
	netherFossilID    = "minecraft:nether_fossil"
	netherFossilPiece = "minecraft:nefos"
	// fossilTemplates is the length of NetherFossilPieces.FOSSILS.
	fossilTemplates = 14
)

// netherFossilData is what the archive says about the fossil.
type netherFossilData struct {
	templates                 [fossilTemplates]*Template
	spacing, separation, salt int32
	// minY is the lowest start height; belowTop is the offset of the highest
	// one under the top of the generation range.
	minY, belowTop int32
}

var (
	fossilOnce sync.Once
	fossilData *netherFossilData
)

// netherFossils returns the archive's data; it panics when the embedded data
// cannot be read, which no input can cause.
func netherFossils() *netherFossilData {
	fossilOnce.Do(func() {
		d, err := loadNetherFossils(netherFossilArchive)
		if err != nil {
			panic("nether fossil data: " + err.Error())
		}
		fossilData = d
	})
	return fossilData
}

func loadNetherFossils(archive []byte) (*netherFossilData, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	read := func(name string) ([]byte, error) {
		f, err := zr.Open(name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(f)
	}
	d := &netherFossilData{}
	var set struct {
		Placement struct{ Salt, Separation, Spacing int32 } `json:"placement"`
	}
	var structure struct {
		Height struct {
			Max struct {
				BelowTop int32 `json:"below_top"`
			} `json:"max_inclusive"`
			Min struct {
				Absolute int32 `json:"absolute"`
			} `json:"min_inclusive"`
		} `json:"height"`
	}
	for name, v := range map[string]any{
		"minecraft/worldgen/structure_set/nether_fossils.json": &set,
		"minecraft/worldgen/structure/nether_fossil.json":      &structure,
	} {
		b, err := read(name)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, v); err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
	}
	d.spacing, d.separation, d.salt = set.Placement.Spacing, set.Placement.Separation, set.Placement.Salt
	d.minY, d.belowTop = structure.Height.Min.Absolute, structure.Height.Max.BelowTop
	for i := range d.templates {
		b, err := read("minecraft/structure/nether_fossils/fossil_" + strconv.Itoa(i+1) + ".nbt")
		if err != nil {
			return nil, err
		}
		if d.templates[i], err = ParseTemplate(b); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// fossilStart is a fossil's start: where a chunk's random put it and, once
// resolved, whether the terrain has a place for it.
type fossilStart struct {
	chunkX, chunkZ int32
	// x and z are the start column; y0 is the height the column search starts
	// from.
	x, z, y0 int32
	rotation Rotation
	template int
	// xz is the piece's box in x and z, which does not depend on the height.
	xz jbox

	once  sync.Once
	valid bool
	// pos is the template's origin; box is the piece's box.
	pos [3]int32
	box jbox
}

// netherFossil holds the fossils drawn so far, the same for every chunk
// reading them.
type netherFossil struct {
	mu     sync.Mutex
	starts map[[2]int32]*fossilStart
}

func (g *NetherGenerator) initFossil() {
	g.fossil.starts = map[[2]int32]*fossilStart{}
}

// fossilInCell returns the fossil drawn in a nether_fossils cell, or nil when
// structures are off.
// vanilla: ChunkGenerator.createStructures, NetherFossilStructure.findGenerationPoint
func (g *NetherGenerator) fossilInCell(gx, gz int32) *fossilStart {
	if g.noStructures {
		return nil
	}
	f := &g.fossil
	f.mu.Lock()
	defer f.mu.Unlock()
	key := [2]int32{gx, gz}
	if s, ok := f.starts[key]; ok {
		return s
	}
	d := netherFossils()
	cx, cz := levelgen.RandomSpreadChunk(g.seed, gx*d.spacing, gz*d.spacing, d.spacing, d.separation, d.salt)
	rng := levelgen.NewLegacyRandom(0)
	rng.SetLargeFeatureSeed(g.seed, cx, cz)
	s := &fossilStart{chunkX: cx, chunkZ: cz}
	s.x = cx*sectionSize + rng.NextIntN(16)
	s.z = cz*sectionSize + rng.NextIntN(16)
	top := int32(netherMaxGenY) - d.belowTop
	s.y0 = d.minY + rng.NextIntN(top-d.minY+1)
	s.rotation = Rotation(rng.NextIntN(4))
	s.template = int(rng.NextIntN(fossilTemplates))
	b := fossilBox(d.templates[s.template], [3]int32{s.x, 0, s.z}, s.rotation)
	s.xz = jbox{b.minX, 0, b.minZ, b.maxX, 0, b.maxZ}
	f.starts[key] = s
	return s
}

// fossilBox is the template's box at pos turned by rotation.
// vanilla: StructureTemplate.getBoundingBox
func fossilBox(t *Template, pos [3]int32, r Rotation) jbox {
	x0, _, z0 := TransformTemplatePos(0, 0, 0, MirrorNone, r)
	x1, _, z1 := TransformTemplatePos(t.Size[0]-1, t.Size[1]-1, t.Size[2]-1, MirrorNone, r)
	return jbox{
		min(x0, x1) + pos[0], pos[1], min(z0, z1) + pos[2],
		max(x0, x1) + pos[0], pos[1] + t.Size[1] - 1, max(z0, z1) + pos[2],
	}
}

// reaches reports whether the start's box, grown by the terrain adaptation's
// 12 blocks, meets the x and z span of chunk (cx, cz): the chunks that hold a
// reference to it.
// vanilla: ChunkGenerator.createReferences, Structure.adjustBoundingBox
func (s *fossilStart) reaches(cx, cz int32) bool {
	const grow = 12
	return absInt32(s.chunkX-cx) <= netherFossilReach && absInt32(s.chunkZ-cz) <= netherFossilReach &&
		s.xz.minX-grow <= cx*sectionSize+15 && s.xz.maxX+grow >= cx*sectionSize &&
		s.xz.minZ-grow <= cz*sectionSize+15 && s.xz.maxZ+grow >= cz*sectionSize
}

// resolved finds the fossil's height and checks its biome; it reports whether
// the fossil exists.
// vanilla: NetherFossilStructure.findGenerationPoint, Structure.isValidBiome
func (g *NetherGenerator) resolved(s *fossilStart) bool {
	s.once.Do(func() {
		// The base column holds netherrack where the density is above 0, lava
		// below the sea level and air above it, so soul sand never shows in it.
		nc := terrain.NewNoiseChunk(g.state, nil, terrain.NewVolume(1, netherTop, 1, s.x, 0, s.z))
		defer nc.Recycle()
		density := nc.SampleDensity()
		defer nc.ReleaseDensity(density)
		solid := func(y int32) bool { return y >= netherBottom && y < netherTop && density[y] > 0 }
		open := func(y int32) bool { return y >= netherSeaLevel && y < netherTop && !solid(y) }
		y := s.y0
		for y > netherSeaLevel {
			current := open(y)
			y--
			if current && solid(y) {
				break
			}
		}
		if y <= netherSeaLevel {
			return
		}
		if terrain.NetherBiomeAt(g.state.Router, s.x>>2, y>>2, s.z>>2) != levelgen.SoulSandValley {
			return
		}
		s.valid = true
		s.pos = [3]int32{s.x, y, s.z}
		s.box = fossilBox(netherFossils().templates[s.template], s.pos, s.rotation)
	})
	return s.valid
}

// eachFossil calls fn for every valid fossil whose references reach chunk
// (cx, cz), in cell order.
func (g *NetherGenerator) eachFossil(cx, cz int32, fn func(*fossilStart)) {
	const reach = netherFossilReach
	spacing := netherFossils().spacing
	for gz := floorDiv(cz-reach, spacing); gz <= floorDiv(cz+reach, spacing); gz++ {
		for gx := floorDiv(cx-reach, spacing); gx <= floorDiv(cx+reach, spacing); gx++ {
			if s := g.fossilInCell(gx, gz); s != nil && s.reaches(cx, cz) && g.resolved(s) {
				fn(s)
			}
		}
	}
}

// fossilBeardifier is the beard of the fossils that reach the chunk, nil for
// none.
// vanilla: Beardifier.forStructuresInChunk
func (g *NetherGenerator) fossilBeardifier(cx, cz int32) *terrain.Beardifier {
	var pieces []levelgen.BeardRigid
	g.eachFossil(cx, cz, func(s *fossilStart) {
		b := s.box
		pieces = append(pieces, levelgen.BeardRigid{
			Box:        levelgen.BoundingBox{MinX: int(b.minX), MinY: int(b.minY), MinZ: int(b.minZ), MaxX: int(b.maxX), MaxY: int(b.maxY), MaxZ: int(b.maxZ)},
			Adjustment: levelgen.AdjustBeardThin,
		})
	})
	return terrain.NewBeardifier(pieces, nil)
}

// placeFossils writes the fossils that reach chunk (nx, nz) and meet it, each
// whole, then its dried ghast. They go after the fortress, which shares their
// step and comes first in the registry. Each fossil meets the chunk box alone.
// Overlapping fossils go in cell order; vanilla iterates a set.
// vanilla: ChunkGenerator.applyBiomeDecoration, StructureStart.placeInChunk,
// NetherFossilPieces.NetherFossilPiece.postProcess
func (r *decorRegion) placeFossils(nx, nz int32) {
	r.g.eachFossil(nx, nz, func(s *fossilStart) {
		chunk := jbox{nx * 16, 1, nz * 16, nx*16 + 15, netherGenHeight - 1, nz*16 + 15}
		if !s.box.intersects(chunk) {
			return
		}
		t := netherFossils().templates[s.template]
		for _, blk := range t.Palettes[0] {
			// vanilla: BlockIgnoreProcessor.STRUCTURE_AND_AIR
			switch registry.BlockOf(blk.State) {
			case registry.BlockAir, registry.BlockStructureBlock:
				continue
			}
			tx, ty, tz := TransformTemplatePos(blk.Pos[0], blk.Pos[1], blk.Pos[2], MirrorNone, s.rotation)
			r.set(tx+s.pos[0], ty+s.pos[1], tz+s.pos[2], RotateState(blk.State, s.rotation))
		}
		r.placeDriedGhast(s)
	})
}

// placeDriedGhast puts a dried ghast on the fossil's lowest layer half the
// time, in a column the position picks.
// vanilla: NetherFossilPieces.NetherFossilPiece.placeDriedGhast
func (r *decorRegion) placeDriedGhast(s *fossilStart) {
	b := s.box
	centre := [3]int32{b.minX + (b.maxX-b.minX+1)/2, b.minY + (b.maxY-b.minY+1)/2, b.minZ + (b.maxZ-b.minZ+1)/2}
	rng := levelgen.NewLegacyRandom(r.g.seed).ForkPositional().At(centre[0], centre[1], centre[2])
	if rng.NextFloat() >= 0.5 {
		return
	}
	x := b.minX + rng.NextIntN(b.maxX-b.minX+1)
	z := b.minZ + rng.NextIntN(b.maxZ-b.minZ+1)
	if !r.isAir(x, b.minY, z) {
		return
	}
	r.set(x, b.minY, z, RotateState(registry.BlockDriedGhast.DefaultState(), Rotation(rng.NextIntN(4))))
}

// nbt is the start as a chunk saves it under structures.starts.
// vanilla: StructureStart.createTag, TemplateStructurePiece.addAdditionalSaveData
func (s *fossilStart) nbt() nbt.Compound {
	piece := nbt.Compound{
		"id":       nbt.String(netherFossilPiece),
		"BB":       nbt.IntArray{s.box.minX, s.box.minY, s.box.minZ, s.box.maxX, s.box.maxY, s.box.maxZ},
		"GD":       nbt.Int(0),
		"O":        nbt.Int(2),
		"TPX":      nbt.Int(s.pos[0]),
		"TPY":      nbt.Int(s.pos[1]),
		"TPZ":      nbt.Int(s.pos[2]),
		"Template": nbt.String("minecraft:nether_fossils/fossil_" + strconv.Itoa(s.template+1)),
		"Rot":      nbt.String(rotationNames[s.rotation]),
	}
	return nbt.Compound{
		"id":         nbt.String(netherFossilID),
		"ChunkX":     nbt.Int(s.chunkX),
		"ChunkZ":     nbt.Int(s.chunkZ),
		"references": nbt.Int(0),
		"Children":   nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{piece}},
	}
}

// saveFossils records the fossil that starts in the chunk, and the ones that
// reach into it, in the chunk's structures tag.
// vanilla: ChunkStatus.STRUCTURE_STARTS, STRUCTURE_REFERENCES
func (g *NetherGenerator) saveFossils(c *Chunk) {
	var refs []int64
	g.eachFossil(c.x, c.z, func(s *fossilStart) {
		if s.chunkX == c.x && s.chunkZ == c.z {
			c.setStructureStart(netherFossilID, s.nbt())
		}
		refs = append(refs, int64(uint32(s.chunkX))|int64(s.chunkZ)<<32)
	})
	if len(refs) > 0 {
		c.setStructureReferences(netherFossilID, refs)
	}
}
