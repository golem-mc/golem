package world

import (
	"github.com/golem-mc/golem/nbt"
)

var rotationNames = [4]string{"NONE", "CLOCKWISE_90", "CLOCKWISE_180", "COUNTERCLOCKWISE_90"}

// nbt is the start as a chunk saves it under structures.starts.
// vanilla: StructureStart.createTag
func (s *jigsawStart) nbt(id string) nbt.Compound {
	children := make([]nbt.Tag, len(s.pieces))
	for i, p := range s.pieces {
		children[i] = p.nbt()
	}
	return structureStartNBT(id, s.chunkX, s.chunkZ, children)
}

// nbt is the piece as the start saves it.
// vanilla: PoolElementStructurePiece.addAdditionalSaveData, StructurePiece.createTag
func (p *jigsawPiece) nbt() nbt.Compound {
	junctions := make([]nbt.Tag, len(p.junctions))
	for i, j := range p.junctions {
		junctions[i] = nbt.Compound{
			"source_x": nbt.Int(j.sourceX), "source_ground_y": nbt.Int(j.sourceGroundY), "source_z": nbt.Int(j.sourceZ),
			"delta_y": nbt.Int(j.deltaY), "dest_proj": nbt.String(projectionName(j.matching)),
		}
	}
	piece := nbt.Compound{
		"id":   nbt.String("minecraft:jigsaw"),
		"BB":   nbt.IntArray{p.box.minX, p.box.minY, p.box.minZ, p.box.maxX, p.box.maxY, p.box.maxZ},
		"GD":   nbt.Int(0),
		"O":    nbt.Int(-1),
		"PosX": nbt.Int(p.pos[0]), "PosY": nbt.Int(p.pos[1]), "PosZ": nbt.Int(p.pos[2]),
		"ground_level_delta": nbt.Int(p.groundLevelDelta),
		"junctions":          nbt.List{Elem: nbt.TypeCompound, Items: junctions},
		"pool_element":       p.element.nbt(),
		"rotation":           nbt.String(rotationNames[p.rotation]),
	}
	if p.ignoreWaterlogging {
		piece["liquid_settings"] = nbt.String("ignore_waterlogging")
	}
	return piece
}

// nbt is the element as a piece saves it.
// vanilla: StructurePoolElement.CODEC
func (e *jigsawElement) nbt() nbt.Compound {
	switch {
	case e.list != nil:
		subs := make([]nbt.Tag, len(e.list))
		for i, sub := range e.list {
			subs[i] = sub.nbt()
		}
		return nbt.Compound{
			"element_type": nbt.String("minecraft:list_pool_element"),
			"elements":     nbt.List{Elem: nbt.TypeCompound, Items: subs},
			"projection":   nbt.String(projectionName(e.matching)),
		}
	case e.feature != "":
		return nbt.Compound{
			"element_type": nbt.String("minecraft:feature_pool_element"),
			"feature":      nbt.String(e.feature),
			"projection":   nbt.String(projectionName(e.matching)),
		}
	}
	singleType := "minecraft:single_pool_element"
	if e.legacy {
		singleType = "minecraft:legacy_single_pool_element"
	}
	var processors nbt.Tag = nbt.String(e.processorList)
	if e.processorList == "" {
		processors = nbt.Compound{"processors": nbt.List{Elem: nbt.TypeCompound}}
	}
	return nbt.Compound{
		"element_type": nbt.String(singleType),
		"location":     nbt.String(e.template.id),
		"processors":   processors,
		"projection":   nbt.String(projectionName(e.matching)),
	}
}

func projectionName(matching bool) string {
	if matching {
		return "terrain_matching"
	}
	return "rigid"
}
