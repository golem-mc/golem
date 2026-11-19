package world

//go:generate go run ../../../tools/gen shipwreck

import (
	_ "embed"
	"slices"
	"strings"
	"sync"

	"github.com/golem-mc/golem/internal/server/blocklogic"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

//go:embed shipwreck.zip
var shipwreckArchive []byte

var shipwreckTemplates = sync.OnceValue(func() map[string]*Template { return templateArchive(shipwreckArchive) })

// vanilla: ShipwreckPieces.STRUCTURE_LOCATION_BEACHED, STRUCTURE_LOCATION_OCEAN
var shipwreckBeached = []string{"with_mast", "sideways_full", "sideways_fronthalf", "sideways_backhalf", "rightsideup_full", "rightsideup_fronthalf", "rightsideup_backhalf", "with_mast_degraded", "rightsideup_full_degraded", "rightsideup_fronthalf_degraded", "rightsideup_backhalf_degraded"}
var shipwreckOcean = []string{"with_mast", "upsidedown_full", "upsidedown_fronthalf", "upsidedown_backhalf", "sideways_full", "sideways_fronthalf", "sideways_backhalf", "rightsideup_full", "rightsideup_fronthalf", "rightsideup_backhalf", "with_mast_degraded", "upsidedown_full_degraded", "upsidedown_fronthalf_degraded", "upsidedown_backhalf_degraded", "sideways_full_degraded", "sideways_fronthalf_degraded", "sideways_backhalf_degraded", "rightsideup_full_degraded", "rightsideup_fronthalf_degraded", "rightsideup_backhalf_degraded"}

var shipwreckLoot = map[nbt.String]string{"map_chest": "shipwreck_map", "treasure_chest": "shipwreck_treasure", "supply_chest": "shipwreck_supply"}

func init() {
	overworldStructureTypes["minecraft:shipwreck"] = shipwreckStructure{}
	overworldStructureTypes["minecraft:shipwreck_beached"] = shipwreckStructure{beached: true}
	overworldStructureTypes["minecraft:buried_treasure"] = buriedTreasureStructure{}
}

type shipwreckStructure struct{ beached bool }

// vanilla: ShipwreckStructure.findGenerationPoint, Structure.onTopOfChunkCenter
func (s shipwreckStructure) generationPoint(ctx *structureContext) ([3]int32, func() []structurePiece, bool) {
	x, z := ctx.chunkX*16, ctx.chunkZ*16
	y := ctx.g.columnHeight(x+8, z+8)
	if s.beached {
		y = ctx.g.firstFreeHeight(x+8, z+8) - 1
	}
	return [3]int32{x + 8, y, z + 8}, func() []structurePiece {
		rot := Rotation(ctx.rng.NextIntN(4))
		names := shipwreckOcean
		if s.beached {
			names = shipwreckBeached
		}
		name := names[ctx.rng.NextIntN(int32(len(names)))]
		p := &shipwreckPiece{name: name, template: shipwreckTemplates()[name], pos: [3]int32{x, 90, z}, rotation: rot, beached: s.beached}
		p.box = p.templateBox()
		return []structurePiece{p}
	}, true
}

// vanilla: ShipwreckPiece(StructureTemplateManager, CompoundTag)
func (shipwreckStructure) loadPiece(tag nbt.Compound) structurePiece {
	id, _ := nbt.Get[nbt.String](tag, "id")
	if id != "minecraft:shipwreck" {
		return nil
	}
	name, _ := nbt.Get[nbt.String](tag, "Template")
	short := strings.TrimPrefix(string(name), "minecraft:shipwreck/")
	template := shipwreckTemplates()[short]
	rot, _ := nbt.Get[nbt.String](tag, "Rot")
	rotation := slices.Index(rotationNames[:], string(rot))
	if template == nil || rotation < 0 {
		return nil
	}
	p := &shipwreckPiece{name: short, template: template, rotation: Rotation(rotation)}
	p.pos[0], _ = nbt.IntField(tag, "TPX")
	p.pos[1], _ = nbt.IntField(tag, "TPY")
	p.pos[2], _ = nbt.IntField(tag, "TPZ")
	p.beached = nbt.BoolField(tag, "isBeached")
	p.adjusted = nbt.BoolField(tag, "height_adjusted")
	p.box = p.templateBox()
	return p
}

// The shared start can be read by terrain workers during height adjustment.
type shipwreckPiece struct {
	mu                sync.Mutex
	name              string
	template          *Template
	pos               [3]int32
	rotation          Rotation
	beached, adjusted bool
	box               jbox
}

// offset compensates for rotating around (4, 0, 15), rather than the origin.
// vanilla: ShipwreckPieces.PIVOT, StructureTemplate.transform
func (p *shipwreckPiece) offset() (int32, int32) {
	x, _, z := TransformTemplatePos(4, 0, 15, MirrorNone, p.rotation)
	return 4 - x, 15 - z
}

func (p *shipwreckPiece) templateBox() jbox {
	dx, dz := p.offset()
	x, _, z := TransformTemplatePos(p.template.Size[0]-1, 0, p.template.Size[2]-1, MirrorNone, p.rotation)
	return jbox{p.pos[0] + dx + min(0, x), p.pos[1], p.pos[2] + dz + min(0, z), p.pos[0] + dx + max(0, x), p.pos[1] + p.template.Size[1] - 1, p.pos[2] + dz + max(0, z)}
}

func (p *shipwreckPiece) bounds() jbox { p.mu.Lock(); defer p.mu.Unlock(); return p.box }

func (p *shipwreckPiece) nbt() nbt.Compound {
	p.mu.Lock()
	defer p.mu.Unlock()
	return nbt.Compound{"id": nbt.String("minecraft:shipwreck"), "GD": nbt.Int(0), "O": nbt.Int(2),
		"BB":       nbt.IntArray{p.box.minX, p.box.minY, p.box.minZ, p.box.maxX, p.box.maxY, p.box.maxZ},
		"Template": nbt.String("minecraft:shipwreck/" + p.name), "TPX": nbt.Int(p.pos[0]), "TPY": nbt.Int(p.pos[1]), "TPZ": nbt.Int(p.pos[2]),
		"Rot": nbt.String(rotationNames[p.rotation]), "isBeached": nbtBool(p.beached), "height_adjusted": nbtBool(p.adjusted)}
}

// vanilla: ShipwreckPiece.postProcess, TemplateStructurePiece.postProcess
func (p *shipwreckPiece) place(r *decorRegion, chunk jbox, _ [3]int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.adjusted {
		sum, low := int32(0), r.minY+r.height
		kind := hmOceanFloorWG
		if p.beached {
			kind = hmWorldSurfaceWG
		}
		// The height-sampling rectangle is deliberately unrotated.
		for x := p.pos[0]; x < p.pos[0]+p.template.Size[0]; x++ {
			for z := p.pos[2]; z < p.pos[2]+p.template.Size[2]; z++ {
				h := r.heightOf(kind, x, z)
				sum += h
				low = min(low, h)
			}
		}
		p.pos[1] = sum / (p.template.Size[0] * p.template.Size[2])
		if p.beached {
			p.pos[1] = low - p.template.Size[1]/2 - r.rng.NextIntN(3)
		}
		p.adjusted = true
	}
	p.box = p.templateBox()
	placed, _ := p.template.Place(TemplatePlacement{Origin: p.pos, Rotation: p.rotation, Integrity: 1, Seed: 1, IgnoreEntities: true})
	dx, dz := p.offset()
	var markers []TemplateBlock
	var shapes []posKey
	var fill []posKey
	locked := map[posKey]bool{}
	for i, b := range placed.Changes {
		x, y, z := b.X+dx, b.Y, b.Z+dz
		if !chunk.inside(x, y, z) {
			continue
		}
		state, tag := b.State, placed.Tags[i]
		if registry.BlockOf(state) == registry.BlockStructureBlock {
			markers = append(markers, TemplateBlock{Pos: [3]int32{x, y, z}, NBT: tag})
			continue
		}
		if r.src.isAir(state) {
			continue
		}
		old := r.get(x, y, z)
		key := posKey{x, y, z}
		if waterSource(state) {
			locked[key] = true
		} else if waterloggable[registry.BlockOf(state)] {
			if !waterSource(old) {
				fill = append(fill, key)
			} else if templateTakesWater(state) {
				state, _ = registry.WithBool(state, registry.PropWaterlogged, true)
			}
		}
		r.set(x, y, z, state)
		r.markShape(x, y, z)
		shapes = append(shapes, key)
		if tag != nil {
			r.placeBlockEntity(x, y, z, state, tag)
		}
	}
	// Sources written by the template do not spread into its other blocks.
	// vanilla: StructureTemplate.placeInWorld
	for changed := true; changed; {
		changed = false
		for i := 0; i < len(fill); {
			k := fill[i]
			wet := false
			for _, d := range [5]posKey{{0, 1, 0}, {0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}} {
				n := posKey{k[0] + d[0], k[1] + d[1], k[2] + d[2]}
				if !locked[n] && waterSource(r.get(n[0], n[1], n[2])) {
					wet = true
					break
				}
			}
			if !wet {
				i++
				continue
			}
			if state := r.get(k[0], k[1], k[2]); templateTakesWater(state) {
				state, _ = registry.WithBool(state, registry.PropWaterlogged, true)
				r.set(k[0], k[1], k[2], state)
			}
			fill = append(fill[:i], fill[i+1:]...)
			changed = true
		}
	}
	r.templateShapes(shapes)
	for _, b := range markers {
		marker, _ := nbt.Get[nbt.String](b.NBT, "metadata")
		loot := shipwreckLoot[marker]
		x, y, z := b.Pos[0], b.Pos[1]-1, b.Pos[2]
		if loot != "" && registry.BlockOf(r.get(x, y, z)) == registry.BlockChest {
			r.placeBlockEntity(x, y, z, r.get(x, y, z), nbt.Compound{"id": nbt.String("minecraft:chest"), "LootTable": nbt.String("minecraft:chests/" + loot)})
		}
	}
}

func waterSource(state uint16) bool {
	f := blocklogic.FluidOf(state)
	return f.Kind == blocklogic.FluidWater && f.IsSource()
}
