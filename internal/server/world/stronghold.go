package world

import (
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/nbt"
)

func init() { overworldStructureTypes["minecraft:stronghold"] = strongholdStructure{} }

// strongholdPiece is a piece of a grown stronghold.
type strongholdPiece struct{ saved levelgen.StrongholdPieceSave }

func (p *strongholdPiece) bounds() jbox {
	b := p.saved.BB
	return jbox{int32(b.MinX), int32(b.MinY), int32(b.MinZ), int32(b.MaxX), int32(b.MaxY), int32(b.MaxZ)}
}

// nbt is the piece as saved. vanilla: StructurePiece.createTag
func (p *strongholdPiece) nbt() nbt.Compound {
	s := p.saved
	b := p.bounds()
	t := nbt.Compound{
		"id":        nbt.String(s.ID),
		"BB":        nbt.IntArray{b.minX, b.minY, b.minZ, b.maxX, b.maxY, b.maxZ},
		"O":         nbt.Int(s.Orientation),
		"GD":        nbt.Int(s.GenDepth),
		"EntryDoor": nbt.String(s.EntryDoor),
	}
	for _, f := range s.Tags {
		if f.Byte {
			t[f.Name] = nbt.Byte(f.Value)
		} else {
			t[f.Name] = nbt.Int(f.Value)
		}
	}
	return t
}

// strongholdStructure is the stronghold's structure type.
// vanilla: StrongholdStructure
type strongholdStructure struct{}

// generationPoint starts at the chunk's corner. vanilla: StrongholdStructure.findGenerationPoint
func (strongholdStructure) generationPoint(ctx *structureContext) ([3]int32, func() []structurePiece, bool) {
	pos := [3]int32{ctx.chunkX * 16, 0, ctx.chunkZ * 16}
	return pos, func() []structurePiece {
		saved := levelgen.NewStrongholdPieces(ctx.g.seed, ctx.chunkX, ctx.chunkZ, int(ctx.g.SeaLevel()), int(ctx.g.minY()))
		out := make([]structurePiece, len(saved))
		for i, s := range saved {
			out[i] = &strongholdPiece{s}
		}
		return out
	}, true
}
