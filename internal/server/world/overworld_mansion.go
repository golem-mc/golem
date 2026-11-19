package world

//go:generate go run ../../../tools/gen mansion

import (
	_ "embed"
	"fmt"
	"strings"
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

//go:embed mansion.zip
var mansionArchive []byte

var mansionTemplates = sync.OnceValue(func() map[string]*Template { return templateArchive(mansionArchive) })

func init() { overworldStructureTypes["minecraft:mansion"] = mansionStructure{} }

type mansionStructure struct{}

// vanilla: WoodlandMansionStructure.findGenerationPoint,
// Structure.getLowestYIn5by5BoxOffset7Blocks
func (mansionStructure) generationPoint(ctx *structureContext) ([3]int32, func() []structurePiece, bool) {
	rot := Rotation(ctx.rng.NextIntN(4))
	x, z := ctx.chunkX*16+7, ctx.chunkZ*16+7
	dx, dz := int32(5), int32(5)
	if rot == RotateClockwise90 || rot == Rotate180 {
		dx = -5
	}
	if rot == Rotate180 || rot == RotateCounterclockwise90 {
		dz = -5
	}
	y := min(ctx.g.firstFreeHeight(x, z), ctx.g.firstFreeHeight(x+dx, z), ctx.g.firstFreeHeight(x, z+dz), ctx.g.firstFreeHeight(x+dx, z+dz)) - 1
	pos := [3]int32{x, y, z}
	return pos, func() []structurePiece {
		layout := levelgen.GenerateMansion(ctx.rng, [3]int{int(x), int(y), int(z)}, int(rot))
		out := make([]structurePiece, len(layout))
		for i, p := range layout {
			out[i] = newMansionPiece(p)
		}
		return out
	}, y >= 60
}

type mansionPiece struct {
	levelgen.MansionPiece
	template *Template
	box      jbox
}

func newMansionPiece(p levelgen.MansionPiece) *mansionPiece {
	t := mansionTemplates()[p.Name]
	if t == nil {
		panic("missing mansion template: " + p.Name)
	}
	x0, y0, z0 := TransformTemplatePos(0, 0, 0, Mirror(p.Mirror), Rotation(p.Rotation))
	x1, y1, z1 := TransformTemplatePos(t.Size[0]-1, t.Size[1]-1, t.Size[2]-1, Mirror(p.Mirror), Rotation(p.Rotation))
	x, y, z := int32(p.Pos[0]), int32(p.Pos[1]), int32(p.Pos[2])
	return &mansionPiece{p, t, jbox{x + min(x0, x1), y + min(y0, y1), z + min(z0, z1), x + max(x0, x1), y + max(y0, y1), z + max(z0, z1)}}
}

func (p *mansionPiece) bounds() jbox { return p.box }

// vanilla: TemplateStructurePiece.addAdditionalSaveData,
// WoodlandMansionPiece.addAdditionalSaveData
func (p *mansionPiece) nbt() nbt.Compound {
	return nbt.Compound{
		"id": nbt.String("minecraft:wmp"), "GD": nbt.Int(0), "O": nbt.Int(2),
		"BB":       nbt.IntArray{p.box.minX, p.box.minY, p.box.minZ, p.box.maxX, p.box.maxY, p.box.maxZ},
		"Template": nbt.String(p.Name), "TPX": nbt.Int(p.Pos[0]), "TPY": nbt.Int(p.Pos[1]), "TPZ": nbt.Int(p.Pos[2]),
		"Rot": nbt.String(rotationNames[p.Rotation]), "Mi": nbt.String([3]string{"NONE", "LEFT_RIGHT", "FRONT_BACK"}[p.Mirror]),
	}
}

// vanilla: TemplateStructurePiece.postProcess, StructureTemplate.placeInWorld
func (p *mansionPiece) place(r *decorRegion, chunk jbox, _ [3]int32) {
	var markers []TemplateBlock
	var placed []posKey
	for _, b := range p.template.Palettes[0] {
		x, y, z := TransformTemplatePos(b.Pos[0], b.Pos[1], b.Pos[2], Mirror(p.Mirror), Rotation(p.Rotation))
		x, y, z = x+int32(p.Pos[0]), y+int32(p.Pos[1]), z+int32(p.Pos[2])
		if !chunk.inside(x, y, z) {
			continue
		}
		if registry.BlockOf(b.State) == registry.BlockStructureBlock {
			if mode, _ := nbt.Get[nbt.String](b.NBT, "mode"); mode == "DATA" {
				b.Pos = [3]int32{x, y, z}
				markers = append(markers, b)
			}
			continue
		}
		state, tag, ok := processTemplateBlock(b, x, y, z)
		if !ok {
			continue
		}
		state = RotateState(MirrorState(state, Mirror(p.Mirror)), Rotation(p.Rotation))
		r.set(x, y, z, state)
		placed = append(placed, posKey{x, y, z})
		r.markShape(x, y, z)
		if tag != nil {
			r.placeBlockEntity(x, y, z, state, tag)
		}
	}
	r.templateShapes(placed)
	for _, b := range markers {
		p.marker(r, b)
	}
}

// vanilla: WoodlandMansionPiece.handleDataMarker
func (p *mansionPiece) marker(r *decorRegion, b TemplateBlock) {
	marker, _ := nbt.Get[nbt.String](b.NBT, "metadata")
	x, y, z := b.Pos[0], b.Pos[1], b.Pos[2]
	if strings.HasPrefix(string(marker), "Chest") {
		if registry.BlockOf(r.get(x, y, z)) == registry.BlockChest {
			return
		}
		state := registry.BlockChest.DefaultState()
		dirs := map[nbt.String]string{"ChestWest": "west", "ChestEast": "east", "ChestNorth": "north", "ChestSouth": "south"}
		if d := dirs[marker]; d != "" {
			state, _ = registry.WithProp(state, registry.PropFacing, d)
		}
		state = RotateState(state, Rotation(p.Rotation)) // Marker chests do not mirror their facing.
		r.set(x, y, z, state)
		r.placeBlockEntity(x, y, z, state, nbt.Compound{"id": nbt.String("minecraft:chest"), "LootTable": nbt.String("minecraft:chests/woodland_mansion")})
		return
	}
	id, count := "", 1
	switch marker {
	case "Mage":
		id = "minecraft:evoker"
	case "Warrior":
		id = "minecraft:vindicator"
	case "Group of Allays":
		id, count = "minecraft:allay", int(r.regionRandom().NextIntN(3))+1
	default:
		return
	}
	for i := 0; i < count; i++ {
		r.spawnStructureMob(id, fmt.Sprintf("mansion/%d/%d/%d/%d/%d", r.worldSeed, x, y, z, i), x, y, z, false)
	}
	r.set(x, y, z, airState)
}

// vanilla: WoodlandMansionStructure.afterPlace
func (mansionStructure) afterPlace(r *decorRegion, chunk jbox, s *structureStart) {
	box := s.pieces[0].bounds()
	for _, p := range s.pieces[1:] {
		box = box.union(p.bounds())
	}
	for x := chunk.minX; x <= chunk.maxX; x++ {
		for z := chunk.minZ; z <= chunk.maxZ; z++ {
			if !box.inside(x, box.minY, z) || r.src.isAir(r.get(x, box.minY, z)) {
				continue
			}
			inside := false
			for _, p := range s.pieces {
				if p.bounds().inside(x, box.minY, z) {
					inside = true
					break
				}
			}
			if !inside {
				continue
			}
			for y := box.minY - 1; y > r.minY; y-- {
				state := r.get(x, y, z)
				if !r.src.isAir(state) && !liquidBlock(state) {
					break
				}
				r.set(x, y, z, registry.BlockCobblestone.DefaultState())
			}
		}
	}
}
