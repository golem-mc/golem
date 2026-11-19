package world

import (
	"fmt"
	"math"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

func init() {
	overworldStructureTypes["minecraft:trail_ruins"] = jigsawStructureType{"minecraft:trail_ruins"}
	overworldStructureTypes["minecraft:ancient_city"] = jigsawStructureType{"minecraft:ancient_city"}
	overworldStructureTypes["minecraft:trial_chambers"] = jigsawStructureType{"minecraft:trial_chambers"}
	overworldStructureTypes["minecraft:pillager_outpost"] = jigsawStructureType{"minecraft:pillager_outpost"}
	for _, biome := range levelgen.AbandonedCampBiomes {
		id := "minecraft:abandoned_camp_" + biome
		overworldStructureTypes[id] = jigsawStructureType{id}
	}
	for _, biome := range []string{"desert", "plains", "savanna", "snowy", "taiga"} {
		id := "minecraft:village_" + biome
		overworldStructureTypes[id] = jigsawStructureType{id}
	}
}

// jigsawStructureType grows a structure from the jigsaw data under its id.
// vanilla: JigsawStructure
type jigsawStructureType struct{ id string }

func (t jigsawStructureType) generationPoint(ctx *structureContext) ([3]int32, func() []structurePiece, bool) {
	const minY, maxY = worldMinY, worldMinY + worldHeight - 1
	start, expand := planJigsawStart(jigsawStructureNamed(t.id), ctx.g.seed, ctx.chunkX, ctx.chunkZ, minY, maxY, ctx.g.firstFreeHeight)
	if start == nil {
		return [3]int32{}, nil, false
	}
	return start.centre, func() []structurePiece {
		expand()
		pieces := make([]structurePiece, len(start.pieces))
		for i, p := range start.pieces {
			pieces[i] = p
		}
		return pieces
	}, true
}

// firstFreeHeight is the Y above the topmost block of the column in the world
// surface heightmap at structure start time: the terrain, or the sea where it
// is under water.
// vanilla: NoiseBasedChunkGenerator.getFirstFreeHeight, FlatLevelSource.getBaseHeight
func (g *OverworldGenerator) firstFreeHeight(x, z int32) int32 {
	if g.flat != nil {
		return g.columnHeight(x, z) + 1
	}
	return max(g.columnHeight(x, z)+1, levelgen.OverworldSeaLevel)
}

// placeEntities queues the template entities clipped by their transformed block positions.
// Block processors (including terrain matching) do not move entities.
// vanilla: StructureTemplate.placeEntities
func (p *jigsawPiece) placeEntities(el *jigsawElement, r *decorRegion, chunk jbox, path string) {
	for i, e := range el.template.Entities {
		x, y, z := TransformTemplatePos(e.BlockPos[0], e.BlockPos[1], e.BlockPos[2], MirrorNone, p.rotation)
		if !chunk.inside(x+p.pos[0], y+p.pos[1], z+p.pos[2]) {
			continue
		}
		placed, ok := e.place(TemplatePlacement{Origin: p.pos, Rotation: p.rotation})
		if !ok {
			continue
		}
		// Vanilla draws an unseeded UUID; derive it so proto regeneration keeps identity.
		key := fmt.Appendf(nil, "jigsaw/%d/%s/%v/%d/%s/%d", r.worldSeed, el.template.id, p.pos, p.rotation, path, i)
		placed.Tag["UUID"] = nbt.UUIDToIntArray(uuid.NewSHA1(uuid.NameSpaceOID, key))
		x, y, z = int32(math.Floor(placed.Pos[0])), int32(math.Floor(placed.Pos[1])), int32(math.Floor(placed.Pos[2]))
		tag := r.finalizeStructureMob(placed.Tag, x, y, z, false)
		proto := r.protoAt(x, z)
		proto.entities = append(proto.entities, protoEntity{x: x & 15, y: y - r.minY, z: z & 15, tag: tag})
	}
}
