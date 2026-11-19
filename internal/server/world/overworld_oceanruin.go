package world

//go:generate go run ../../../tools/gen oceanruin

import (
	_ "embed"
	"fmt"
	"sync"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

//go:embed oceanruin.zip
var oceanRuinArchive []byte

var oceanRuinTemplates = sync.OnceValue(func() map[string]*Template { return templateArchive(oceanRuinArchive) })

func init() {
	overworldStructureTypes["minecraft:ocean_ruin_cold"] = oceanRuinStructure{cold: true}
	overworldStructureTypes["minecraft:ocean_ruin_warm"] = oceanRuinStructure{}
}

type oceanRuinStructure struct{ cold bool }

// vanilla: OceanRuinStructure.findGenerationPoint, OceanRuinPieces.addPieces
func (s oceanRuinStructure) generationPoint(ctx *structureContext) ([3]int32, func() []structurePiece, bool) {
	x, z := ctx.chunkX*16, ctx.chunkZ*16
	return [3]int32{x + 8, ctx.g.columnHeight(x+8, z+8), z + 8}, func() []structurePiece {
		rng := ctx.rng
		rot := Rotation(rng.NextIntN(4))
		large := rng.NextFloat() <= 0.3 // vanilla: Structures.bootstrap
		integrity := float32(0.8)
		if large {
			integrity = 0.9
		}
		pieces := s.addPieces(nil, [3]int32{x, 90, z}, rot, large, integrity, rng)
		if !large || rng.NextFloat() > 0.9 { // vanilla: Structures.bootstrap
			return pieces
		}
		// The cluster tests a fixed 16x16 parent, independent of its template.
		dx, _, dz := TransformTemplatePos(15, 0, 15, MirrorNone, rot)
		parent := jbox{x + min(0, dx), 90, z + min(0, dz), x + max(0, dx), 90, z + max(0, dz)}
		positions := [][3]int32{
			{-15 + rng.NextIntN(8), 90, 17 + rng.NextIntN(7)},
			{-15 + rng.NextIntN(8), 90, 1 + rng.NextIntN(7)},
			{-15 + rng.NextIntN(8), 90, -12 + rng.NextIntN(5)},
			{1 + rng.NextIntN(7), 90, 17 + rng.NextIntN(7)},
			{1 + rng.NextIntN(7), 90, -12 + rng.NextIntN(3)},
			{17 + rng.NextIntN(7), 90, 19 + rng.NextIntN(6)},
			{17 + rng.NextIntN(7), 90, 1 + rng.NextIntN(7)},
			{17 + rng.NextIntN(7), 90, -12 + rng.NextIntN(5)},
		}
		count := 4 + rng.NextIntN(5)
		for range count {
			i := rng.NextIntN(int32(len(positions)))
			pos := positions[i]
			positions = append(positions[:i], positions[i+1:]...)
			pos[0], pos[2] = pos[0]+parent.minX, pos[2]+parent.minZ
			nextRot := Rotation(rng.NextIntN(4))
			dx, _, dz := TransformTemplatePos(5, 0, 6, MirrorNone, nextRot)
			box := jbox{pos[0] + min(0, dx), 90, pos[2] + min(0, dz), pos[0] + max(0, dx), 90, pos[2] + max(0, dz)}
			if !box.intersects(parent) {
				pieces = s.addPieces(pieces, pos, nextRot, false, 0.8, rng)
			}
		}
		return pieces
	}, true
}

// vanilla: OceanRuinPieces.addPiece
func (s oceanRuinStructure) addPieces(out []structurePiece, pos [3]int32, rot Rotation, large bool, integrity float32, rng *levelgen.LegacyRandomSource) []structurePiece {
	var n int32
	prefix := ""
	if large {
		i := rng.NextIntN(4)
		n = [4]int32{4, 5, 6, 7}[i]
		if s.cold {
			n = [4]int32{1, 2, 3, 8}[i]
		}
		prefix = "big_"
	} else {
		n = rng.NextIntN(8) + 1
	}
	kinds := []string{"warm"}
	if s.cold {
		kinds = []string{"brick", "cracked", "mossy"}
	}
	for i, kind := range kinds {
		switch i {
		case 1:
			integrity = 0.7
		case 2:
			integrity = 0.5
		}
		name := fmt.Sprintf("%s%s_%d", prefix, kind, n)
		p := &oceanRuinPiece{name: name, template: oceanRuinTemplates()[name], pos: pos, rotation: rot, cold: s.cold, large: large, integrity: integrity}
		p.box = p.templateBox()
		out = append(out, p)
	}
	return out
}

type oceanRuinPiece struct {
	mu          sync.Mutex
	name        string
	template    *Template
	pos         [3]int32
	rotation    Rotation
	cold, large bool
	integrity   float32
	box         jbox
}

func (p *oceanRuinPiece) templateBox() jbox {
	dx, _, dz := TransformTemplatePos(p.template.Size[0]-1, 0, p.template.Size[2]-1, MirrorNone, p.rotation)
	return jbox{p.pos[0] + min(0, dx), p.pos[1], p.pos[2] + min(0, dz), p.pos[0] + max(0, dx), p.pos[1] + p.template.Size[1] - 1, p.pos[2] + max(0, dz)}
}

func (p *oceanRuinPiece) bounds() jbox { p.mu.Lock(); defer p.mu.Unlock(); return p.box }

func (p *oceanRuinPiece) nbt() nbt.Compound {
	p.mu.Lock()
	defer p.mu.Unlock()
	biome := "WARM"
	if p.cold {
		biome = "COLD"
	}
	return nbt.Compound{"id": nbt.String("minecraft:orp"), "GD": nbt.Int(0), "O": nbt.Int(2),
		"BB":       nbt.IntArray{p.box.minX, p.box.minY, p.box.minZ, p.box.maxX, p.box.maxY, p.box.maxZ},
		"Template": nbt.String("minecraft:underwater_ruin/" + p.name), "TPX": nbt.Int(p.pos[0]), "TPY": nbt.Int(p.pos[1]), "TPZ": nbt.Int(p.pos[2]),
		"Rot": nbt.String(rotationNames[p.rotation]), "Integrity": nbt.Float(p.integrity), "BiomeType": nbt.String(biome), "IsLarge": nbtBool(p.large)}
}

// vanilla: OceanRuinPiece.postProcess, OceanRuinPiece.getHeight
func (p *oceanRuinPiece) place(r *decorRegion, chunk jbox, _ [3]int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pos[1] = r.heightOf(hmOceanFloorWG, p.pos[0], p.pos[2])
	box := p.templateBox()
	low, area, top := int32(512), int32(0), p.pos[1]-1
	for x := box.minX; x <= box.maxX; x++ {
		for z := box.minZ; z <= box.maxZ; z++ {
			y := top
			for y > r.minY+1 {
				state := r.get(x, y, z)
				if !r.src.isAir(state) && !registry.BlockOf(state).InTag(registry.TagIDIce) && fluidHolding(state).kind != fluidWater {
					break
				}
				y--
			}
			low = min(low, y)
			if y < top-2 {
				area++
			}
		}
	}
	if top-low > 2 && area > box.maxX-box.minX-2 {
		p.pos[1] = low + 1
	}
	p.box = p.templateBox()
	placed := p.process(r.worldSeed)
	clipped := &PlacedTemplate{}
	var shapes []posKey
	for i, b := range placed.Changes {
		if chunk.inside(b.X, b.Y, b.Z) {
			clipped.Changes = append(clipped.Changes, b)
			clipped.Tags = append(clipped.Tags, placed.Tags[i])
			shapes = append(shapes, posKey{b.X, b.Y, b.Z})
			r.markShape(b.X, b.Y, b.Z)
		}
	}
	r.placeFeatureTemplate(clipped)
	r.templateShapes(shapes)
	// Markers are independent of block rot and run after the template's shapes.
	for _, b := range p.template.Palettes[0] {
		if registry.BlockOf(b.State) != registry.BlockStructureBlock {
			continue
		}
		x, y, z := TransformTemplatePos(b.Pos[0], b.Pos[1], b.Pos[2], MirrorNone, p.rotation)
		x, y, z = x+p.pos[0], y+p.pos[1], z+p.pos[2]
		if !chunk.inside(x, y, z) {
			continue
		}
		marker, _ := nbt.Get[nbt.String](b.NBT, "metadata")
		p.marker(r, x, y, z, string(marker))
	}
}

// process applies block rot and the five-block archaeology cap before chunk clipping.
// vanilla: StructureTemplate.processBlockInfos, CappedProcessor.finalizeProcessing
func (p *oceanRuinPiece) process(seed int64) *PlacedTemplate {
	placed, _ := p.template.Place(TemplatePlacement{Origin: p.pos, Rotation: p.rotation, Integrity: 1, Seed: 1, IgnoreEntities: true,
		process: func(state uint16, tag nbt.Compound, pos [3]int32) (uint16, nbt.Compound, bool) {
			if _, ok := (blockRot{integrity: p.integrity}).apply(state, 0, pos[0], pos[1], pos[2], 0); !ok {
				return state, tag, false
			}
			return state, tag, registry.BlockOf(state) != registry.BlockAir && registry.BlockOf(state) != registry.BlockStructureBlock
		}})
	candidate, replacement, loot := registry.BlockSand, registry.BlockSuspiciousSand, "minecraft:archaeology/ocean_ruin_warm"
	if p.cold {
		candidate, replacement, loot = registry.BlockGravel, registry.BlockSuspiciousGravel, "minecraft:archaeology/ocean_ruin_cold"
	}
	cap := cappedProcessor{delegate: ruleProcessor{rules: []processorRule{{kind: inputBlock, input: candidate, output: replacement.DefaultState(), loot: loot}}}, min: 5, max: 5}
	cap.finalize(placed, seed, p.pos, 0, nil)
	for i, tag := range placed.Tags {
		if registry.BlockOf(placed.Changes[i].State) == replacement {
			tag["id"] = nbt.String(KindBrushable.Name())
			placed.Tags[i] = tag
		}
	}
	return placed
}

// vanilla: OceanRuinPiece.handleDataMarker
func (p *oceanRuinPiece) marker(r *decorRegion, x, y, z int32, marker string) {
	switch marker {
	case "chest":
		state, _ := registry.WithBool(registry.BlockChest.DefaultState(), registry.PropWaterlogged, fluidHolding(r.get(x, y, z)).kind == fluidWater)
		r.set(x, y, z, state)
		loot := "minecraft:chests/underwater_ruin_small"
		if p.large {
			loot = "minecraft:chests/underwater_ruin_big"
		}
		r.placeBlockEntity(x, y, z, state, nbt.Compound{"id": nbt.String("minecraft:chest"), "LootTable": nbt.String(loot)})
	case "drowned":
		r.spawnStructureMob("minecraft:drowned", fmt.Sprintf("ocean_ruin/%d/%d/%d/%d", r.worldSeed, x, y, z), x, y, z, false)
		state := uint16(airState)
		if y <= SeaLevel {
			state = registry.BlockWater.DefaultState()
		}
		r.set(x, y, z, state)
	}
}
