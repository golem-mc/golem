package world

//go:generate go run ../../../tools/gen igloo

import (
	_ "embed"
	"fmt"
	"math"
	"sync"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

//go:embed igloo.zip
var iglooArchive []byte

var iglooTemplates = sync.OnceValue(func() map[string]*Template { return templateArchive(iglooArchive) })

var iglooPivots = map[string][3]int32{"top": {3, 5, 5}, "middle": {1, 3, 1}, "bottom": {3, 6, 7}}
var iglooOffsets = map[string][3]int32{"top": {}, "middle": {2, -3, 4}, "bottom": {0, -3, -2}}

func init() { overworldStructureTypes["minecraft:igloo"] = iglooStructure{} }

type iglooStructure struct{}

// vanilla: IglooStructure.findGenerationPoint, onTopOfChunkCenter
func (iglooStructure) generationPoint(ctx *structureContext) ([3]int32, func() []structurePiece, bool) {
	x, z := ctx.chunkX*16, ctx.chunkZ*16
	return [3]int32{x + 8, ctx.g.firstFreeHeight(x+8, z+8) - 1, z + 8}, func() []structurePiece {
		rot := Rotation(ctx.rng.NextIntN(4))
		pos := [3]int32{x, 90, z}
		var pieces []structurePiece
		if ctx.rng.NextDouble() < 0.5 {
			depth := ctx.rng.NextIntN(8) + 4
			pieces = append(pieces, newIglooPiece("bottom", pos, rot, depth*3))
			for i := int32(0); i < depth-1; i++ {
				pieces = append(pieces, newIglooPiece("middle", pos, rot, i*3))
			}
		}
		return append(pieces, newIglooPiece("top", pos, rot, 0))
	}, true
}

// vanilla: IglooPieces.IglooPiece
type iglooPiece struct {
	mu       sync.Mutex
	name     string
	template *Template
	pos      [3]int32
	rotation Rotation
	box      jbox
}

func newIglooPiece(name string, pos [3]int32, rotation Rotation, depth int32) *iglooPiece {
	p := &iglooPiece{name: name, template: iglooTemplates()[name], rotation: rotation}
	p.pos = [3]int32{pos[0] + iglooOffsets[name][0], pos[1] + iglooOffsets[name][1] - depth, pos[2] + iglooOffsets[name][2]}
	p.box = p.templateBoxAt(p.pos)
	return p
}

// pivotOffset turns placement around the template's saved pivot into the
// corner-relative transform the shared Template.Place uses.
func (p *iglooPiece) pivotOffset() (int32, int32) {
	pivot := iglooPivots[p.name]
	x, _, z := TransformTemplatePos(pivot[0], pivot[1], pivot[2], MirrorNone, p.rotation)
	return pivot[0] - x, pivot[2] - z
}

func (p *iglooPiece) templateBoxAt(pos [3]int32) jbox {
	dx, dz := p.pivotOffset()
	x, _, z := TransformTemplatePos(p.template.Size[0]-1, 0, p.template.Size[2]-1, MirrorNone, p.rotation)
	ox, oz := pos[0]+dx, pos[2]+dz
	return jbox{ox + min(0, x), pos[1], oz + min(0, z), ox + max(0, x), pos[1] + p.template.Size[1] - 1, oz + max(0, z)}
}

func (p *iglooPiece) bounds() jbox { p.mu.Lock(); defer p.mu.Unlock(); return p.box }

func (p *iglooPiece) nbt() nbt.Compound {
	p.mu.Lock()
	defer p.mu.Unlock()
	return nbt.Compound{"id": nbt.String("minecraft:iglu"), "GD": nbt.Int(0), "O": nbt.Int(2),
		"BB":       nbt.IntArray{p.box.minX, p.box.minY, p.box.minZ, p.box.maxX, p.box.maxY, p.box.maxZ},
		"Template": nbt.String("minecraft:igloo/" + p.name), "TPX": nbt.Int(p.pos[0]), "TPY": nbt.Int(p.pos[1]), "TPZ": nbt.Int(p.pos[2]),
		"Rot": nbt.String(rotationNames[p.rotation])}
}

// vanilla: IglooPieces.IglooPiece.postProcess, TemplateStructurePiece.postProcess
func (p *iglooPiece) place(r *decorRegion, chunk jbox, _ [3]int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	offset := iglooOffsets[p.name]
	pivot := iglooPivots[p.name]
	lx, _, lz := TransformTemplatePos(3-offset[0], 0, -offset[2], MirrorNone, p.rotation)
	dx, dz := p.pivotOffset()
	entranceX := p.pos[0] + lx + dx
	entranceZ := p.pos[2] + lz + dz
	effective := [3]int32{p.pos[0], p.pos[1] + r.heightOf(hmWorldSurfaceWG, entranceX, entranceZ) - 91, p.pos[2]}
	p.box = p.templateBoxAt(effective)
	origin := [3]int32{effective[0] + dx, effective[1], effective[2] + dz}
	placed, _ := p.template.Place(TemplatePlacement{
		Origin:    origin,
		Rotation:  p.rotation,
		Integrity: 1,
		Seed:      1,
	})
	var markers []TemplateBlock
	var shapes []posKey
	for i, b := range placed.Changes {
		if !chunk.inside(b.X, b.Y, b.Z) {
			continue
		}
		if registry.BlockOf(b.State) == registry.BlockStructureBlock {
			markers = append(markers, TemplateBlock{Pos: [3]int32{int32(b.X), int32(b.Y), int32(b.Z)}, NBT: placed.Tags[i]})
			continue
		}
		r.set(int32(b.X), int32(b.Y), int32(b.Z), b.State)
		r.markShape(int32(b.X), int32(b.Y), int32(b.Z))
		shapes = append(shapes, posKey{int32(b.X), int32(b.Y), int32(b.Z)})
		if placed.Tags[i] != nil {
			r.placeBlockEntity(int32(b.X), int32(b.Y), int32(b.Z), b.State, placed.Tags[i])
		}
	}
	r.templateShapes(shapes)
	for _, marker := range markers {
		if metadata, _ := nbt.Get[nbt.String](marker.NBT, "metadata"); metadata != "chest" {
			continue
		}
		r.set(marker.Pos[0], marker.Pos[1], marker.Pos[2], airState)
		r.markShape(marker.Pos[0], marker.Pos[1], marker.Pos[2])
		x, y, z := marker.Pos[0], marker.Pos[1]-1, marker.Pos[2]
		if chunk.inside(x, y, z) && registry.BlockOf(r.get(x, y, z)) == registry.BlockChest {
			r.placeBlockEntity(x, y, z, r.get(x, y, z), nbt.Compound{"id": nbt.String("minecraft:chest"),
				"LootTable": nbt.String("minecraft:chests/igloo_chest")})
		}
	}
	for i, entity := range p.template.Entities {
		placedEntity, ok := entity.place(TemplatePlacement{Origin: origin, Rotation: p.rotation})
		if !ok {
			continue
		}
		x := int32(math.Floor(placedEntity.Pos[0]))
		y := int32(math.Floor(placedEntity.Pos[1]))
		z := int32(math.Floor(placedEntity.Pos[2]))
		if !chunk.inside(x, y, z) {
			continue
		}
		key := fmt.Appendf(nil, "igloo/%d/%s/%v/%d/%d", r.worldSeed, p.name, p.pos, p.rotation, i)
		placedEntity.Tag["UUID"] = nbt.UUIDToIntArray(uuid.NewSHA1(uuid.NameSpaceOID, key))
		proto := r.protoAt(x, z)
		proto.entities = append(proto.entities, protoEntity{x: x & 15, y: y - r.minY, z: z & 15, tag: placedEntity.Tag})
	}
	if p.name != "top" {
		return
	}
	tlx, _, tlz := TransformTemplatePos(pivot[0], 0, pivot[2], MirrorNone, p.rotation)
	x, y, z := origin[0]+tlx, origin[1], origin[2]+tlz
	if !chunk.inside(x, y, z) {
		return
	}
	below := r.get(x, y-1, z)
	if !r.src.isAir(below) && registry.BlockOf(below) != registry.BlockLadder {
		r.set(x, y, z, registry.BlockSnowBlock.DefaultState())
		r.markShape(x, y, z)
	}
}
